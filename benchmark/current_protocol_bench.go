//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

type latencyRecorder struct {
	mu     sync.Mutex
	values []time.Duration
}

func (r *latencyRecorder) add(v time.Duration) {
	r.mu.Lock()
	r.values = append(r.values, v)
	r.mu.Unlock()
}

func (r *latencyRecorder) summary() (count int, avg, p50, p95, p99 time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.values) == 0 {
		return 0, 0, 0, 0, 0
	}
	values := append([]time.Duration(nil), r.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, v := range values {
		total += v
	}
	return len(values), total / time.Duration(len(values)), percentile(values, 50), percentile(values, 95), percentile(values, 99)
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
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
	var (
		host              = flag.String("host", "127.0.0.1", "gateway host")
		startPort         = flag.Int("start-port", 8090, "first gateway port")
		gateways          = flag.Int("gateways", 10, "gateway count")
		perGateway        = flag.Int("per-gateway", 1000, "websocket connections per gateway for heartbeat test")
		heartbeatDuration = flag.Duration("heartbeat-duration", 30*time.Second, "heartbeat test duration")
		heartbeatEvery    = flag.Duration("heartbeat-every", 5*time.Second, "heartbeat interval")
		pairs             = flag.Int("pairs", 100, "single-chat sender/receiver connection pairs")
		messages          = flag.Int("messages", 1000, "single-chat messages")
		messageTimeout    = flag.Duration("message-timeout", 45*time.Second, "single-chat receive timeout")
		secret            = flag.String("jwt-secret", "linkgo_im_secret_2026", "JWT secret")
	)
	flag.Parse()

	fmt.Printf("bench_config gateways=%d per_gateway=%d heartbeat_duration=%s pairs=%d messages=%d\n",
		*gateways, *perGateway, heartbeatDuration.String(), *pairs, *messages)

	waitGateways(*host, *startPort, *gateways, 60*time.Second)

	if *perGateway > 0 {
		runHeartbeatBench(*host, *startPort, *gateways, *perGateway, *heartbeatDuration, *heartbeatEvery, *secret)
	}
	if *pairs > 0 && *messages > 0 {
		runSingleChatBench(*host, *startPort, *gateways, *pairs, *messages, *messageTimeout, *secret)
	}

	scrapeMetrics(*host, *startPort, *gateways)
}

