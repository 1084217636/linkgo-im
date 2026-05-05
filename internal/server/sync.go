package server

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

// SyncOfflineMessages 按 pending_ack ZSet 顺序回放未 ACK 消息。
func SyncOfflineMessages(ctx context.Context, rdb *redis.Client, uid string, conn *ClientConn) {
	key := fmt.Sprintf("pending_ack:%s", uid)

	msgs, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil || len(msgs) == 0 {
		return
	}

	logx.Infof("sync offline messages user=%s count=%d", uid, len(msgs))

	for _, messageID := range msgs {
		encoded, err := rdb.HGet(ctx, "ack_idx:"+uid, messageID).Result()
		if err != nil {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		_ = conn.WriteBinary(payload)
	}
}
