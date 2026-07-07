package ai

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSummaryServiceGenerate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status, mute_until").
		WithArgs("G100", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"status", "mute_until"}).AddRow("active", 0))
	mock.ExpectQuery("SELECT message_id, conversation_id, seq, from_uid, content, create_time").
		WithArgs("group:G100", 20).
		WillReturnRows(sqlmock.NewRows([]string{"message_id", "conversation_id", "seq", "from_uid", "content", "create_time"}).
			AddRow("m3", "group:G100", int64(3), "1003", "上线存在超时风险", int64(3000)).
			AddRow("m2", "group:G100", int64(2), "1002", "请 1001 明天补充接口测试", int64(2000)).
			AddRow("m1", "group:G100", int64(1), "1001", "今天完成登录链路联调", int64(1000)))
	mock.ExpectExec("INSERT INTO ai_call_logs").
		WithArgs(sqlmock.AnyArg(), "mock", "G100", "group:G100", "1001", 3, int64(1), int64(3), sqlmock.AnyArg(), "success", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO ai_summary_records").
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewSummaryService(db, NewMockProvider(), 50)
	result, err := service.Generate(context.Background(), GenerateSummaryParams{
		GroupID:      "G100",
		OperatorID:   "1001",
		MessageLimit: 20,
		IncludeTodos: true,
		IncludeRisks: true,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.GroupID != "G100" || result.ConversationID != "group:G100" {
		t.Fatalf("unexpected target: %#v", result)
	}
	if result.MessageStartSeq != 1 || result.MessageEndSeq != 3 {
		t.Fatalf("unexpected seq range: %d-%d", result.MessageStartSeq, result.MessageEndSeq)
	}
	if len(result.Todos) == 0 {
		t.Fatalf("expected todos, got none")
	}
	if len(result.Risks) == 0 {
		t.Fatalf("expected risks, got none")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSummaryServiceAuditsProviderErrorBestEffort(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status, mute_until").
		WithArgs("G100", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"status", "mute_until"}).AddRow("active", 0))
	mock.ExpectQuery("SELECT message_id, conversation_id, seq, from_uid, content, create_time").
		WithArgs("group:G100", 20).
		WillReturnRows(sqlmock.NewRows([]string{"message_id", "conversation_id", "seq", "from_uid", "content", "create_time"}).
			AddRow("m1", "group:G100", int64(1), "1001", "今天完成登录链路联调", int64(1000)))
	mock.ExpectExec("INSERT INTO ai_call_logs").
		WithArgs(sqlmock.AnyArg(), "failing", "G100", "group:G100", "1001", 1, int64(1), int64(1), sqlmock.AnyArg(), "error", "provider unavailable", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewSummaryService(db, failingProvider{}, 50)
	_, err = service.Generate(context.Background(), GenerateSummaryParams{
		GroupID:      "G100",
		OperatorID:   "1001",
		MessageLimit: 20,
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSummaryServiceGenerateRequiresGroupMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status, mute_until").
		WithArgs("G100", "1009").
		WillReturnError(sql.ErrNoRows)

	service := NewSummaryService(db, NewMockProvider(), 50)
	_, err = service.Generate(context.Background(), GenerateSummaryParams{
		GroupID:    "G100",
		OperatorID: "1009",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

type failingProvider struct{}

func (failingProvider) Name() string {
	return "failing"
}

func (failingProvider) Summarize(context.Context, SummaryRequest) (*SummaryResult, error) {
	return nil, errors.New("provider unavailable")
}