func waitGateways(host string, startPort, gateways int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		ready := 0
		for i := 0; i < gateways; i++ {
			url := fmt.Sprintf("http://%s:%d/healthz", host, startPort+i)
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode == http.StatusOK {
				ready++
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
		if ready == gateways {
			fmt.Printf("gateway_ready ready=%d/%d\n", ready, gateways)
			return
		}
		if time.Now().After(deadline) {
			fmt.Printf("gateway_ready ready=%d/%d timeout=%s\n", ready, gateways, timeout)
			return
		}
		time.Sleep(time.Second)
	}
}

func runHeartbeatBench(host string, startPort, gateways, perGateway int, duration, every time.Duration, secret string) {
	var success, failed, heartbeatSent, heartbeatOK, heartbeatFailed int64
	var latencies latencyRecorder
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < gateways; i++ {
		port := startPort + i
		for j := 0; j < perGateway; j++ {
			wg.Add(1)
			uid := fmt.Sprintf("hb_%d_%d", port, j)
			go func(port int, uid string) {
				defer wg.Done()
				conn, err := dialWS(host, port, uid, secret)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					return
				}
				defer conn.Close()
				atomic.AddInt64(&success, 1)

				ticker := time.NewTicker(every)
				defer ticker.Stop()
				end := time.After(duration)
				for {
					select {
					case <-end:
						return
					case <-ticker.C:
						traceID := fmt.Sprintf("hb-%s-%d", uid, time.Now().UnixNano())
						payload, _ := proto.Marshal(&api.WireMessage{
							MsgType: api.MsgType_HEARTBEAT,
							TraceId: traceID,
							SentAt:  time.Now().UnixMilli(),
						})
						begin := time.Now()
						atomic.AddInt64(&heartbeatSent, 1)
						if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
							atomic.AddInt64(&heartbeatFailed, 1)
							return
						}
						_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
						_, msg, err := conn.ReadMessage()
						if err != nil {
							atomic.AddInt64(&heartbeatFailed, 1)
							return
						}
						var pong api.WireMessage
						if err := proto.Unmarshal(msg, &pong); err != nil || pong.MsgType != api.MsgType_HEARTBEAT {
							atomic.AddInt64(&heartbeatFailed, 1)
							return
						}
						atomic.AddInt64(&heartbeatOK, 1)
						latencies.add(time.Since(begin))
					}
				}
			}(port, uid)
			if (j+1)%200 == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	wg.Wait()
	count, avg, p50, p95, p99 := latencies.summary()
	fmt.Printf("heartbeat_result total=%d success_conn=%d failed_conn=%d heartbeat_sent=%d heartbeat_ok=%d heartbeat_failed=%d latency_count=%d avg=%s p50=%s p95=%s p99=%s elapsed=%s\n",
		gateways*perGateway,
		success,
		failed,
		heartbeatSent,
		heartbeatOK,
		heartbeatFailed,
		count,
		avg,
		p50,
		p95,
		p99,
		time.Since(start),
	)
}

type chatPair struct {
	senderUID   string
	receiverUID string
	sender      *websocket.Conn
	receiver    *websocket.Conn
}

func runSingleChatBench(host string, startPort, gateways, pairs, messages int, timeout time.Duration, secret string) {
	if gateways < 1 {
		return
	}
	chatPairs := make([]chatPair, 0, pairs)
	for i := 0; i < pairs; i++ {
		senderPort := startPort + (i % gateways)
		receiverPort := startPort + ((i + gateways/2) % gateways)
		senderUID := fmt.Sprintf("msg_sender_%d", i)
		receiverUID := fmt.Sprintf("msg_receiver_%d", i)
		sender, err := dialWS(host, senderPort, senderUID, secret)
		if err != nil {
			fmt.Printf("single_chat_setup_error sender=%s err=%v\n", senderUID, err)
			continue
		}
		receiver, err := dialWS(host, receiverPort, receiverUID, secret)
		if err != nil {
			_ = sender.Close()
			fmt.Printf("single_chat_setup_error receiver=%s err=%v\n", receiverUID, err)
			continue
		}
		chatPairs = append(chatPairs, chatPair{
			senderUID:   senderUID,
			receiverUID: receiverUID,
			sender:      sender,
			receiver:    receiver,
		})
	}
	defer func() {
		for _, pair := range chatPairs {
			_ = pair.sender.Close()
			_ = pair.receiver.Close()
		}
	}()
	if len(chatPairs) == 0 {
		fmt.Println("single_chat_result setup_failed=true")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startAt := sync.Map{}
	var received, acked, recvErrors int64
	var latencies latencyRecorder
	var wg sync.WaitGroup

	for _, pair := range chatPairs {
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
				_ = pair.receiver.SetReadDeadline(time.Now().Add(time.Second))
				_, msg, err := pair.receiver.ReadMessage()
				if err != nil {
					if ctx.Err() != nil || isTimeout(err) {
						continue
					}
					atomic.AddInt64(&recvErrors, 1)
					return
				}
				var frame api.WireMessage
				if err := proto.Unmarshal(msg, &frame); err != nil || frame.MsgType != api.MsgType_NORMAL {
					continue
				}
				if started, ok := startAt.LoadAndDelete(frame.ClientMsgId); ok {
					latencies.add(time.Since(started.(time.Time)))
				}
				atomic.AddInt64(&received, 1)
				ackPayload, _ := proto.Marshal(&api.WireMessage{
					MsgType:      api.MsgType_ACK,
					AckMessageId: frame.MessageId,
					TraceId:      frame.TraceId,
					SentAt:       time.Now().UnixMilli(),
				})
				if err := pair.receiver.WriteMessage(websocket.BinaryMessage, ackPayload); err == nil {
					atomic.AddInt64(&acked, 1)
				}
			}
		}()
	}

	begin := time.Now()
	var sent int64
	for i := 0; i < messages; i++ {
		pair := chatPairs[i%len(chatPairs)]
		clientMsgID := fmt.Sprintf("bench-msg-%d-%d", begin.UnixNano(), i)
		traceID := fmt.Sprintf("bench-trace-%d", i)
		startAt.Store(clientMsgID, time.Now())
		payload, _ := proto.Marshal(&api.WireMessage{
			From:        pair.senderUID,
			To:          pair.receiverUID,
			ToType:      "user",
			MsgType:     api.MsgType_NORMAL,
			Body:        fmt.Sprintf("hello-%d", i),
			ClientMsgId: clientMsgID,
			TraceId:     traceID,
		})
		if err := pair.sender.WriteMessage(websocket.BinaryMessage, payload); err == nil {
			atomic.AddInt64(&sent, 1)
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&received) >= sent {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	count, avg, p50, p95, p99 := latencies.summary()
	timeoutCount := sent - atomic.LoadInt64(&received)
	ackTimeoutRate := float64(timeoutCount) / math.Max(1, float64(sent))
	fmt.Printf("single_chat_result pairs=%d sent=%d received=%d acked=%d receive_errors=%d timeout=%d ack_timeout_rate=%.4f latency_count=%d avg=%s p50=%s p95=%s p99=%s elapsed=%s\n",
		len(chatPairs),
		sent,
		received,
		acked,
		recvErrors,
		timeoutCount,
		ackTimeoutRate,
		count,
		avg,
		p50,
		p95,
		p99,
		time.Since(begin),
	)
}

func dialWS(host string, port int, uid, secret string) (*websocket.Conn, error) {
	token, err := signJWT(uid, secret)
	if err != nil {
		return nil, err
	}
	u := url.URL{
		Scheme:   "ws",
		Host:     fmt.Sprintf("%s:%d", host, port),
		Path:     "/ws",
		RawQuery: "token=" + url.QueryEscape(token),
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	return conn, err
}

func signJWT(uid, secret string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": uid,
		"exp":     time.Now().Add(2 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func scrapeMetrics(host string, startPort, gateways int) {
	var ackSuccess, retrySuccess, retryExhausted float64
	for i := 0; i < gateways; i++ {
		body := httpGet(fmt.Sprintf("http://%s:%d/metrics", host, startPort+i))
		ackSuccess += metricValue(body, `linkgo_ack_operations_total{result="success"}`)
		retrySuccess += metricValue(body, `linkgo_ack_operations_total{result="retry_success"}`)
		retryExhausted += metricValue(body, `linkgo_ack_operations_total{result="retry_exhausted"}`)
	}

	transfer := httpGet("http://127.0.0.1:9102/metrics")
	kafkaRetry := metricValueContains(transfer, "linkgo_kafka_operations_total", `stage="retry_write"`)
	kafkaDLQ := metricValueContains(transfer, "linkgo_kafka_operations_total", `stage="dlq_write"`)
	fmt.Printf("server_metrics ack_success=%.0f ack_retry_success=%.0f ack_retry_exhausted=%.0f kafka_retry_write=%.0f kafka_dlq_write=%.0f\n",
		ackSuccess, retrySuccess, retryExhausted, kafkaRetry, kafkaDLQ)
}

func httpGet(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

func metricValue(body, prefix string) float64 {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			var value float64
			_, _ = fmt.Sscanf(line[len(prefix):], " %f", &value)
			return value
		}
	}
	return 0
}

func metricValueContains(body, metric, label string) float64 {
	var sum float64
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metric) && strings.Contains(line, label) {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			var value float64
			_, _ = fmt.Sscanf(fields[len(fields)-1], "%f", &value)
			sum += value
		}
	}
	return sum
}

func isTimeout(err error) bool {
	return strings.Contains(err.Error(), "i/o timeout")
}

func postJSON(url, token string, req any) (*http.Response, error) {
	payload, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(httpReq)
}
