package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

const messageReplayTTL = 7 * 24 * time.Hour

const (
	syncBatchSize       = 200
	maxMySQLReplayCount = 1000
)

// SyncOfflineMessages 先按 pending_ack 回放未 ACK 消息，再按会话 timeline 和 last_seq 补齐近期消息。
func SyncOfflineMessages(ctx context.Context, rdb *redis.Client, uid string, conn binaryWriter, sessionID string, lastSeq int64) error {
	return SyncOfflineMessagesWithDB(ctx, rdb, nil, uid, conn, sessionID, lastSeq)
}

func SyncOfflineMessagesWithDB(ctx context.Context, rdb *redis.Client, db *sql.DB, uid string, conn binaryWriter, sessionID string, lastSeq int64) error {
	replayed := make(map[string]struct{})
	if err := syncPendingMessages(ctx, rdb, uid, conn, replayed); err != nil {
		if db == nil {
			return err
		}
		logx.Errorw("redis pending replay unavailable, continue with mysql cursor",
			logx.Field("target_id", uid),
			logx.Field("error", err.Error()),
		)
	}
	return SyncSessionMessagesAfterSeqWithDB(ctx, rdb, db, uid, conn, sessionID, lastSeq, replayed)
}

func syncPendingMessages(ctx context.Context, rdb *redis.Client, uid string, conn binaryWriter, replayed map[string]struct{}) error {
	msgs, err := rdb.ZRange(ctx, PendingAckKey(uid), 0, -1).Result()
	if err != nil || len(msgs) == 0 {
		return err
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
		if err := conn.WriteBinary(payload); err != nil {
			return err
		}
		replayed[messageID] = struct{}{}
	}
	return nil
}

func SyncSessionMessagesAfterSeq(ctx context.Context, rdb *redis.Client, uid string, conn binaryWriter, sessionID string, lastSeq int64, replayed map[string]struct{}) error {
	_, err := syncSessionMessagesAfterSeq(ctx, rdb, uid, conn, sessionID, lastSeq, replayed)
	return err
}

func SyncSessionMessagesAfterSeqWithDB(ctx context.Context, rdb *redis.Client, db *sql.DB, uid string, conn binaryWriter, sessionID string, lastSeq int64, replayed map[string]struct{}) error {
	if replayed == nil {
		replayed = make(map[string]struct{})
	}
	_, err := syncSessionMessagesAfterSeq(ctx, rdb, uid, conn, sessionID, lastSeq, replayed)
	if err != nil {
		if db == nil {
			return err
		}
		logx.Errorw("redis timeline replay unavailable, continue with mysql cursor",
			logx.Field("target_id", uid),
			logx.Field("session_id", sessionID),
			logx.Field("error", err.Error()),
		)
	}
	if db == nil || sessionID == "" || lastSeq < 0 {
		return nil
	}
	return syncMySQLMessagesAfterSeq(ctx, db, uid, conn, sessionID, lastSeq, replayed)
}

func syncSessionMessagesAfterSeq(ctx context.Context, rdb *redis.Client, uid string, conn binaryWriter, sessionID string, lastSeq int64, replayed map[string]struct{}) (int, error) {
	if sessionID == "" || lastSeq < 0 {
		return 0, nil
	}
	if replayed == nil {
		replayed = make(map[string]struct{})
	}
	key := SessionTimelineKey(sessionID)
	msgs, err := rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    fmt.Sprintf("(%d", lastSeq),
		Max:    "+inf",
		Offset: 0,
		Count:  syncBatchSize,
	}).Result()
	if err != nil || len(msgs) == 0 {
		return 0, err
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
		if err := conn.WriteBinary(payload); err != nil {
			return 0, err
		}
		replayed[messageID] = struct{}{}
		metrics.ReconnectReplay.WithLabelValues("redis").Inc()
	}
	return len(msgs), nil
}

func syncMySQLMessagesAfterSeq(ctx context.Context, db *sql.DB, uid string, conn binaryWriter, sessionID string, lastSeq int64, replayed map[string]struct{}) error {
	cursor := lastSeq
	count := 0
	for count < maxMySQLReplayCount {
		rows, err := db.QueryContext(ctx, `
SELECT message_id, client_msg_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ? AND seq > ?
ORDER BY seq ASC
LIMIT ?
`, sessionID, cursor, syncBatchSize)
		if err != nil {
			return err
		}
		batch := 0
		maxSeq := cursor
		for rows.Next() {
			var msg api.WireMessage
			var body string
			if err := rows.Scan(&msg.MessageId, &msg.ClientMsgId, &msg.SessionId, &msg.Seq, &msg.From, &msg.To, &msg.ToType, &body, &msg.SentAt); err != nil {
				rows.Close()
				return err
			}
			batch++
			if msg.Seq > maxSeq {
				maxSeq = msg.Seq
			}
			if _, ok := replayed[msg.MessageId]; ok {
				continue
			}
			msg.Body = body
			msg.MsgType = api.MsgType_NORMAL
			payload, err := proto.Marshal(&msg)
			if err != nil {
				rows.Close()
				return err
			}
			if err := conn.WriteBinary(payload); err != nil {
				rows.Close()
				return err
			}
			replayed[msg.MessageId] = struct{}{}
			count++
			metrics.ReconnectReplay.WithLabelValues("mysql").Inc()
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if batch == 0 || maxSeq <= cursor || batch < syncBatchSize {
			break
		}
		cursor = maxSeq
	}
	if count > 0 {
		logx.Infow("sync messages from mysql fallback",
			logx.Field("target_id", uid),
			logx.Field("session_id", sessionID),
			logx.Field("last_seq", lastSeq),
			logx.Field("count", count),
		)
	}
	return nil
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
