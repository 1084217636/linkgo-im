package logic

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

const (
	defaultConversationLimit = 50
	conversationCacheTTL     = 30 * 24 * time.Hour
)

var updateConversationLastScript = redis.NewScript(`
local current_seq = tonumber(redis.call("HGET", KEYS[1], "last_seq") or "-1")
local incoming_seq = tonumber(ARGV[1])
if current_seq > incoming_seq then
  redis.call("EXPIRE", KEYS[1], ARGV[8])
  return 0
end
local current_updated_at = tonumber(redis.call("HGET", KEYS[1], "updated_at") or "0")
local incoming_updated_at = tonumber(ARGV[6])
if current_updated_at > incoming_updated_at then
  incoming_updated_at = current_updated_at
end
redis.call("HSET", KEYS[1],
  "conversation_id", ARGV[2],
  "type", ARGV[3],
  "title", ARGV[4],
  "last_msg", ARGV[5],
  "last_seq", ARGV[1],
  "last_msg_time", incoming_updated_at,
  "updated_at", incoming_updated_at,
  "sender_id", ARGV[7])
redis.call("EXPIRE", KEYS[1], ARGV[8])
return 1
`)

func (h *LogicHandler) listConversations(ctx context.Context, uid string, limit int64) ([]*api.Conversation, error) {
	if limit <= 0 {
		limit = defaultConversationLimit
	}

	if h.Rdb != nil {
		items, err := h.Rdb.ZRevRangeWithScores(ctx, server.UserConversationsKey(uid), 0, limit-1).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		if len(items) > 0 {
			return h.loadConversationsFromRedis(ctx, uid, items)
		}
	}

	conversations, err := h.loadConversationsFromDB(ctx, uid, int(limit))
	if err != nil {
		return nil, err
	}
	if h.Rdb != nil && len(conversations) > 0 {
		h.cacheConversationList(ctx, uid, conversations)
	}
	return conversations, nil
}

func (h *LogicHandler) loadConversationsFromRedis(ctx context.Context, uid string, items []redis.Z) ([]*api.Conversation, error) {
	conversations := make([]*api.Conversation, 0, len(items))
	for _, item := range items {
		conversationID, ok := item.Member.(string)
		if !ok || conversationID == "" {
			continue
		}

		fields, err := h.Rdb.HGetAll(ctx, server.ConversationLastKey(conversationID)).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		readSeq := h.readConversationSeq(ctx, uid, conversationID)
		ackedSeq := h.ackedConversationSeq(ctx, uid, conversationID)
		lastSeq := parseInt64(fields["last_seq"])
		updatedAt := parseInt64(fields["updated_at"])
		if updatedAt == 0 {
			updatedAt = int64(item.Score)
		}
		conversationType := valueOrDefault(fields["type"], conversationTypeFromID(conversationID))

		conversations = append(conversations, &api.Conversation{
			ConversationId: conversationID,
			Type:           conversationType,
			Title:          conversationTitleForUser(uid, conversationID, conversationType, fields["title"]),
			LastMsg:        fields["last_msg"],
			LastSeq:        lastSeq,
			ReadSeq:        readSeq,
			AckedSeq:       ackedSeq,
			UnreadCount:    unreadCount(lastSeq, readSeq),
			UpdatedAt:      updatedAt,
		})
	}
	return conversations, nil
}

