package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const defaultAskTopK = 3

var ErrQuestionRequired = errors.New("question is required")

type AskService struct {
	db       *sql.DB
	provider Provider
	kb       *KnowledgeBase
	topK     int
}

func NewAskService(db *sql.DB, provider Provider, kb *KnowledgeBase, topK int) *AskService {
	if provider == nil {
		provider = NewMockProvider()
	}
	if topK <= 0 {
		topK = defaultAskTopK
	}
	return &AskService{
		db:       db,
		provider: provider,
		kb:       kb,
		topK:     topK,
	}
}

func (s *AskService) Ask(ctx context.Context, params AskParams) (*AskResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrDatabaseRequired
	}

	operatorID := strings.TrimSpace(params.OperatorID)
	if operatorID == "" {
		return nil, ErrOperatorRequired
	}
	question := strings.TrimSpace(params.Question)
	if question == "" {
		return nil, ErrQuestionRequired
	}

	topK := params.TopK
	if topK <= 0 {
		topK = s.topK
	}
	if topK <= 0 {
		topK = defaultAskTopK
	}

	sources := s.kb.Search(question, topK)
	callID := newAIRecordID("aiq", time.Now().UnixMilli())
	req := AskRequest{
		OperatorID: operatorID,
		Question:   question,
		Sources:    sources,
	}
	recorder := NewAttemptRecorder()
	providerCtx := WithAttemptRecorder(ctx, recorder)
	providerStart := time.Now()
	result, err := s.provider.Answer(providerCtx, req)
	durationMs := time.Since(providerStart).Milliseconds()
	if err != nil {
		_ = s.saveResult(ctx, AskResult{
			AnswerID:      callID,
			Question:      question,
			Sources:       sources,
			KnowledgeHits: len(sources),
			Provider:      s.provider.Name(),
		}, operatorID, "error", err.Error())
		_ = saveProviderAttempts(ctx, s.db, callID, ensureAttempts(s.provider.Name(), recorder.Attempts(), durationMs, "error", err.Error()))
		return nil, err
	}
	if result == nil {
		result = &AskResult{}
	}

	now := time.Now().UnixMilli()
	completeAskResult(result, callID, question, sources, s.provider.Name(), now)
	_ = saveProviderAttempts(ctx, s.db, result.AnswerID, ensureAttempts(result.Provider, recorder.Attempts(), durationMs, "success", ""))
	if err := s.saveResult(ctx, *result, operatorID, "success", ""); err != nil {
		return nil, err
	}
	return result, nil
}

func completeAskResult(result *AskResult, answerID, question string, sources []KnowledgeSource, provider string, now int64) {
	if result == nil {
		return
	}
	if result.AnswerID == "" {
		result.AnswerID = answerID
	}
	if result.Question == "" {
		result.Question = question
	}
	if len(result.Sources) == 0 {
		result.Sources = sources
	}
	if result.KnowledgeHits == 0 {
		result.KnowledgeHits = len(result.Sources)
	}
	if result.Provider == "" {
		result.Provider = provider
	}
	if result.CreatedAt == 0 {
		result.CreatedAt = now
	}
}

func (s *AskService) saveResult(ctx context.Context, result AskResult, operatorID, status, errMessage string) error {
	if result.Sources == nil {
		result.Sources = []KnowledgeSource{}
	}
	sourcesJSON, err := json.Marshal(result.Sources)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ai_qa_records
  (answer_id, operator_id, question, answer, sources_json, provider, knowledge_hits, status, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, result.AnswerID, operatorID, truncateRunes(RedactSensitive(result.Question), 1024), result.Answer, string(sourcesJSON), result.Provider, result.KnowledgeHits, status, truncateRunes(RedactSensitive(errMessage), 512), result.CreatedAt)
	return err
}
