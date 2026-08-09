package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strconv"
	"time"

	"github.com/1084217636/linkgo-im/api"
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

var acknowledgeMessageScript = redis.NewScript(`
if redis.call("HEXISTS", KEYS[3], ARGV[1]) == 0 then
  return 0
end
redis.call("ZREM", KEYS[1], ARGV[1])
redis.call("ZREM", KEYS[2], ARGV[1])
redis.call("HDEL", KEYS[3], ARGV[1])
redis.call("HDEL", KEYS[4], ARGV[1])
return 1
`)

func AckMessage(ctx context.Context, rdb *redis.Client, uid, messageID string) {
	AckMessageWithDB(ctx, rdb, nil, uid, messageID)
}

func AckMessageWithDB(ctx context.Context, rdb *redis.Client, db *sql.DB, uid, messageID string) {
	if messageID == "" {
		return
	}

	frame := ackFrameFromIndex(ctx, rdb, uid, messageID)
	if frame != nil {
		if err := MarkConversationAcked(ctx, rdb, uid, frame.SessionId, frame.Seq); err != nil {
			logx.Errorw("mark conversation acked cursor failed",
				logx.Field("target_id", uid),
				logx.Field("message_id", messageID),
				logx.Field("session_id", frame.SessionId),
				logx.Field("seq", frame.Seq),
				logx.Field("error", err.Error()),
			)
		}
	}

	removed, err := acknowledgeMessageScript.Run(ctx, rdb, []string{
		PendingAckKey(uid),
		OfflineMessageKey(uid),
		AckIndexKey(uid),
		AckRetryKey(uid),
	}, messageID).Int64()
	if err != nil {
		logx.Errorw("atomically acknowledge message failed",
			logx.Field("target_id", uid),
			logx.Field("message_id", messageID),
			logx.Field("error", err.Error()),
		)
		metrics.AckOperations.WithLabelValues("cleanup_error").Inc()
		return
	}
	if removed == 0 {
		metrics.AckOperations.WithLabelValues("miss").Inc()
		return
	}
	metrics.AckOperations.WithLabelValues("success").Inc()
	if db != nil && frame != nil && frame.SessionId != "" && frame.Seq > 0 {
		if _, err := db.ExecContext(ctx, `
UPDATE conversation_members
SET acked_seq = GREATEST(acked_seq, ?)
WHERE conversation_id = ? AND user_id = ?
`, frame.Seq, frame.SessionId, uid); err != nil {
			logx.Errorw("persist conversation acked cursor failed",
				logx.Field("target_id", uid),
				logx.Field("session_id", frame.SessionId),
				logx.Field("seq", frame.Seq),
				logx.Field("error", err.Error()),
			)
		}
	}
	logx.Infow("ack confirmed",
		logx.Field("target_id", uid),
		logx.Field("message_id", messageID),
	)
}

func ackFrameFromIndex(ctx context.Context, rdb *redis.Client, uid, messageID string) *api.WireMessage {
	encoded, err := rdb.HGet(ctx, AckIndexKey(uid), messageID).Result()
	if err != nil {
		return nil
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	frame := DecodeWireMessage(payload)
	if frame == nil || frame.SessionId == "" || frame.Seq <= 0 {
		return nil
	}
	return frame
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

func MarkConversationAcked(ctx context.Context, rdb *redis.Client, uid, conversationID string, seq int64) error {
	if rdb == nil || uid == "" || conversationID == "" || seq <= 0 {
		return nil
	}
	return markConversationReadScript.Run(ctx, rdb, []string{UserConversationAckedKey(uid)},
		conversationID,
		strconv.FormatInt(seq, 10),
		strconv.FormatInt(int64(conversationReadTTL.Seconds()), 10),
	).Err()
}
