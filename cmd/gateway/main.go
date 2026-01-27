package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// 配置
const (
	GatewayPort = 8090
	RpcPort     = 9002
	LogicAddr   = "127.0.0.1:9001"
	RedisAddr   = "127.0.0.1:6379"
)

var (
	userConns   sync.Map
	logicClient api.LogicClient
	rdb         *redis.Client
	upgrader    = websocket.Upgrader{
		// 允许跨域 (关键)
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

func main() {
	// 1. 初始化 Redis
	rdb = redis.NewClient(&redis.Options{Addr: RedisAddr, Password: "123456"})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatal("❌ Redis 连接失败:", err)
	}
	fmt.Println("📚 Redis 连接成功")

	// 2. 连接 Logic
	conn, err := grpc.NewClient(LogicAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("❌ 连接 Logic 失败:", err)
	}
	logicClient = api.NewLogicClient(conn)
	fmt.Println("📚 Logic 连接初始化完成")

	// 3. 启动 gRPC
	go startRpcServer()

	// 4. 启动 WebSocket
	http.HandleFunc("/ws", handleWebSocket)
	fmt.Printf("🚪 Gateway 服务已启动 (WS: %d, RPC: %d)\n", GatewayPort, RpcPort)
	
	// 监听所有 IP，防止 WSL 网络问题
	if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", GatewayPort), nil); err != nil {
		log.Fatal("❌ HTTP 服务启动失败:", err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 【Debug 日志 1】请求进来了吗？
	fmt.Printf("🔍 新连接请求: %s (来自: %s)\n", r.URL.String(), r.RemoteAddr)

	userId := r.URL.Query().Get("user_id")
	if userId == "" {
		fmt.Println("   ❌ 拒绝连接: 缺少 user_id")
		http.Error(w, "Missing user_id", 400)
		return
	}

	// 升级连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("   ❌ WebSocket 升级失败:", err)
		return
	}

	// 【核心修复】defer 必须放在 Upgrade 成功后的第一行！
	// 确保无论发生什么（报错、Panic、断网），都会执行清理
	defer func() {
		fmt.Printf("❌ 用户下线: %s (清理资源)\n", userId)
		conn.Close()
		userConns.Delete(userId)
		rdb.Del(context.Background(), "route:"+userId)
	}()

	// 1. 登记连接
	userConns.Store(userId, conn)
	myRpcAddr := fmt.Sprintf("127.0.0.1:%d", RpcPort)
	rdb.Set(context.Background(), "route:"+userId, myRpcAddr, 5*time.Minute)
	
	fmt.Printf("✅ 用户握手成功: %s\n", userId)

	// 通知 Logic (异步，不阻塞)
	go func() {
		_, err := logicClient.UserLogin(context.Background(), &api.UserLoginReq{UserId: userId})
		if err != nil {
			fmt.Printf("   ⚠️ 通知 Logic 登录失败: %v\n", err)
		}
	}()

	// 2. 心跳与消息读取循环
	readTimeout := 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(readTimeout))

	for {
		_, bytes, err := conn.ReadMessage()
		if err != nil {
			// 这里不需要打印太详细，因为断开连接是常态
			// fmt.Println("   👋 连接读取结束:", err)
			break 
		}

		// 续命
		conn.SetReadDeadline(time.Now().Add(readTimeout))

		if string(bytes) == "PING" {
			conn.WriteMessage(websocket.TextMessage, []byte("PONG"))
			fmt.Printf("💓 [%s] 心跳续命\n", userId)
			continue
		}

		// 正常消息
		_, err = logicClient.PushMessage(context.Background(), &api.PushMsgReq{
			UserId:  userId,
			Content: bytes,
		})
		if err != nil {
			fmt.Printf("   ⚠️ Logic 转发失败: %v\n", err)
		}
	}
}

// ... gRPC Server 代码保持不变 (如果你没改动的话) ...
func startRpcServer() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", RpcPort))
	if err != nil {
		panic(err)
	}
	s := grpc.NewServer()
	api.RegisterGatewayServer(s, &gatewayServer{})
	s.Serve(lis)
}

type gatewayServer struct {
	api.UnimplementedGatewayServer
}

func (s *gatewayServer) PushToUser(ctx context.Context, req *api.SendToUserReq) (*api.SendToUserReply, error) {
	val, ok := userConns.Load(req.TargetUserId)
	if !ok {
		return &api.SendToUserReply{Success: false}, nil
	}
	conn := val.(*websocket.Conn)
	err := conn.WriteMessage(websocket.BinaryMessage, req.Content)
	if err != nil {
		return &api.SendToUserReply{Success: false}, err
	}
	return &api.SendToUserReply{Success: true}, nil
}