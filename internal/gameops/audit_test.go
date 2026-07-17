package gameops

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInsertAuditWritesManagementEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO operation_audit_logs").
		WithArgs(sqlmock.AnyArg(), "1001", "operator", "activity.create", "activity", "summer", "req-1", "success", `{"version":1}`, "trace-1", "127.0.0.1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = InsertAudit(context.Background(), db, AuditEntry{
		OperatorID:   "1001",
		OperatorRole: "operator",
		Operation:    "activity.create",
		ResourceType: "activity",
		ResourceID:   "summer",
		RequestID:    "req-1",
		Result:       "success",
		DetailJSON:   `{"version":1}`,
		TraceID:      "trace-1",
		ClientIP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("InsertAudit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
