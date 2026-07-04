//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type fakeLogicClient struct {
	core *corelogic.LogicHandler
}

func (f *fakeLogicClient) Login(context.Context, *api.LoginReq, ...grpc.CallOption) (*api.LoginReply, error) {
	return nil, fmt.Errorf("not used")
}

func (f *fakeLogicClient) PushMessage(ctx context.Context, in *api.PushMsgReq, _ ...grpc.CallOption) (*api.PushMsgReply, error) {
	return f.core.PushMessage(ctx, in)
}

func (f *fakeLogicClient) UserLogin(context.Context, *api.UserLoginReq, ...grpc.CallOption) (*api.UserLoginReply, error) {
	return &api.UserLoginReply{}, nil
}

func (f *fakeLogicClient) GetHistory(context.Context, *api.GetHistoryReq, ...grpc.CallOption) (*api.GetHistoryReply, error) {
	return &api.GetHistoryReply{}, nil
}

type latencyRecorder struct {
	mu     sync.Mutex
	values []time.Duration
}

func (r *latencyRecorder) add(d time.Duration) {
	r.mu.Lock()
	r.values = append(r.values, d)
	r.mu.Unlock()
}

func (r *latencyRecorder) summary() (count int, avg, p50, p95, p99, max time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.values) == 0 {
		return 0, 0, 0, 0, 0, 0
	}
	values := append([]time.Duration(nil), r.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, v := range values {
		total += v
	}
	return len(values), total / time.Duration(len(values)), percentile(values, 50), percentile(values, 95), percentile(values, 99), values[len(values)-1]
}

func percentile(values []time.Duration, p float64) time.Duration {
	idx := int(math.Ceil(float64(len(values))*p/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func main() {
	startPort := flag.Int("start-port", 18090, "first local gateway port")
	gateways := flag.Int("gateways", 10, "local gateway count")
	perGateway := flag.Int("per-gateway", 1000, "heartbeat connections per gateway")
	heartbeatDuration := flag.Duration("heartbeat-duration", 30*time.Second, "heartbeat test duration")
	heartbeatEvery := flag.Duration("heartbeat-every", 5*time.Second, "heartbeat interval")
	pairs := flag.Int("pairs", 100, "single-chat connection pairs")
	messages := flag.Int("messages", 1000, "single-chat messages")
	redisAddr := flag.String("redis-addr", "127.0.0.1:6380", "redis address")
	flag.Parse()

	logx.Disable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	authutil.SetJWTSecret("linkgo_im_secret_2026")
	rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(err)
	}
	_ = rdb.FlushDB(ctx).Err()

	core := &corelogic.LogicHandler{
		Rdb:      rdb,
		Delivery: &delivery.RedisDelivery{Rdb: rdb},
	}
	logicClient := &fakeLogicClient{core: core}

	for i := 0; i < *gateways; i++ {
		gatewayID := fmt.Sprintf("gateway-%d", i)
		go server.Manager.SubscribeRedis(ctx, rdb, gatewayID)
		startGateway(ctx, *startPort+i, gatewayID, rdb, logicClient)
	}
	time.Sleep(800 * time.Millisecond)

	fmt.Printf("local_core_config gateways=%d per_gateway=%d heartbeat_duration=%s pairs=%d messages=%d redis=%s\n",
		*gateways, *perGateway, heartbeatDuration.String(), *pairs, *messages, *redisAddr)
	runHeartbeat(*startPort, *gateways, *perGateway, *heartbeatDuration, *heartbeatEvery)
	runSingleChat(*startPort, *gateways, *pairs, *messages)
	printRedisStats(ctx, rdb)
}

func startGateway(ctx context.Context, port int, gatewayID string, rdb *redis.Client, logicClient api.LogicClient) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		token := authutil.ExtractBearerToken(r.Header.Get("Authorization"))
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		claims, err := authutil.ParseToken(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		clientConn := server.NewClientConn(conn, fmt.Sprintf("%d-%d", port, time.Now().UnixNano()))
		routeValue := server.BuildRouteValue(gatewayID, clientConn.SessionID)
		server.Manager.Add(claims.UserID, clientConn)
		defer server.Manager.Remove(claims.UserID, clientConn)
		defer clientConn.Close()
		_ = server.RefreshRoute(ctx, rdb, claims.UserID, routeValue, 45*time.Second)
		defer server.ClearRouteIfMatch(ctx, rdb, claims.UserID, routeValue)
		server.SyncOfflineMessages(ctx, rdb, claims.UserID, clientConn, r.URL.Query().Get("session_id"), -1)
		server.StartClientLoop(ctx, claims.UserID, clientConn, logicClient, rdb, routeValue, 45*time.Second)
	})

	go func() {
		if err := (&http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}).ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("gateway_error port=%d err=%v\n", port, err)
		}
	}()
}

