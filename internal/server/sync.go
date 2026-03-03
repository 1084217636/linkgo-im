package server

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// SyncOfflineMessages 对应简历：基于 ZSet 的时序同步
func SyncOfflineMessages(ctx context.Context, rdb *redis.Client, uid string, conn *websocket.Conn) {
	key := fmt.Sprintf("offline_msg:%s", uid)
	
	// 从 ZSet 按照时间戳从小到大拉取所有消息
	msgs, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil || len(msgs) == 0 {
		return
	}

	fmt.Printf("🕒 正在为用户 %s 推送 %d 条离线消息...\n", uid, len(msgs))

	for _, m := range msgs {
		// 推送消息给客户端
		_ = conn.WriteMessage(websocket.TextMessage, []byte(m))
	}

	// 对应简历：降低无效资源占用
	// 同步完后删除，防止 Redis 内存堆积
	rdb.Del(ctx, key)
}