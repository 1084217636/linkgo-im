package server

import (
	"sync"
	"github.com/gorilla/websocket"
	"context"
    "fmt"
    "strings"
    "github.com/redis/go-redis/v9"
)

// ClientManager 对应简历：连接池管理
type ClientManager struct {
	UserConns sync.Map     // 映射关系：UserId -> *websocket.Conn
	Lock      sync.RWMutex // 保护并发安全
}

var Manager = &ClientManager{}

func (m *ClientManager) Add(uid string, conn *websocket.Conn) {
	m.UserConns.Store(uid, conn)
}

func (m *ClientManager) Remove(uid string) {
	m.UserConns.Delete(uid)
}

// GetConn 获取指定用户的连接
func (m *ClientManager) GetConn(uid string) (*websocket.Conn, bool) {
	val, ok := m.UserConns.Load(uid)
	if !ok {
		return nil, false
	}
	return val.(*websocket.Conn), true
}
// internal/server/manager.go



// SubscribeRedis 修正：将接收者改为 *ClientManager
// 对应简历：利用 Redis Pub/Sub 实现多节点间的消息广播
func (m *ClientManager) SubscribeRedis(ctx context.Context, rdb *redis.Client) {
    pubsub := rdb.Subscribe(ctx, "im_message_push")
    defer pubsub.Close()

    ch := pubsub.Channel()
    fmt.Println("📥 [Gateway] 已开启 Redis 订阅，准备接收跨节点消息...")

    for msg := range ch {
        // 1. 解析消息：假设格式为 "targetId:content"
        parts := strings.SplitN(msg.Payload, ":", 2)
        if len(parts) < 2 {
            continue
        }
        targetId := parts[0]
        content := parts[1]

        // 2. 检查用户是否在本网关节点
        if conn, ok := m.GetConn(targetId); ok {
            // 3. 如果在，直接通过 WebSocket 推送
            err := conn.WriteMessage(websocket.TextMessage, []byte(content))
            if err != nil {
                fmt.Printf("❌ 推送给用户 %s 失败: %v\n", targetId, err)
                m.Remove(targetId)
            } else {
                fmt.Printf("✅ 跨节点消息已成功推送到本地用户: %s\n", targetId)
            }
        }
    }
}