package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
	"strings"
)

func (h *LogicHandler) saveMessage(ctx context.Context, frame *api.WireMessage) (bool, error) {
	return h.saveMessageWithRecipients(ctx, frame, nil)
}

func (h *LogicHandler) saveMessageWithRecipients(ctx context.Context, frame *api.WireMessage, recipients []string) (bool, error) {
	if h.ConversationOutbox && h.DB != nil {
		persisted, handled, err := h.saveMessageWithOutbox(ctx, frame, recipients)
		if handled {
			return persisted, err
		}
		metrics.ConversationOutbox.WithLabelValues("schema_missing_fallback").Inc()
		logx.Errorw("conversation outbox unavailable; falling back to direct message persistence",
			logx.Field("message_id", frame.MessageId),
			logx.Field("session_id", frame.SessionId),
		)
	}
	return h.saveMessageWithoutOutbox(ctx, frame)
}

// saveMessageWithOutbox commits the durable message and the conversation
// summary event in one InnoDB transaction. handled=false means the deployed
// schema has not been migrated yet, so the caller may use the legacy path.
func (h *LogicHandler) saveMessageWithOutbox(ctx context.Context, frame *api.WireMessage, recipients []string) (persisted, handled bool, err error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, true, err
	}
	rollback := func() {
		_ = tx.Rollback()
	}
	query := `
INSERT INTO messages (message_id, client_msg_id, conversation_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	if _, err = tx.ExecContext(ctx, query,
		frame.MessageId, frame.ClientMsgId, frame.SessionId, frame.SessionId,
		frame.Seq, frame.From, frame.To, frame.ToType, frame.Body, frame.SentAt,
	); err != nil {
		rollback()
		if isUnknownColumn(err, "client_msg_id") || isUnknownColumn(err, "conversation_id") || isMissingTable(err, "conversation_outbox") {
			return false, false, nil
		}
		if isDuplicateClientMessage(err) {
			existing, ok, loadErr := h.loadMessageByClientMsgID(ctx, frame.From, frame.ClientMsgId)
			if loadErr != nil {
				return false, true, loadErr
			}
			if ok {
				proto.Reset(frame)
				proto.Merge(frame, existing)
				return false, true, nil
			}
		}
		metrics.MessagePersisted.WithLabelValues("failure").Inc()
		return false, true, err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO conversation_outbox
  (message_id, session_id, from_uid, to_id, to_type, seq, sent_at, status, attempts, available_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?)
ON DUPLICATE KEY UPDATE message_id = VALUES(message_id)
`, frame.MessageId, frame.SessionId, frame.From, frame.To, frame.ToType,
		frame.Seq, frame.SentAt, frame.SentAt, frame.SentAt)
	if err != nil {
		rollback()
		if isMissingTable(err, "conversation_outbox") {
			return false, false, nil
		}
		metrics.MessagePersisted.WithLabelValues("failure").Inc()
		return false, true, err
	}
	if h.DeliveryOutbox {
		recipientsJSON, marshalErr := json.Marshal(recipients)
		if marshalErr != nil {
			rollback()
			return false, true, marshalErr
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO delivery_outbox
  (message_id, trace_id, recipients_json, status, attempts, available_at, last_error, created_at, updated_at)
VALUES (?, ?, ?, 'pending', 0, ?, '', ?, ?)
ON DUPLICATE KEY UPDATE message_id = VALUES(message_id)
`, frame.MessageId, frame.TraceId, string(recipientsJSON), frame.SentAt, frame.SentAt, frame.SentAt)
		if err != nil {
			rollback()
			// DeliveryOutbox is a reliability contract. Do not silently fall
			// back to a message-only commit when its schema is unavailable.
			metrics.DeliveryOutbox.WithLabelValues("enqueue_failure").Inc()
			return false, true, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, true, err
	}
	metrics.MessagePersisted.WithLabelValues("success").Inc()
	metrics.ConversationOutbox.WithLabelValues("enqueued").Inc()
	if h.DeliveryOutbox {
		metrics.DeliveryOutbox.WithLabelValues("enqueued").Inc()
	}
	return true, true, nil
}

func (h *LogicHandler) saveMessageWithoutOutbox(ctx context.Context, frame *api.WireMessage) (bool, error) {
	if h.DB == nil {
		return true, nil
	}

	query := `
INSERT INTO messages (message_id, client_msg_id, conversation_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	if _, err := h.DB.ExecContext(
		ctx,
		query,
		frame.MessageId,
		frame.ClientMsgId,
		frame.SessionId,
		frame.SessionId,
		frame.Seq,
		frame.From,
		frame.To,
		frame.ToType,
		frame.Body,
		frame.SentAt,
	); err != nil {
		metrics.MessagePersisted.WithLabelValues("failure").Inc()
		if isUnknownColumn(err, "client_msg_id") {
			return h.saveMessageWithoutClientID(ctx, frame)
		}
		if isUnknownColumn(err, "conversation_id") {
			return h.saveMessageLegacy(ctx, frame)
		}
		if isDuplicateClientMessage(err) {
			existing, ok, loadErr := h.loadMessageByClientMsgID(ctx, frame.From, frame.ClientMsgId)
			if loadErr != nil {
				return false, loadErr
			}
			if ok {
				proto.Reset(frame)
				proto.Merge(frame, existing)
				return false, nil
			}
		}
		logx.Errorw("persist message failed",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("message_id", frame.MessageId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
		return false, err
	}
	metrics.MessagePersisted.WithLabelValues("success").Inc()
	return true, nil
}

func (h *LogicHandler) saveMessageWithoutClientID(ctx context.Context, frame *api.WireMessage) (bool, error) {
	query := `
INSERT INTO messages (message_id, conversation_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	if _, err := h.DB.ExecContext(
		ctx,
		query,
		frame.MessageId,
		frame.SessionId,
		frame.SessionId,
		frame.Seq,
		frame.From,
		frame.To,
		frame.ToType,
		frame.Body,
		frame.SentAt,
	); err != nil {
		if isUnknownColumn(err, "conversation_id") {
			return h.saveMessageLegacy(ctx, frame)
		}
		logx.Errorw("persist message without client_msg_id failed",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("message_id", frame.MessageId),
			logx.Field("client_msg_id", frame.ClientMsgId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
		return false, err
	}
	logx.Errorw("messages table missing client_msg_id; db idempotency is degraded",
		logx.Field("trace_id", frame.TraceId),
		logx.Field("message_id", frame.MessageId),
		logx.Field("client_msg_id", frame.ClientMsgId),
	)
	return true, nil
}

func (h *LogicHandler) saveMessageLegacy(ctx context.Context, frame *api.WireMessage) (bool, error) {
	query := `
INSERT INTO messages (message_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`
	if _, err := h.DB.ExecContext(
		ctx,
		query,
		frame.MessageId,
		frame.SessionId,
		frame.Seq,
		frame.From,
		frame.To,
		frame.ToType,
		frame.Body,
		frame.SentAt,
	); err != nil {
		logx.Errorw("persist message with legacy schema failed",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("message_id", frame.MessageId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
		return false, err
	}
	logx.Errorw("messages table missing conversation_id/client_msg_id; db idempotency is degraded",
		logx.Field("trace_id", frame.TraceId),
		logx.Field("message_id", frame.MessageId),
		logx.Field("client_msg_id", frame.ClientMsgId),
	)
	return true, nil
}

func (h *LogicHandler) loadMessageByClientMsgID(ctx context.Context, senderID, clientMsgID string) (*api.WireMessage, bool, error) {
	if h.DB == nil || senderID == "" || clientMsgID == "" {
		return nil, false, nil
	}
	var msg api.WireMessage
	var body string
	err := h.DB.QueryRowContext(ctx, `
SELECT message_id, client_msg_id, conversation_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE from_uid = ? AND client_msg_id = ?
LIMIT 1
`, senderID, clientMsgID).Scan(
		&msg.MessageId,
		&msg.ClientMsgId,
		&msg.SessionId,
		&msg.SessionId,
		&msg.Seq,
		&msg.From,
		&msg.To,
		&msg.ToType,
		&body,
		&msg.SentAt,
	)
	if err == nil {
		msg.Body = body
		msg.MsgType = api.MsgType_NORMAL
		return &msg, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if isUnknownColumn(err, "client_msg_id") || isUnknownColumn(err, "conversation_id") {
		return nil, false, nil
	}
	return nil, false, err
}

func isUnknownColumn(err error, column string) bool {
	if err == nil || column == "" {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, strings.ToLower(column)) && strings.Contains(message, "unknown column")
}

func isMissingTable(err error, table string) bool {
	if err == nil || table == "" {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "table") &&
		strings.Contains(message, strings.ToLower(table)) &&
		(strings.Contains(message, "doesn't exist") || strings.Contains(message, "does not exist"))
}

func isDuplicateClientMessage(err error) bool {
	if !isDuplicateEntry(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "uk_sender_client_msg") ||
		(strings.Contains(message, "from_uid") && strings.Contains(message, "client_msg_id"))
}

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}
