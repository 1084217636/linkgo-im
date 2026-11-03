package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/attachment"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/1084217636/linkgo-im/internal/ids"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

var ErrClientMessageInFlight = errors.New("client message is still processing")

const (
	clientMessageIDTTL      = 7 * 24 * time.Hour
	clientMessagePendingTTL = 5 * time.Minute
	clientMessagePending    = "__PENDING__"
	sequenceTTL             = 7 * 24 * time.Hour
	defaultHistoryLimit     = 50
	maxHistoryLimit         = 100
)

type GroupDispatcher interface {
	PublishGroupDispatch(ctx context.Context, frame *api.WireMessage, recipients []string) error
}

type LogicHandler struct {
	api.UnimplementedLogicServer

	Rdb             *redis.Client
	DB              *sql.DB
	Delivery        *delivery.RedisDelivery
	GroupDispatcher GroupDispatcher
	BotResponder    BotResponder
	// ConversationOutbox enables atomic message + conversation-summary event
	// persistence. It is enabled by the logic service after the migration is
	// applied; tests and legacy callers can keep the direct fallback.
	ConversationOutbox bool
	// DeliveryOutbox makes durable acceptance independent from Redis/Kafka
	// availability. The message and its delivery event commit atomically; a
	// worker performs the external side effect with at-least-once semantics.
	DeliveryOutbox bool
}

func (h *LogicHandler) PushMessage(ctx context.Context, req *api.PushMsgReq) (*api.PushMsgReply, error) {
	var frame api.WireMessage
	if err := proto.Unmarshal(req.Content, &frame); err != nil {
		metrics.InboundMessages.WithLabelValues("logic", "decode_error").Inc()
		return nil, fmt.Errorf("invalid protobuf payload: %w", err)
	}
	if frame.MsgType == api.MsgType_HEARTBEAT || frame.MsgType == api.MsgType_ACK {
		metrics.InboundMessages.WithLabelValues("logic", "control").Inc()
		return &api.PushMsgReply{}, nil
	}
	metrics.InboundMessages.WithLabelValues("logic", "normal").Inc()

	if err := normalizeFrame(req.UserId, &frame); err != nil {
		return nil, err
	}
	if frame.TraceId == "" {
		frame.TraceId = ids.NewTraceID()
	}
	if frame.ClientMsgId == "" {
		frame.ClientMsgId = frame.MessageId
	}
	if frame.ClientMsgId == "" {
		return nil, fmt.Errorf("client_msg_id is required")
	}
	if duplicate, err := h.reserveClientMessage(ctx, &frame); err != nil {
		return nil, err
	} else if duplicate {
		metrics.InboundMessages.WithLabelValues("logic", "duplicate").Inc()
		return &api.PushMsgReply{}, nil
	}
	if existing, ok, err := h.loadMessageByClientMsgID(ctx, frame.From, frame.ClientMsgId); err != nil {
		h.releaseClientMessage(ctx, &frame)
		return nil, err
	} else if ok {
		if existing.TraceId == "" {
			existing.TraceId = frame.TraceId
		}
		if err := h.validateSendPermission(ctx, existing); err != nil {
			h.releaseClientMessage(ctx, existing)
			return nil, err
		}
		if h.DeliveryOutbox {
			if _, err := h.ProcessDeliveryOutboxMessage(ctx, existing.MessageId); err != nil {
				logx.Errorw("delivery outbox immediate retry failed",
					logx.Field("message_id", existing.MessageId),
					logx.Field("error", err.Error()))
			}
		} else if err := h.deliverPersistedMessage(ctx, existing); err != nil {
			h.releaseClientMessage(ctx, existing)
			return nil, err
		}
		h.commitClientMessage(ctx, existing)
		metrics.InboundMessages.WithLabelValues("logic", "duplicate").Inc()
		return &api.PushMsgReply{}, nil
	}

	if frame.SessionId == "" {
		frame.SessionId = buildSessionID(frame.From, frame.To, frame.ToType)
	}
	if err := h.validateSendPermission(ctx, &frame); err != nil {
		h.releaseClientMessage(ctx, &frame)
		return nil, err
	}
	if h.DB != nil {
		dir := os.Getenv("LINKGO_UPLOAD_DIR")
		if dir == "" {
			dir = "./uploads"
		}
		canonical, err := (&attachment.Store{DB: h.DB, Dir: dir}).CanonicalizeMessage(ctx, frame.From, frame.To, frame.Body)
		if err != nil {
			h.releaseClientMessage(ctx, &frame)
			return nil, err
		}
		frame.Body = canonical
	}
	if frame.Seq <= 0 {
		seq, err := h.nextSequence(ctx, frame.SessionId)
		if err != nil {
			h.releaseClientMessage(ctx, &frame)
			return nil, err
		}
		frame.Seq = seq
		metrics.MessageSeqAllocated.Inc()
	}
	frame.SentAt = time.Now().UnixMilli()
	if frame.MessageId == "" {
		frame.MessageId = fmt.Sprintf("%s-%d", frame.SessionId, frame.Seq)
	}
	h.commitClientMessageReservation(ctx, &frame)

	recipients, err := h.resolveRecipients(ctx, &frame)
	if err != nil {
		h.releaseClientMessage(ctx, &frame)
		return nil, err
	}
	persisted, err := h.saveMessageWithRecipients(ctx, &frame, recipients)
	if err != nil {
		h.releaseClientMessage(ctx, &frame)
		return nil, err
	}
	if !persisted {
		// saveMessage replaces frame with the canonical DB row when a concurrent
		// insert wins the client_msg_id unique-key race. Re-authorize that row,
		// rather than relying on the check performed for the submitted frame.
		if err := h.validateSendPermission(ctx, &frame); err != nil {
			h.releaseClientMessage(ctx, &frame)
			return nil, err
		}
	}
	if h.DeliveryOutbox {
		// The durable message is accepted once the transaction commits. Failure
		// of the immediate attempt leaves the Outbox event pending for a worker;
		// returning an error here would make the client retry an accepted fact.
		if _, err := h.ProcessDeliveryOutboxMessage(ctx, frame.MessageId); err != nil {
			logx.Errorw("delivery outbox immediate dispatch failed",
				logx.Field("trace_id", frame.TraceId),
				logx.Field("message_id", frame.MessageId),
				logx.Field("error", err.Error()))
		}
	} else if err := h.deliverMessageToRecipients(ctx, &frame, recipients); err != nil {
		h.releaseClientMessage(ctx, &frame)
		return nil, err
	}

	h.commitClientMessage(ctx, &frame)
	if !persisted {
		metrics.InboundMessages.WithLabelValues("logic", "duplicate").Inc()
		return &api.PushMsgReply{}, nil
	}
	logx.Infow("logic accepted message",
		logx.Field("trace_id", frame.TraceId),
		logx.Field("message_id", frame.MessageId),
		logx.Field("client_msg_id", frame.ClientMsgId),
		logx.Field("seq", frame.Seq),
		logx.Field("target_id", frame.To),
		logx.Field("to_type", frame.ToType),
	)
	h.triggerBotResponse(&frame)
	return &api.PushMsgReply{}, nil
}

