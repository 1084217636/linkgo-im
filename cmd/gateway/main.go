package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings" // 必须导入
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	GatewayPort = 8090
	LogicAddr   = "127.0.0.1:9001"
	RedisAddr   = "127.0.0.1:6379"
)

var (
	userConns   sync.Map
	logicClient api.LogicClient
	rdb         *redis.Client
	upgrader    = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
)

func main() {
	rdb = redis.NewClient(&redis.Options{Addr: RedisAddr, Password: "123456"})
	
	// 连接 Logic 服务
	conn, err := grpc.NewClient(LogicAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	logicClient = api.NewLogicClient(conn)

	// 【关键】Redis 广播订阅逻辑
	go func() {
		ctx := context.Background()
		pubsub := rdb.Subscribe(ctx, "im_message_push")
		for msg := range pubsub.Channel() {
			// 格式 "targetId:message"
			parts := strings.SplitN(msg.Payload, ":", 2)
			if len(parts) < 2 { continue }
			targetId, content := parts[0], parts[1]

			if val, ok := userConns.Load(targetId); ok {
				_ = val.(*websocket.Conn).WriteMessage(websocket.TextMessage, []byte(content))
			}
		}
	}()

	router := gin.Default()
	router.GET("/ws", handleWS)
	router.Run(":8090")
}

func handleWS(c *gin.Context) {
	userId := c.Query("user_id")
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil { return }

	// 资源释放
	defer func() {
		ws.Close()
		userConns.Delete(userId)
		rdb.Del(context.Background(), "route:"+userId)
		fmt.Printf("🔌 用户 %s 已清理\n", userId)
	}()

	userConns.Store(userId, ws)
	// 在 Redis 标记在线
	rdb.Set(context.Background(), "route:"+userId, "online", 30*time.Minute)

	// 拉取历史消息（离线补偿）
	go syncOffline(userId, ws)

	ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	for {
		_, bytes, err := ws.ReadMessage()
		if err != nil { break }
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))

		if string(bytes) == "PING" {
			ws.WriteMessage(websocket.TextMessage, []byte("PONG"))
			continue
		}

		// 把前端传来的 JSON 直接丢给 Logic，由 Logic 解析 targetId
		_, _ = logicClient.PushMessage(context.Background(), &api.PushMsgReq{
			UserId:  userId,
			Content: bytes,
		})
	}
}

func syncOffline(userId string, ws *websocket.Conn) {
	key := "offline_msg:" + userId
	msgs, _ := rdb.ZRange(context.Background(), key, 0, -1).Result()
	if len(msgs) > 0 {
		fmt.Printf("🕒 补偿 %s 的历史消息\n", userId)
		for _, m := range msgs {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(m))
		}
		rdb.Del(context.Background(), key)
	}
}