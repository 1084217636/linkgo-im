package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAskServiceAsk(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO ai_provider_attempt_logs").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, "mock", "success", sqlmock.AnyArg(), "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO ai_qa_records").
		WithArgs(sqlmock.AnyArg(), "1001", "群聊为什么用 Kafka", sqlmock.AnyArg(), sqlmock.AnyArg(), "mock", 1, "success", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAskService(db, NewMockProvider(), &KnowledgeBase{
		documents: []knowledgeDocument{{
			Path:    "docs/AI_FAQ.md",
			Title:   "群聊为什么用 Kafka",
			Content: "群聊扩散是高扇出操作，同步 for 循环会拖慢发送链路。",
		}},
	}, 3)

	result, err := service.Ask(context.Background(), AskParams{
		OperatorID: "1001",
		Question:   "群聊为什么用 Kafka",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.KnowledgeHits != 1 {
		t.Fatalf("knowledge hits = %d", result.KnowledgeHits)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("sources = %+v", result.Sources)
	}
	if result.Answer == "" {
		t.Fatalf("expected answer")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestAskServicePersistsErrorBestEffort(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO ai_qa_records").
		WithArgs(sqlmock.AnyArg(), "1001", "问题", "", "[]", "failing", 0, "error", "provider unavailable", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO ai_provider_attempt_logs").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, "failing", "error", sqlmock.AnyArg(), "provider unavailable", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAskService(db, failingProvider{}, &KnowledgeBase{}, 3)
	_, err = service.Ask(context.Background(), AskParams{
		OperatorID: "1001",
		Question:   "问题",
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func (failingProvider) Answer(context.Context, AskRequest) (*AskResult, error) {
	return nil, errors.New("provider unavailable")
}
