package logic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/1084217636/linkgo-im/api"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type clientMessageRecord struct {
	MessageID   string `json:"message_id"`
	ClientMsgID string `json:"client_msg_id"`
	SessionID   string `json:"session_id"`
	Seq         int64  `json:"seq"`
	TraceID     string `json:"trace_id"`
	Status      string `json:"status"`
}

func (h *LogicHandler) reserveClientMessage(ctx context.Context, frame *api.WireMessage) (bool, error) {
	if frame.ClientMsgId == "" {
		return false, nil
	}
	if h.Rdb == nil {
		return false, fmt.Errorf("redis is required for client message idempotency")
	}
	key := clientMessageKey(frame.From, frame.ClientMsgId)
	value, err := h.Rdb.Get(ctx, key).Result()
	if err == nil {
		if value == clientMessagePending {
			return false, ErrClientMessageInFlight
		}
		var record clientMessageRecord
		if json.Unmarshal([]byte(value), &record) == nil && record.Status == "pending" && record.Seq > 0 {
			if record.SessionID != "" && frame.To != "" && record.SessionID != buildSessionID(frame.From, frame.To, frame.ToType) {
				return false, fmt.Errorf("client_msg_id is already reserved for another conversation")
			}
			frame.SessionId = record.SessionID
			frame.Seq = record.Seq
			frame.MessageId = record.MessageID
			frame.TraceId = valueOrDefault(frame.TraceId, record.TraceID)
			return false, nil
		}
		logx.Infow("duplicate client message ignored",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("client_msg_id", frame.ClientMsgId),
			logx.Field("sender_id", frame.From),
		)
		return true, nil
	}
	if err != redis.Nil {
		return false, err
	}
	ok, err := h.Rdb.SetNX(ctx, key, clientMessagePending, clientMessagePendingTTL).Result()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrClientMessageInFlight
	}
	return false, nil
}

func (h *LogicHandler) commitClientMessage(ctx context.Context, frame *api.WireMessage) {
	if frame.ClientMsgId == "" {
		return
	}
	if h.Rdb == nil {
		return
	}
	record, err := json.Marshal(clientMessageRecord{
		MessageID:   frame.MessageId,
		ClientMsgID: frame.ClientMsgId,
		SessionID:   frame.SessionId,
		Seq:         frame.Seq,
		TraceID:     frame.TraceId,
		Status:      "committed",
	})
	if err != nil {
		return
	}
	_ = h.Rdb.Set(ctx, clientMessageKey(frame.From, frame.ClientMsgId), record, clientMessageIDTTL).Err()
}

func (h *LogicHandler) commitClientMessageReservation(ctx context.Context, frame *api.WireMessage) {
	if frame == nil || frame.ClientMsgId == "" || h.Rdb == nil || frame.Seq <= 0 {
		return
	}
	record, err := json.Marshal(clientMessageRecord{
		MessageID:   frame.MessageId,
		ClientMsgID: frame.ClientMsgId,
		SessionID:   frame.SessionId,
		Seq:         frame.Seq,
		TraceID:     frame.TraceId,
		Status:      "pending",
	})
	if err != nil {
		return
	}
	_ = h.Rdb.SetXX(ctx, clientMessageKey(frame.From, frame.ClientMsgId), record, clientMessagePendingTTL).Err()
}

func (h *LogicHandler) releaseClientMessage(ctx context.Context, frame *api.WireMessage) {
	if frame.ClientMsgId == "" {
		return
	}
	if h.Rdb == nil {
		return
	}
	key := clientMessageKey(frame.From, frame.ClientMsgId)
	value, err := h.Rdb.Get(ctx, key).Result()
	if err == nil {
		pending := value == clientMessagePending
		if !pending {
			var record clientMessageRecord
			pending = json.Unmarshal([]byte(value), &record) == nil && record.Status == "pending"
		}
		if pending {
			_ = h.Rdb.Del(ctx, key).Err()
		}
	}
}

func clientMessageKey(uid, clientMsgID string) string {
	return "client_msg:" + uid + ":" + clientMsgID
}
