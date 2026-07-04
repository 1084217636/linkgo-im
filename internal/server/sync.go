package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

const messageReplayTTL = 7 * 24 * time.Hour

// SyncOfflineMessages 先按 pending_ack 回放未 ACK 消息，再按会话 timeline 和 last_seq 补齐近期消息。
func SyncOfflineMessages(ctx context.Context, rdb *redis.Client, uid string, conn *ClientConn, sessionID string, lastSeq int64) {
	replayed := make(map[string]struct{})
	syncPendingMessages(ctx, rdb, uid, conn, replayed)
	SyncSessionMessagesAfterSeq(ctx, rdb, uid, conn, sessionID, lastSeq, replayed)
}

func syncPendingMessages(ctx context.Context, rdb *redis.Client, uid string, conn *ClientConn, replayed map[string]struct{}) {
	msgs, err := rdb.ZRange(ctx, PendingAckKey(uid), 0, -1).Result()
	if err != nil || len(msgs) == 0 {
		return
	}

	logx.Infow("sync pending messages",
		logx.Field("target_id", uid),
		logx.Field("count", len(msgs)),
	)

	for _, messageID := range msgs {
		encoded, err := rdb.HGet(ctx, AckIndexKey(uid), messageID).Result()
		if err != nil {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		if err := conn.WriteBinary(payload); err == nil {
			replayed[messageID] = struct{}{}
		}
	}
}

func SyncSessionMessagesAfterSeq(ctx context.Context, rdb *redis.Client, uid string, conn *ClientConn, sessionID string, lastSeq int64, replayed map[string]struct{}) {
	if sessionID == "" || lastSeq < 0 {
		return
	}
	key := SessionTimelineKey(sessionID)
	msgs, err := rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    fmt.Sprintf("(%d", lastSeq),
		Max:    "+inf",
		Offset: 0,
		Count:  200,
	}).Result()
	if err != nil || len(msgs) == 0 {
		return
	}

	logx.Infow("sync messages after last_seq",
		logx.Field("target_id", uid),
		logx.Field("session_id", sessionID),
		logx.Field("last_seq", lastSeq),
		logx.Field("count", len(msgs)),
	)

	for _, messageID := range msgs {
		if _, ok := replayed[messageID]; ok {
			continue
		}
		encoded, err := rdb.Get(ctx, MessagePayloadKey(messageID)).Result()
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

func RememberSessionMessage(ctx context.Context, rdb *redis.Client, frame *api.WireMessage, payload []byte) {
	if rdb == nil || frame == nil || frame.MessageId == "" || frame.SessionId == "" {
		return
	}

	encoded := base64.StdEncoding.EncodeToString(payload)
	pipe := rdb.TxPipeline()
	pipe.SetNX(ctx, MessagePayloadKey(frame.MessageId), encoded, messageReplayTTL)
	pipe.Expire(ctx, MessagePayloadKey(frame.MessageId), messageReplayTTL)
	pipe.ZAddArgs(ctx, SessionTimelineKey(frame.SessionId), redis.ZAddArgs{
		NX: true,
		Members: []redis.Z{{
			Score:  float64(frame.Seq),
			Member: frame.MessageId,
		}},
	})
	pipe.Expire(ctx, SessionTimelineKey(frame.SessionId), messageReplayTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logx.Errorw("remember session message failed",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("message_id", frame.MessageId),
			logx.Field("session_id", frame.SessionId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
	}
}

func DecodeWireMessage(payload []byte) *api.WireMessage {
	var frame api.WireMessage
	if err := proto.Unmarshal(payload, &frame); err != nil {
		return nil
	}
	return &frame
}
