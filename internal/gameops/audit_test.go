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

func TestListAuditsUsesBoundedFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := sqlmock.NewRows([]string{"audit_id", "operator_id", "operator_role", "operation", "resource_type", "resource_id", "request_id", "result", "detail_json", "trace_id", "client_ip", "created_at"}).
		AddRow("audit-1", "1001", "operator", "item.batch_grant", "grant_request", "grant-1", "grant-1", "success", `{}`, "trace-1", "127.0.0.1", int64(100))
	mock.ExpectQuery("SELECT audit_id.*FROM operation_audit_logs WHERE 1=1 AND operator_id = \\? AND resource_type = \\? ORDER BY created_at DESC LIMIT \\?").
		WithArgs("1001", "grant_request", 200).WillReturnRows(rows)
	entries, err := ListAudits(context.Background(), db, AuditFilter{OperatorID: "1001", ResourceType: "grant_request", Limit: 999})
	if err != nil {
		t.Fatalf("ListAudits() error = %v", err)
	}
	if len(entries) != 1 || entries[0].AuditID != "audit-1" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
