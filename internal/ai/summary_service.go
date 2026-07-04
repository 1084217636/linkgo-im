package ai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultSummaryMessageLimit = 50
	defaultSummaryMaxMessages  = 100
)

var (
	ErrDatabaseRequired = errors.New("database is required")
	ErrGroupIDRequired  = errors.New("group_id is required")
	ErrOperatorRequired = errors.New("operator_id is required")
	ErrNoMessages       = errors.New("no group messages found")
	ErrForbidden        = errors.New("user is not an active group member")
)

type SummaryService struct {
	db          *sql.DB
	provider    Provider
	maxMessages int
}

func NewSummaryService(db *sql.DB, provider Provider, maxMessages int) *SummaryService {
	if provider == nil {
		provider = NewMockProvider()
	}
	if maxMessages <= 0 {
		maxMessages = defaultSummaryMaxMessages
	}
	return &SummaryService{
		db:          db,
		provider:    provider,
		maxMessages: maxMessages,
	}
}

func (s *SummaryService) Generate(ctx context.Context, params GenerateSummaryParams) (*SummaryResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrDatabaseRequired
	}

	groupID := strings.TrimSpace(params.GroupID)
	operatorID := strings.TrimSpace(params.OperatorID)
	if groupID == "" {
		return nil, ErrGroupIDRequired
	}
	if operatorID == "" {
		return nil, ErrOperatorRequired
	}
	if err := s.validateActiveGroupMember(ctx, groupID, operatorID); err != nil {
		return nil, err
	}

	limit := normalizeLimit(params.MessageLimit, s.maxMessages)
	conversationID := BuildGroupConversationID(groupID)
	messages, err := s.loadMessages(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, ErrNoMessages
	}

	result, err := s.provider.Summarize(ctx, SummaryRequest{
		GroupID:        groupID,
		ConversationID: conversationID,
		OperatorID:     operatorID,
		Messages:       messages,
		IncludeTodos:   params.IncludeTodos,
		IncludeRisks:   params.IncludeRisks,
	})
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	completeSummaryResult(result, groupID, conversationID, s.provider.Name(), messages, now)
	if err := s.saveResult(ctx, operatorID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func BuildGroupConversationID(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if strings.HasPrefix(groupID, "group:") {
		return groupID
	}
	return "group:" + groupID
}

func normalizeLimit(limit, maxMessages int) int {
	if maxMessages <= 0 {
		maxMessages = defaultSummaryMaxMessages
	}
	if limit <= 0 {
		limit = defaultSummaryMessageLimit
	}
	if limit > maxMessages {
		return maxMessages
	}
	return limit
}

func (s *SummaryService) validateActiveGroupMember(ctx context.Context, groupID, userID string) error {
	var status string
	var muteUntil int64
	err := s.db.QueryRowContext(ctx, `
SELECT status, mute_until
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`, strings.TrimPrefix(groupID, "group:"), userID).Scan(&status, &muteUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if status != "active" || (muteUntil > 0 && muteUntil > time.Now().UnixMilli()) {
		return ErrForbidden
	}
	return nil
}

func (s *SummaryService) loadMessages(ctx context.Context, conversationID string, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT message_id, conversation_id, seq, from_uid, content, create_time
FROM messages
WHERE conversation_id = ? AND to_type = 'group'
ORDER BY seq DESC
LIMIT ?
`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reversed := make([]Message, 0, limit)
	for rows.Next() {
		var item Message
		if err := rows.Scan(
			&item.MessageID,
			&item.ConversationID,
			&item.Seq,
			&item.FromUID,
			&item.Content,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		reversed = append(reversed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		messages = append(messages, reversed[i])
	}
	return messages, nil
}

func completeSummaryResult(result *SummaryResult, groupID, conversationID, provider string, messages []Message, now int64) {
	if result.SummaryID == "" {
		result.SummaryID = newSummaryID(now)
	}
	if result.GroupID == "" {
		result.GroupID = groupID
	}
	if result.ConversationID == "" {
		result.ConversationID = conversationID
	}
	if result.Provider == "" {
		result.Provider = provider
	}
	if result.CreatedAt == 0 {
		result.CreatedAt = now
	}
	if len(messages) > 0 {
		if result.MessageStartSeq == 0 {
			result.MessageStartSeq = messages[0].Seq
		}
		if result.MessageEndSeq == 0 {
			result.MessageEndSeq = messages[len(messages)-1].Seq
		}
	}
}

func (s *SummaryService) saveResult(ctx context.Context, operatorID string, result *SummaryResult) error {
	todosJSON, err := json.Marshal(result.Todos)
	if err != nil {
		return err
	}
	risksJSON, err := json.Marshal(result.Risks)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ai_summary_records
  (summary_id, group_id, conversation_id, operator_id, message_start_seq, message_end_seq, summary, todos_json, risks_json, provider, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, result.SummaryID, result.GroupID, result.ConversationID, operatorID, result.MessageStartSeq, result.MessageEndSeq, result.Summary, string(todosJSON), string(risksJSON), result.Provider, result.CreatedAt)
	return err
}

func newSummaryID(now int64) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err == nil {
		return fmt.Sprintf("ais_%d_%x", now, suffix)
	}
	return fmt.Sprintf("ais_%d", now)
}
