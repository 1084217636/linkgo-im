package server

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// SyncOfflineMessages 对应简历：基于 ZSet 的时序同步
func SyncOfflineMessages(ctx context.Context, rdb *redis.Client, uid string, conn *ClientConn) {
	key := fmt.Sprintf("pending_ack:%s", uid)

	msgs, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil || len(msgs) == 0 {
		return
	}

	fmt.Printf("sync offline messages for user=%s count=%d\n", uid, len(msgs))

	for _, m := range msgs {
		payload, err := base64.StdEncoding.DecodeString(m)
		if err != nil {
			continue
		}
		_ = conn.WriteBinary(payload)
	}
}