func (h *LogicHandler) loadConversationsFromDB(ctx context.Context, uid string, limit int) ([]*api.Conversation, error) {
	if h.DB == nil {
		return nil, nil
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT c.id, c.type, c.updated_at, c.last_seq, cm.read_seq, cm.acked_seq, COALESCE(m.content, '')
FROM conversation_members cm
JOIN conversations c ON c.id = cm.conversation_id
LEFT JOIN messages m ON m.session_id = c.id AND m.seq = c.last_seq
WHERE cm.user_id = ?
ORDER BY c.updated_at DESC
LIMIT ?
`, uid, limit)
	legacyAcked := false
	if isUnknownColumn(err, "acked_seq") {
		legacyAcked = true
		rows, err = h.DB.QueryContext(ctx, `
SELECT c.id, c.type, c.updated_at, c.last_seq, cm.read_seq, COALESCE(m.content, '')
FROM conversation_members cm
JOIN conversations c ON c.id = cm.conversation_id
LEFT JOIN messages m ON m.session_id = c.id AND m.seq = c.last_seq
WHERE cm.user_id = ?
ORDER BY c.updated_at DESC
LIMIT ?
`, uid, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]*api.Conversation, 0, limit)
	for rows.Next() {
		var conv api.Conversation
		var scanErr error
		if legacyAcked {
			scanErr = rows.Scan(
				&conv.ConversationId,
				&conv.Type,
				&conv.UpdatedAt,
				&conv.LastSeq,
				&conv.ReadSeq,
				&conv.LastMsg,
			)
			conv.AckedSeq = conv.ReadSeq
		} else {
			scanErr = rows.Scan(
				&conv.ConversationId,
				&conv.Type,
				&conv.UpdatedAt,
				&conv.LastSeq,
				&conv.ReadSeq,
				&conv.AckedSeq,
				&conv.LastMsg,
			)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		conv.Title = conversationTitleForUser(uid, conv.ConversationId, conv.Type, "")
		conv.UnreadCount = unreadCount(conv.LastSeq, conv.ReadSeq)
		conversations = append(conversations, &conv)
	}
	return conversations, rows.Err()
}

func (h *LogicHandler) cacheConversationList(ctx context.Context, uid string, conversations []*api.Conversation) {
	pipe := h.Rdb.TxPipeline()
	for _, conv := range conversations {
		if conv == nil || conv.ConversationId == "" {
			continue
		}
		pipe.ZAdd(ctx, server.UserConversationsKey(uid), redis.Z{
			Score:  float64(conv.UpdatedAt),
			Member: conv.ConversationId,
		})
		pipe.HSet(ctx, server.ConversationLastKey(conv.ConversationId), map[string]any{
			"conversation_id": conv.ConversationId,
			"type":            conv.Type,
			"title":           conv.Title,
			"last_msg":        conv.LastMsg,
			"last_seq":        conv.LastSeq,
			"updated_at":      conv.UpdatedAt,
		})
		pipe.HSet(ctx, server.UserConversationReadKey(uid), conv.ConversationId, conv.ReadSeq)
		pipe.HSet(ctx, server.UserConversationAckedKey(uid), conv.ConversationId, conv.AckedSeq)
		pipe.Expire(ctx, server.ConversationLastKey(conv.ConversationId), conversationCacheTTL)
	}
	pipe.Expire(ctx, server.UserConversationsKey(uid), conversationCacheTTL)
	pipe.Expire(ctx, server.UserConversationReadKey(uid), conversationCacheTTL)
	pipe.Expire(ctx, server.UserConversationAckedKey(uid), conversationCacheTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logx.Errorw("cache conversation list failed",
			logx.Field("target_id", uid),
			logx.Field("error", err.Error()),
		)
	}
}

func (h *LogicHandler) updateConversationState(ctx context.Context, frame *api.WireMessage, recipients []string) {
	if frame == nil || frame.SessionId == "" {
		return
	}

	members := conversationMembers(frame, recipients)
	if h.Rdb != nil {
		if err := h.cacheConversationState(ctx, frame, members); err != nil {
			logx.Errorw("cache conversation state failed",
				logx.Field("trace_id", frame.TraceId),
				logx.Field("message_id", frame.MessageId),
				logx.Field("session_id", frame.SessionId),
				logx.Field("seq", frame.Seq),
				logx.Field("error", err.Error()),
			)
		}
	}
	if h.DB != nil {
		frameCopy, ok := proto.Clone(frame).(*api.WireMessage)
		if !ok {
			return
		}
		membersCopy := append([]string(nil), members...)
		go func() {
			dbCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			h.persistConversationState(dbCtx, frameCopy, membersCopy)
		}()
	}
}

func (h *LogicHandler) cacheConversationState(ctx context.Context, frame *api.WireMessage, members []string) error {
	pipe := h.Rdb.TxPipeline()
	memberArgs := make([]any, 0, len(members))
	for _, member := range members {
		memberArgs = append(memberArgs, member)
	}
	if len(memberArgs) > 0 {
		pipe.SAdd(ctx, server.ConversationMembersKey(frame.SessionId), memberArgs...)
		pipe.Expire(ctx, server.ConversationMembersKey(frame.SessionId), conversationCacheTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	applied, err := updateConversationLastScript.Run(ctx, h.Rdb, []string{
		server.ConversationLastKey(frame.SessionId),
	},
		frame.Seq,
		frame.SessionId,
		frame.ToType,
		conversationTitle(frame.From, frame.SessionId, frame.ToType),
		frame.Body,
		frame.SentAt,
		frame.From,
		int64(conversationCacheTTL.Seconds()),
	).Int64()
	if err != nil {
		return err
	}
	if applied == 1 {
		indexPipe := h.Rdb.TxPipeline()
		for _, member := range members {
			indexPipe.ZAddArgs(ctx, server.UserConversationsKey(member), redis.ZAddArgs{
				GT: true,
				Members: []redis.Z{{
					Score:  float64(frame.SentAt),
					Member: frame.SessionId,
				}},
			})
			indexPipe.Expire(ctx, server.UserConversationsKey(member), conversationCacheTTL)
		}
		if _, err := indexPipe.Exec(ctx); err != nil {
			return err
		}
	}
	return server.MarkConversationRead(ctx, h.Rdb, frame.From, frame.SessionId, frame.Seq)
}

func (h *LogicHandler) persistConversationState(ctx context.Context, frame *api.WireMessage, members []string) {
	if _, err := h.DB.ExecContext(ctx, `
INSERT INTO conversations (id, type, created_at, updated_at, last_seq)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  type = VALUES(type),
  updated_at = VALUES(updated_at),
  last_seq = GREATEST(last_seq, VALUES(last_seq))
`, frame.SessionId, frame.ToType, frame.SentAt, frame.SentAt, frame.Seq); err != nil {
		logx.Errorw("persist conversation failed",
			logx.Field("trace_id", frame.TraceId),
			logx.Field("message_id", frame.MessageId),
			logx.Field("session_id", frame.SessionId),
			logx.Field("seq", frame.Seq),
			logx.Field("error", err.Error()),
		)
		return
	}

	// Group membership is initialized by group create/join/leave flows. A
	// message must not rewrite every member on the hot path. A one-to-one
	// conversation has a constant two-user cardinality and remains safe here.
	if frame.ToType == "group" {
		return
	}
	for _, member := range members {
		readSeq := int64(0)
		if member == frame.From {
			readSeq = frame.Seq
		}
		if _, err := h.DB.ExecContext(ctx, `
INSERT INTO conversation_members (conversation_id, user_id, read_seq, joined_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  read_seq = GREATEST(read_seq, VALUES(read_seq))
`, frame.SessionId, member, readSeq, frame.SentAt); err != nil {
			logx.Errorw("persist conversation member failed",
				logx.Field("trace_id", frame.TraceId),
				logx.Field("message_id", frame.MessageId),
				logx.Field("session_id", frame.SessionId),
				logx.Field("target_id", member),
				logx.Field("error", err.Error()),
			)
		}
	}
}

func (h *LogicHandler) readConversationSeq(ctx context.Context, uid, conversationID string) int64 {
	value, err := h.Rdb.HGet(ctx, server.UserConversationReadKey(uid), conversationID).Result()
	if err != nil {
		return 0
	}
	return parseInt64(value)
}

func (h *LogicHandler) ackedConversationSeq(ctx context.Context, uid, conversationID string) int64 {
	value, err := h.Rdb.HGet(ctx, server.UserConversationAckedKey(uid), conversationID).Result()
	if err != nil {
		return 0
	}
	return parseInt64(value)
}

func conversationMembers(frame *api.WireMessage, recipients []string) []string {
	seen := make(map[string]struct{}, len(recipients)+2)
	if frame.From != "" {
		seen[frame.From] = struct{}{}
	}
	if frame.ToType == "user" && frame.To != "" {
		seen[frame.To] = struct{}{}
	}
	for _, recipient := range recipients {
		if recipient == "" {
			continue
		}
		seen[recipient] = struct{}{}
	}

	members := make([]string, 0, len(seen))
	for member := range seen {
		members = append(members, member)
	}
	return members
}

func conversationTypeFromID(conversationID string) string {
	if strings.HasPrefix(conversationID, "group:") {
		return "group"
	}
	return "user"
}

func conversationTitle(uid, conversationID, conversationType string) string {
	if conversationType == "group" || strings.HasPrefix(conversationID, "group:") {
		return strings.TrimPrefix(conversationID, "group:")
	}
	parts := strings.Split(conversationID, ":")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && parts[i] != "c2c" && parts[i] != uid {
			return parts[i]
		}
	}
	return conversationID
}

func conversationTitleForUser(uid, conversationID, conversationType, cachedTitle string) string {
	if conversationType == "group" || strings.HasPrefix(conversationID, "group:") {
		return valueOrDefault(cachedTitle, conversationTitle(uid, conversationID, conversationType))
	}
	return conversationTitle(uid, conversationID, conversationType)
}

func unreadCount(lastSeq, readSeq int64) int64 {
	if lastSeq <= readSeq {
		return 0
	}
	return lastSeq - readSeq
}

func parseInt64(raw string) int64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
