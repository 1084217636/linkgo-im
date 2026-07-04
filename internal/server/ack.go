package server

import (
	"context"
	"encoding/base64"
	"strconv"
	"time"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const conversationReadTTL = 30 * 24 * time.Hour

var markConversationReadScript = redis.NewScript(`
local current = redis.call("HGET", KEYS[1], ARGV[1])
if (not current) or tonumber(current) < tonumber(ARGV[2]) then
  redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
end
redis.call("EXPIRE", KEYS[1], ARGV[3])
return 1
`)

func AckMessage(ctx context.Context, rdb *redis.Client, uid, messageID string) {
	if messageID == "" {
		return
	}

	exists, err := rdb.HExists(ctx, AckIndexKey(uid), messageID).Result()
	if err != nil {
		metrics.AckOperations.WithLabelValues("lookup_error").Inc()
		return
	}
	if !exists {
		metrics.AckOperations.WithLabelValues("miss").Inc()
		return
	}

	markReadFromAck(ctx, rdb, uid, messageID)

	if err := rdb.ZRem(ctx, PendingAckKey(uid), messageID).Err(); err != nil {
		logx.Errorw("remove pending ack failed",
			logx.Field("target_id", uid),
			logx.Field("message_id", messageID),
			logx.Field("error", err.Error()),
		)
		metrics.AckOperations.WithLabelValues("pending_remove_error").Inc()
	}
	if err := rdb.ZRem(ctx, OfflineMessageKey(uid), messageID).Err(); err != nil {
		logx.Errorw("remove offline ack failed",
			logx.Field("target_id", uid),
			logx.Field("message_id", messageID),
			logx.Field("error", err.Error()),
		)
		metrics.AckOperations.WithLabelValues("offline_remove_error").Inc()
	}
	if err := rdb.HDel(ctx, AckIndexKey(uid), messageID).Err(); err != nil {
		logx.Errorw("delete ack index failed",
			logx.Field("target_id", uid),
			logx.Field("message_id", messageID),
			logx.Field("error", err.Error()),
		)
		metrics.AckOperations.WithLabelValues("index_delete_error").Inc()
	}
	_ = rdb.HDel(ctx, AckRetryKey(uid), messageID).Err()
	metrics.AckOperations.WithLabelValues("success").Inc()
	logx.Infow("ack confirmed",
		logx.Field("target_id", uid),
		logx.Field("message_id", messageID),
	)
}

func markReadFromAck(ctx context.Context, rdb *redis.Client, uid, messageID string) {
	encoded, err := rdb.HGet(ctx, AckIndexKey(uid), messageID).Result()
	if err != nil {
		return
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return
	}
	frame := DecodeWireMessage(payload)
	if frame == nil || frame.SessionId == "" || frame.Seq <= 0 {
		return
	}
	if err := MarkConversationRead(ctx, rdb, uid, frame.SessionId, frame.Seq); err != nil {
		logx.Errorw("mark conversation read failed",
			logx.Field("target_id", uid),
			logx.Field("message_id", messageID),
			logx.Field("session_id", frame.SessionId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
	}
}

func MarkConversationRead(ctx context.Context, rdb *redis.Client, uid, conversationID string, seq int64) error {
	if rdb == nil || uid == "" || conversationID == "" || seq <= 0 {
		return nil
	}
	return markConversationReadScript.Run(ctx, rdb, []string{UserConversationReadKey(uid)},
		conversationID,
		strconv.FormatInt(seq, 10),
		strconv.FormatInt(int64(conversationReadTTL.Seconds()), 10),
	).Err()
}
