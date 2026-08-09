package logic

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/1084217636/linkgo-im/internal/ids"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
)

var sessionSeqScript = redis.NewScript(`
local nextSeq = redis.call("INCR", KEYS[1])
redis.call("PEXPIRE", KEYS[1], ARGV[1])
return nextSeq
`)

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
		if err := h.deliverPersistedMessage(ctx, existing); err != nil {
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

	persisted, err := h.saveMessage(ctx, &frame)
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
	if err := h.deliverPersistedMessage(ctx, &frame); err != nil {
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

func (h *LogicHandler) GetHistory(ctx context.Context, req *api.GetHistoryReq) (*api.GetHistoryReply, error) {
	toType := targetType(req.TargetId)
	targetID := req.TargetId
	if toType == "group" {
		targetID = normalizeGroupID(req.TargetId)
		allowed, err := h.isCurrentGroupMember(ctx, targetID, req.UserId)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("user is not an active group member")
		}
	}
	sessionID := buildSessionID(req.UserId, targetID, toType)
	limit := int(req.Limit)
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	queryLimit := limit + 1
	var rows *sql.Rows
	var err error
	if req.BeforeSeq > 0 {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, client_msg_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ? AND seq < ?
ORDER BY seq DESC
LIMIT ?
`, sessionID, req.BeforeSeq, queryLimit)
	} else {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, client_msg_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
	LIMIT ?
`, sessionID, queryLimit)
	}
	if isUnknownColumn(err, "client_msg_id") {
		metrics.HistoryQueries.WithLabelValues("mysql_legacy", "success").Inc()
		return h.getHistoryLegacy(ctx, sessionID, req.BeforeSeq, limit)
	}
	if err != nil {
		metrics.HistoryQueries.WithLabelValues("mysql", "error").Inc()
		return nil, err
	}
	defer rows.Close()

	res := make([]*api.WireMessage, 0, queryLimit)
	for rows.Next() {
		var msg api.WireMessage
		var body string
		if err := rows.Scan(
			&msg.MessageId,
			&msg.ClientMsgId,
			&msg.SessionId,
			&msg.Seq,
			&msg.From,
			&msg.To,
			&msg.ToType,
			&body,
			&msg.SentAt,
		); err != nil {
			return nil, err
		}
		msg.Body = body
		msg.MsgType = api.MsgType_NORMAL
		res = append(res, &msg)
	}
	hasMore := len(res) > limit
	if hasMore {
		res = res[:limit]
	}

	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	reply := &api.GetHistoryReply{Messages: res, HasMore: hasMore}
	if len(res) > 0 && hasMore {
		reply.NextBeforeSeq = res[0].Seq
	}
	metrics.HistoryQueries.WithLabelValues("mysql", "success").Inc()
	return reply, nil
}

func (h *LogicHandler) getHistoryLegacy(ctx context.Context, sessionID string, beforeSeq int64, limit int) (*api.GetHistoryReply, error) {
	queryLimit := limit + 1
	var rows *sql.Rows
	var err error
	if beforeSeq > 0 {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ? AND seq < ?
ORDER BY seq DESC
	LIMIT ?
`, sessionID, beforeSeq, queryLimit)
	} else {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
LIMIT ?
`, sessionID, queryLimit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*api.WireMessage, 0, queryLimit)
	for rows.Next() {
		var msg api.WireMessage
		var body string
		if err := rows.Scan(
			&msg.MessageId,
			&msg.SessionId,
			&msg.Seq,
			&msg.From,
			&msg.To,
			&msg.ToType,
			&body,
			&msg.SentAt,
		); err != nil {
			return nil, err
		}
		msg.Body = body
		msg.MsgType = api.MsgType_NORMAL
		res = append(res, &msg)
	}
	hasMore := len(res) > limit
	if hasMore {
		res = res[:limit]
	}

	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	reply := &api.GetHistoryReply{Messages: res, HasMore: hasMore}
	if len(res) > 0 && hasMore {
		reply.NextBeforeSeq = res[0].Seq
	}
	return reply, nil
}