func runHeartbeat(startPort, gateways, perGateway int, duration, every time.Duration) {
	var successConn, failedConn, sent, ok, failed int64
	var latencies latencyRecorder
	var wg sync.WaitGroup
	begin := time.Now()

	for g := 0; g < gateways; g++ {
		port := startPort + g
		for i := 0; i < perGateway; i++ {
			wg.Add(1)
			uid := fmt.Sprintf("hb_%d_%d", g, i)
			go func(port int, uid string) {
				defer wg.Done()
				conn, err := dialWS(port, uid)
				if err != nil {
					atomic.AddInt64(&failedConn, 1)
					return
				}
				defer conn.Close()
				atomic.AddInt64(&successConn, 1)

				ticker := time.NewTicker(every)
				defer ticker.Stop()
				end := time.After(duration)
				for {
					select {
					case <-end:
						return
					case <-ticker.C:
						payload, _ := proto.Marshal(&api.WireMessage{
							MsgType: api.MsgType_HEARTBEAT,
							TraceId: uid,
							SentAt:  time.Now().UnixMilli(),
						})
						start := time.Now()
						atomic.AddInt64(&sent, 1)
						if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
							atomic.AddInt64(&failed, 1)
							return
						}
						_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
						_, msg, err := conn.ReadMessage()
						if err != nil {
							atomic.AddInt64(&failed, 1)
							return
						}
						var pong api.WireMessage
						if err := proto.Unmarshal(msg, &pong); err != nil || pong.MsgType != api.MsgType_HEARTBEAT {
							atomic.AddInt64(&failed, 1)
							return
						}
						atomic.AddInt64(&ok, 1)
						latencies.add(time.Since(start))
					}
				}
			}(port, uid)
			if (i+1)%250 == 0 {
				time.Sleep(80 * time.Millisecond)
			}
		}
	}

	wg.Wait()
	count, avg, p50, p95, p99, max := latencies.summary()
	fmt.Printf("heartbeat_result total_conn=%d success_conn=%d failed_conn=%d sent=%d ok=%d failed=%d latency_count=%d avg=%s p50=%s p95=%s p99=%s max=%s elapsed=%s\n",
		gateways*perGateway, successConn, failedConn, sent, ok, failed, count, avg, p50, p95, p99, max, time.Since(begin))
}

type chatPair struct {
	senderUID   string
	receiverUID string
	sender      *websocket.Conn
	receiver    *websocket.Conn
}

