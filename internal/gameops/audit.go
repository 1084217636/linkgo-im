package gameops

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type AuditExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type AuditEntry struct {
	OperatorID   string
	OperatorRole string
	Operation    string
	ResourceType string
	ResourceID   string
	RequestID    string
	Result       string
	DetailJSON   string
	TraceID      string
	ClientIP     string
}

func InsertAudit(ctx context.Context, execer AuditExecer, entry AuditEntry) error {
	if execer == nil {
		return errors.New("audit store is unavailable")
	}
	if strings.TrimSpace(entry.OperatorID) == "" || strings.TrimSpace(entry.Operation) == "" {
		return errors.New("audit operator and operation are required")
	}
	if entry.Result == "" {
		entry.Result = "success"
	}
	if entry.DetailJSON == "" {
		entry.DetailJSON = "{}"
	}
	_, err := execer.ExecContext(ctx, `
INSERT INTO operation_audit_logs
  (audit_id, operator_id, operator_role, operation, resource_type, resource_id, request_id, result, detail_json, trace_id, client_ip, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, newAuditID(), entry.OperatorID, entry.OperatorRole, entry.Operation, entry.ResourceType, entry.ResourceID, entry.RequestID, entry.Result, entry.DetailJSON, entry.TraceID, entry.ClientIP, time.Now().UnixMilli())
	return err
}

func newAuditID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return "audit-" + hex.EncodeToString(buf)
	}
	return "audit-" + time.Now().UTC().Format("20060102150405.000000000")
}
