package server

import (
	"context"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

func AckMessage(ctx context.Context, rdb *redis.Client, uid, messageID string) {
	if messageID == "" {
		return
	}

	exists, err := rdb.HExists(ctx, "ack_idx:"+uid, messageID).Result()
	if err != nil {
		metrics.AckOperations.WithLabelValues("lookup_error").Inc()
		return
	}
	if !exists {
		metrics.AckOperations.WithLabelValues("miss").Inc()
		return
	}

	if err := rdb.ZRem(ctx, "pending_ack:"+uid, messageID).Err(); err != nil {
		logx.Errorf("remove pending ack failed user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("pending_remove_error").Inc()
	}
	if err := rdb.ZRem(ctx, "offline_msg:"+uid, messageID).Err(); err != nil {
		logx.Errorf("remove offline ack failed user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("offline_remove_error").Inc()
	}
	if err := rdb.HDel(ctx, "ack_idx:"+uid, messageID).Err(); err != nil {
		logx.Errorf("delete ack index failed user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("index_delete_error").Inc()
	}
	metrics.AckOperations.WithLabelValues("success").Inc()
}