func (h *LogicHandler) deliverPersistedMessage(ctx context.Context, frame *api.WireMessage) error {
	recipients, err := h.resolveRecipients(ctx, frame)
	if err != nil {
		return err
	}
	return h.deliverMessageToRecipients(ctx, frame, recipients)
}

func (h *LogicHandler) deliverMessageToRecipients(ctx context.Context, frame *api.WireMessage, recipients []string) error {
	if frame.ToType == "group" {
		metrics.GroupDispatchMembers.Observe(float64(len(recipients)))
	}
	payload, err := proto.Marshal(frame)
	if err != nil {
		return err
	}

	if frame.ToType == "group" && h.GroupDispatcher != nil {
		if err := h.GroupDispatcher.PublishGroupDispatch(ctx, frame, recipients); err != nil {
			return err
		}
		server.RememberSessionMessage(ctx, h.Rdb, frame, payload)
	} else {
		if err := h.dispatchToRecipients(ctx, frame, recipients, payload); err != nil {
			return err
		}
		server.RememberSessionMessage(ctx, h.Rdb, frame, payload)
	}

	h.updateConversationState(ctx, frame, recipients)
	return nil
}

func normalizeFrame(sender string, frame *api.WireMessage) error {
	if frame.From == "" {
		frame.From = sender
	}
	if frame.From != sender {
		return fmt.Errorf("sender mismatch")
	}
	if frame.To == "" || frame.Body == "" {
		return fmt.Errorf("to and body are required")
	}
	if frame.ToType == "" {
		frame.ToType = targetType(frame.To)
	}
	if frame.ToType != "user" && frame.ToType != "group" {
		return fmt.Errorf("unsupported to_type")
	}
	frame.MsgType = api.MsgType_NORMAL
	return nil
}

func (h *LogicHandler) resolveRecipients(ctx context.Context, frame *api.WireMessage) ([]string, error) {
	if frame.ToType == "user" {
		return []string{frame.To}, nil
	}

	if h.DB == nil {
		return nil, errors.New("group membership store is unavailable")
	}
	recipients, err := h.loadGroupRecipientsFromDB(ctx, frame.To, frame.From)
	if err != nil {
		return nil, err
	}
	return recipients, nil
}

func (h *LogicHandler) dispatchToRecipients(ctx context.Context, frame *api.WireMessage, recipients []string, payload []byte) error {
	for _, recipient := range recipients {
		if err := h.Delivery.Deliver(ctx, recipient, frame.MessageId, payload, frame.SentAt); err != nil {
			return err
		}
	}
	return nil
}