func (h *LogicHandler) Login(ctx context.Context, req *api.LoginReq) (*api.LoginReply, error) {
	var uid string
	var pwdInDB string
	var status int

	query := "SELECT user_id, password, status FROM users WHERE username = ? LIMIT 1"
	if err := h.DB.QueryRowContext(ctx, query, req.Username).Scan(&uid, &pwdInDB, &status); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logx.Errorw("login query failed", logx.Field("error", err.Error()))
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	valid, legacy := verifyPassword(pwdInDB, req.Password)
	if status != 1 || !valid {
		return nil, fmt.Errorf("invalid credentials")
	}
	if legacy {
		h.upgradeLegacyPassword(ctx, uid, pwdInDB, req.Password)
	}

	token, err := middleware.GenerateToken(uid)
	if err != nil {
		return nil, err
	}

	conversations, err := h.listConversations(ctx, uid, defaultConversationLimit)
	if err != nil {
		logx.Errorw("list conversations failed",
			logx.Field("target_id", uid),
			logx.Field("error", err.Error()),
		)
	}

	return &api.LoginReply{
		Token:         token,
		UserId:        uid,
		Conversations: conversations,
	}, nil
}

func verifyPassword(storedPassword, suppliedPassword string) (valid bool, legacy bool) {
	if isBcryptHash(storedPassword) {
		return bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(suppliedPassword)) == nil, false
	}
	return subtle.ConstantTimeCompare([]byte(storedPassword), []byte(suppliedPassword)) == 1, true
}

func isBcryptHash(password string) bool {
	return strings.HasPrefix(password, "$2a$") ||
		strings.HasPrefix(password, "$2b$") ||
		strings.HasPrefix(password, "$2y$")
}

func (h *LogicHandler) upgradeLegacyPassword(ctx context.Context, uid, storedPassword, suppliedPassword string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(suppliedPassword), bcrypt.DefaultCost)
	if err != nil {
		logx.Errorw("hash legacy password failed", logx.Field("target_id", uid), logx.Field("error", err.Error()))
		return
	}
	if _, err := h.DB.ExecContext(ctx, `
UPDATE users SET password = ?
WHERE user_id = ? AND password = ?
`, string(hash), uid, storedPassword); err != nil {
		logx.Errorw("upgrade legacy password failed", logx.Field("target_id", uid), logx.Field("error", err.Error()))
	}
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

func (h *LogicHandler) nextSequence(ctx context.Context, sessionID string) (int64, error) {
	if h.Rdb == nil {
		return 0, fmt.Errorf("redis is required for sequence allocation")
	}
	key := "seq:" + sessionID
	if err := h.ensureSequenceInitialized(ctx, key, sessionID); err != nil {
		return 0, err
	}
	return sessionSeqScript.Run(ctx, h.Rdb, []string{key}, sequenceTTL.Milliseconds()).Int64()
}

func (h *LogicHandler) ensureSequenceInitialized(ctx context.Context, key, sessionID string) error {
	if h.DB == nil {
		return nil
	}
	exists, err := h.Rdb.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	var maxSeq int64
	if err := h.DB.QueryRowContext(ctx, `
SELECT COALESCE(MAX(seq), 0)
FROM messages
WHERE session_id = ?
`, sessionID).Scan(&maxSeq); err != nil {
		return err
	}
	if maxSeq <= 0 {
		return nil
	}
	return h.Rdb.SetNX(ctx, key, maxSeq, sequenceTTL).Err()
}

func (h *LogicHandler) saveMessage(ctx context.Context, frame *api.WireMessage) (bool, error) {
	if h.ConversationOutbox && h.DB != nil {
		persisted, handled, err := h.saveMessageWithOutbox(ctx, frame)
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
func (h *LogicHandler) saveMessageWithOutbox(ctx context.Context, frame *api.WireMessage) (persisted, handled bool, err error) {
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
	if err = tx.Commit(); err != nil {
		return false, true, err
	}
	metrics.MessagePersisted.WithLabelValues("success").Inc()
	metrics.ConversationOutbox.WithLabelValues("enqueued").Inc()
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

func buildSessionID(from, to, toType string) string {
	if toType == "group" {
		return "group:" + normalizeGroupID(to)
	}
	ids := []string{from, to}
	sort.Strings(ids)
	return "c2c:" + strings.Join(ids, ":")
}

func normalizeGroupID(target string) string {
	return strings.TrimPrefix(target, "group:")
}

func targetType(target string) string {
	if strings.HasPrefix(strings.ToUpper(target), "G") || strings.HasPrefix(target, "group:") {
		return "group"
	}
	return "user"
}
