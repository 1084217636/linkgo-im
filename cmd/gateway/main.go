package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)
// 全局变量区：系统的“地基”
var (
    rdb         *redis.Client           // Redis 客户端：用于存储路由信息和订阅消息推送
    logicClient api.LogicClient         // gRPC 客户端：Gateway 用它来“遥控” Logic 服务
    clients     = make(map[string]*websocket.Conn) // 连接池：Key 是用户ID，Value 是长连接对象
    mutex       sync.RWMutex            // 读写锁：保证多个协程同时操作 clients map 时不崩溃
    upgrader    = websocket.Upgrader{   // 升级器：负责把普通的 HTTP 请求变成 WebSocket 长连接
        CheckOrigin: func(r *http.Request) bool { return true }, // 允许跨域（面试常问）
    }
)

// Cors 跨域中间件
func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type,AccessToken,X-CSRF-Token, Authorization, Token")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Header("Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")
		c.Header("Access-Control-Allow-Credentials", "true")

		// 处理浏览器的 OPTIONS 预检请求
		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
		}
		c.Next()
	}
}

func main() {
	// 1. 初始化 Redis (注意: 如果是 Docker 运行，地址可能需要改为 redis:6379)
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "123456",
		DB:       0,
	})

	// 2. 连接 Logic RPC (注意: 如果是 Docker 运行，地址改为 logic:9001)
	//NewClient 是非阻塞的，它会立即返回一个连接对象，即使服务端还没启动，它也会在后台静默重试。
	conn, err := grpc.NewClient("logic:9001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// conn, err := grpc.Dial("logic:9001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 Logic 服务失败: %v", err)
	}
	defer conn.Close()
	logicClient = api.NewLogicClient(conn)

	// 3. 启动后台订阅
	go subscribeMessages()

	// 4. 配置 Gin
	router := gin.Default()
	router.Use(Cors()) // 使用跨域中间件

	v1 := router.Group("/api/v1")
	{
		// 新增：登录接口 (补全前端缺失的路由)
		v1.POST("/login", func(c *gin.Context) {
			var req struct {
				UserID string `json:"user_id"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "参数错误"})
				return
			}
			// 这里简单模拟：直接返回一个 Token 即可
			c.JSON(200, gin.H{
				"token":   "mock_token_" + req.UserID,
				"user_id": req.UserID,
			})
		})

		// 拉取历史记录
		v1.GET("/history", func(c *gin.Context) {
			userId := c.Query("user_id")
			targetId := c.Query("target_id")
			reply, err := logicClient.GetHistory(context.Background(), &api.GetHistoryReq{
				UserId:   userId,
				TargetId: targetId,
			})
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"data": reply.Messages})
		})

		// 在 v1 路由组内修改和添加
		v1.POST("/group/create", func(c *gin.Context) {
			var req struct {
				GroupID string   `json:"group_id"`
				Members []string `json:"members"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "参数错误"})
				return
			}
			ctx := context.Background()
			for _, m := range req.Members {
				// 1. 记录群里有哪些人
				rdb.SAdd(ctx, "group_members:"+req.GroupID, m)
				// 2. 核心修改：记录这个人加入了哪些群（用于同步）
				rdb.SAdd(ctx, "user_groups:"+m, req.GroupID)
			}
			c.JSON(200, gin.H{"msg": "群组创建成功", "group_id": req.GroupID})
		})

		// 新增：获取用户所属的所有群聊
		v1.GET("/user/groups", func(c *gin.Context) {
			uid := c.Query("user_id")
			if uid == "" {
				c.JSON(400, gin.H{"error": "缺少 user_id"})
				return
			}
			groups, _ := rdb.SMembers(context.Background(), "user_groups:"+uid).Result()
			c.JSON(200, gin.H{"groups": groups})
		})
	}

	// WebSocket 接口
	router.GET("/ws", handleWebSocket)

	fmt.Println("🚀 [Gateway] 启动成功，监听端口 :8090")
	router.Run(":8090")
}

// WebSocket 处理及订阅函数保持不变...
func handleWebSocket(c *gin.Context) {
	userId := c.Query("user_id")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil { return }
	defer conn.Close()

	mutex.Lock()
	clients[userId] = conn
	mutex.Unlock()
	rdb.Set(context.Background(), "route:"+userId, "gateway_1", 24*time.Hour)

	defer func() {
		mutex.Lock()
		delete(clients, userId)
		mutex.Unlock()
		rdb.Del(context.Background(), "route:"+userId)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil { break }
		logicClient.PushMessage(context.Background(), &api.PushMsgReq{
			UserId:  userId,
			Content: msg,
		})
	}
}

func subscribeMessages() {
	ctx := context.Background()
	pubsub := rdb.Subscribe(ctx, "im_message_push")
	defer pubsub.Close()
	for msg := range pubsub.Channel() {
		parts := strings.SplitN(msg.Payload, ":", 2)
		if len(parts) == 2 {
			targetId, content := parts[0], parts[1]
			mutex.RLock()
			conn, ok := clients[targetId]
			mutex.RUnlock()
			if ok {
				conn.WriteMessage(websocket.TextMessage, []byte(content))
			}
		}
	}
}