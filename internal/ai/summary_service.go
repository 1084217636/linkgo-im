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

type CallLog struct {
	CallID          string
	Provider        string
	GroupID         string
	ConversationID  string
	OperatorID      string
	MessageCount    int
	MessageStartSeq int64
	MessageEndSeq   int64
	DurationMs      int64
	Status          string
	ErrorMessage    string
	CreatedAt       int64
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

	request := SummaryRequest{
		GroupID:        groupID,
		ConversationID: conversationID,
		OperatorID:     operatorID,
		Messages:       messages,
		IncludeTodos:   params.IncludeTodos,
		IncludeRisks:   params.IncludeRisks,
	}
	callID := newSummaryID(time.Now().UnixMilli())
	recorder := NewAttemptRecorder()
	providerCtx := WithAttemptRecorder(ctx, recorder)
	providerStart := time.Now()
	result, err := s.provider.Summarize(providerCtx, request)
	durationMs := time.Since(providerStart).Milliseconds()
	if err != nil {
		_ = s.saveCallLog(ctx, buildCallLog(callID, s.provider.Name(), operatorID, request, nil, durationMs, "error", err.Error()))
		_ = s.saveProviderAttempts(ctx, callID, ensureAttempts(s.provider.Name(), recorder.Attempts(), durationMs, "error", err.Error()))
		return nil, err
	}
	now := time.Now().UnixMilli()
	completeSummaryResult(result, groupID, conversationID, s.provider.Name(), messages, now)
	_ = s.saveCallLog(ctx, buildCallLog(callID, result.Provider, operatorID, request, result, durationMs, "success", ""))
	_ = s.saveProviderAttempts(ctx, callID, ensureAttempts(result.Provider, recorder.Attempts(), durationMs, "success", ""))
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

func buildCallLog(callID, provider, operatorID string, req SummaryRequest, result *SummaryResult, durationMs int64, status, errMessage string) CallLog {
	log := CallLog{
		Provider:       provider,
		GroupID:        req.GroupID,
		ConversationID: req.ConversationID,
		OperatorID:     operatorID,
		MessageCount:   len(req.Messages),
		DurationMs:     durationMs,
		Status:         status,
		ErrorMessage:   truncateRunes(RedactSensitive(errMessage), 512),
		CreatedAt:      time.Now().UnixMilli(),
	}
	if len(req.Messages) > 0 {
		log.MessageStartSeq = req.Messages[0].Seq
		log.MessageEndSeq = req.Messages[len(req.Messages)-1].Seq
	}
	if result != nil {
		log.MessageStartSeq = result.MessageStartSeq
		log.MessageEndSeq = result.MessageEndSeq
	}
	log.CallID = callID
	return log
}

func (s *SummaryService) saveCallLog(ctx context.Context, item CallLog) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO ai_call_logs
  (call_id, provider, group_id, conversation_id, operator_id, message_count, message_start_seq, message_end_seq, duration_ms, status, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, item.CallID, item.Provider, item.GroupID, item.ConversationID, item.OperatorID, item.MessageCount, item.MessageStartSeq, item.MessageEndSeq, item.DurationMs, item.Status, item.ErrorMessage, item.CreatedAt)
	return err
}

func ensureAttempts(provider string, attempts []ProviderAttempt, durationMs int64, status, errMessage string) []ProviderAttempt {
	if len(attempts) > 0 {
		return attempts
	}
	return []ProviderAttempt{{
		Provider:     provider,
		Status:       status,
		DurationMs:   durationMs,
		ErrorMessage: errMessage,
		CreatedAt:    time.Now().UnixMilli(),
	}}
}

func (s *SummaryService) saveProviderAttempts(ctx context.Context, callID string, attempts []ProviderAttempt) error {
	for idx, attempt := range attempts {
		_, err := s.db.ExecContext(ctx, `
INSERT INTO ai_provider_attempt_logs
  (attempt_id, call_id, attempt_order, provider, status, duration_ms, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, newSummaryID(time.Now().UnixMilli()), callID, idx+1, attempt.Provider, attempt.Status, attempt.DurationMs, truncateRunes(RedactSensitive(attempt.ErrorMessage), 512), attempt.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func newSummaryID(now int64) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err == nil {
		return fmt.Sprintf("ais_%d_%x", now, suffix)
	}
	return fmt.Sprintf("ais_%d", now)
}
