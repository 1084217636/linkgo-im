package server

import (
	"context"
	"log"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
)

func AckMessage(ctx context.Context, rdb *redis.Client, uid, messageID string) {
	if messageID == "" {
		return
	}

	payload, err := rdb.HGet(ctx, "ack_idx:"+uid, messageID).Result()
	if err != nil {
		metrics.AckOperations.WithLabelValues("miss").Inc()
		return
	}

	if err := rdb.ZRem(ctx, "pending_ack:"+uid, payload).Err(); err != nil {
		log.Printf("remove pending ack failed for user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("pending_remove_error").Inc()
	}
	if err := rdb.ZRem(ctx, "offline_msg:"+uid, payload).Err(); err != nil {
		log.Printf("remove offline ack failed for user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("offline_remove_error").Inc()
	}
	if err := rdb.HDel(ctx, "ack_idx:"+uid, messageID).Err(); err != nil {
		log.Printf("delete ack index failed for user=%s message=%s: %v", uid, messageID, err)
		metrics.AckOperations.WithLabelValues("index_delete_error").Inc()
	}
	metrics.AckOperations.WithLabelValues("success").Inc()
}
