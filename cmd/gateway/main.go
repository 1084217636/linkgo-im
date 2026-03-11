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






func main() {
	// 1. 初始化
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "123456",
		DB:       0,
	})

	conn, err := grpc.NewClient("logic:9001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 Logic 失败: %v", err)
	}
	defer conn.Close()
	logicClient = api.NewLogicClient(conn)

	go subscribeMessages()

	// 2. 配置路由
	router := gin.Default()
	router.Use(Cors())

	// V1 标准路由组
	v1 := router.Group("/api/v1")
	{
		// 【修改后的标准登录逻辑】
		v1.POST("/login", func(c *gin.Context) {
			var req struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "参数错误"})
				return
			}

			// 调用 Logic 服务进行数据库验证
			reply, err := logicClient.Login(context.Background(), &api.LoginReq{
				Username: req.Username,
				Password: req.Password,
			})

			if err != nil {
				c.JSON(401, gin.H{"error": "登录失败: " + err.Error()})
				return
			}

			// 返回 Logic 生成的真实 Token
			c.JSON(200, gin.H{
				"token":   reply.Token,
				"user_id": reply.UserId,
			})
		})

		// 历史记录与群组管理（这些接口以后可以考虑加 .Use(middleware.Auth())）
		v1.GET("/history", handleHistory)
		v1.POST("/group/create", handleGroupCreate)
		v1.GET("/user/groups", handleUserGroups)
	}

	// WebSocket 接口（通常单独放，或者也放入带有鉴权中间件的组）
	router.GET("/ws", handleWebSocket)

	fmt.Println("🚀 [Gateway] 启动成功，监听端口 :8090")
	router.Run(":8090")
}

// --- 抽离出来的 Handler 函数，让 main 看起来更整洁 ---

func handleHistory(c *gin.Context) {
	userId := c.Query("user_id")
	targetId := c.Query("target_id")
	reply, err := logicClient.GetHistory(c.Request.Context(), &api.GetHistoryReq{
		UserId:   userId,
		TargetId: targetId,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"data": reply.Messages})
}

func handleGroupCreate(c *gin.Context) {
	var req struct {
		GroupID string   `json:"group_id"`
		Members []string `json:"members"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	for _, m := range req.Members {
		rdb.SAdd(c.Request.Context(), "group_members:"+req.GroupID, m)
		rdb.SAdd(c.Request.Context(), "user_groups:"+m, req.GroupID)
	}
	c.JSON(200, gin.H{"msg": "群组创建成功", "group_id": req.GroupID})
}

func handleUserGroups(c *gin.Context) {
	uid := c.Query("user_id")
	groups, _ := rdb.SMembers(c.Request.Context(), "user_groups:"+uid).Result()
	c.JSON(200, gin.H{"groups": groups})
}

func handleWebSocket(c *gin.Context) {
    userId := c.Query("user_id")
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil { return }
    defer conn.Close()

    mutex.Lock()
    clients[userId] = conn
    mutex.Unlock()

    // --- 修改点1: 初始有效期设短一点，比如 1 分钟 ---
    rdb.Set(context.Background(), "route:"+userId, "gateway_1", 60*time.Second) // 实际生产建议 60*time.Second

    defer func() {
        mutex.Lock()
        delete(clients, userId)
        mutex.Unlock()
        rdb.Del(context.Background(), "route:"+userId)
        fmt.Printf("❌ 用户 %s 已下线并清理路由\n", userId)
    }()

    for {
        _, msg, err := conn.ReadMessage()
        if err != nil { break }

        // --- 修改点2: 拦截前端发来的 PING ---
		fmt.Printf("收到消息内容: [%s], 长度: %d\n", string(msg), len(msg))
        if string(msg) == "PING" {
            // 收到心跳，为 Redis 里的路由信息续命 60 秒
            rdb.Expire(context.Background(), "route:"+userId, 60*time.Second)
            // 回复 PONG 让前端知道服务器还活着
            conn.WriteMessage(websocket.TextMessage, []byte("PONG"))
            continue
        }

        // 正常业务逻辑
        logicClient.PushMessage(context.Background(), &api.PushMsgReq{
            UserId:   userId,
            Content:  msg,
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