func runSingleChat(startPort, gateways, pairCount, messageCount int) {
	pairs := make([]chatPair, 0, pairCount)
	for i := 0; i < pairCount; i++ {
		senderPort := startPort + (i % gateways)
		receiverPort := startPort + ((i + gateways/2) % gateways)
		senderUID := fmt.Sprintf("sender_%d", i)
		receiverUID := fmt.Sprintf("receiver_%d", i)
		sender, err := dialWS(senderPort, senderUID)
		if err != nil {
			continue
		}
		receiver, err := dialWS(receiverPort, receiverUID)
		if err != nil {
			_ = sender.Close()
			continue
		}
		pairs = append(pairs, chatPair{senderUID: senderUID, receiverUID: receiverUID, sender: sender, receiver: receiver})
	}
	defer func() {
		for _, pair := range pairs {
			_ = pair.sender.Close()
			_ = pair.receiver.Close()
		}
	}()
	if len(pairs) == 0 {
		fmt.Println("single_chat_result setup_failed=true")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	startTimes := sync.Map{}
	var received, acked, receiveErrors int64
	var latencies latencyRecorder
	var wg sync.WaitGroup

	for _, pair := range pairs {
		pair := pair
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				_, msg, err := pair.receiver.ReadMessage()
				if err != nil {
					if ctx.Err() != nil || strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					atomic.AddInt64(&receiveErrors, 1)
					return
				}
				var frame api.WireMessage
				if err := proto.Unmarshal(msg, &frame); err != nil || frame.MsgType != api.MsgType_NORMAL {
					continue
				}
				if started, ok := startTimes.LoadAndDelete(frame.ClientMsgId); ok {
					latencies.add(time.Since(started.(time.Time)))
				}
				atomic.AddInt64(&received, 1)
				ack, _ := proto.Marshal(&api.WireMessage{
					MsgType:      api.MsgType_ACK,
					AckMessageId: frame.MessageId,
					TraceId:      frame.TraceId,
					SentAt:       time.Now().UnixMilli(),
				})
				if err := pair.receiver.WriteMessage(websocket.BinaryMessage, ack); err == nil {
					atomic.AddInt64(&acked, 1)
				}
			}
		}()
	}

	begin := time.Now()
	var sent int64
	for i := 0; i < messageCount; i++ {
		pair := pairs[i%len(pairs)]
		clientMsgID := fmt.Sprintf("cmid_%d_%d", begin.UnixNano(), i)
		startTimes.Store(clientMsgID, time.Now())
		payload, _ := proto.Marshal(&api.WireMessage{
			From:        pair.senderUID,
			To:          pair.receiverUID,
			ToType:      "user",
			MsgType:     api.MsgType_NORMAL,
			Body:        "hello",
			ClientMsgId: clientMsgID,
			TraceId:     fmt.Sprintf("tr_%d", i),
		})
		if err := pair.sender.WriteMessage(websocket.BinaryMessage, payload); err == nil {
			atomic.AddInt64(&sent, 1)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&received) < sent {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	for _, pair := range pairs {
		_ = pair.receiver.Close()
	}
	wg.Wait()

	count, avg, p50, p95, p99, max := latencies.summary()
	timeouts := sent - received
	fmt.Printf("single_chat_result pairs=%d sent=%d received=%d acked=%d receive_errors=%d timeout=%d ack_timeout_rate=%.4f latency_count=%d avg=%s p50=%s p95=%s p99=%s max=%s elapsed=%s\n",
		len(pairs), sent, received, acked, receiveErrors, timeouts, float64(timeouts)/math.Max(1, float64(sent)), count, avg, p50, p95, p99, max, time.Since(begin))
}

func dialWS(port int, uid string) (*websocket.Conn, error) {
	token, err := authutil.GenerateToken(uid)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("127.0.0.1:%d", port), Path: "/ws", RawQuery: "token=" + url.QueryEscape(token)}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	return conn, err
}

func printRedisStats(ctx context.Context, rdb *redis.Client) {
	keys, _ := rdb.DBSize(ctx).Result()
	routes := countKeys(ctx, rdb, "route:*")
	pending := countKeys(ctx, rdb, "pending_ack:*")
	timelines := countKeys(ctx, rdb, "session_timeline:*")
	fmt.Printf("redis_result db_keys=%d route_keys=%d pending_ack_keys=%d session_timeline_keys=%d\n", keys, routes, pending, timelines)
}

func countKeys(ctx context.Context, rdb *redis.Client, pattern string) int64 {
	var cursor uint64
	var total int64
	for {
		keys, next, _ := rdb.Scan(ctx, cursor, pattern, 1000).Result()
		total += int64(len(keys))
		if next == 0 {
			return total
		}
		cursor = next
	}
}
