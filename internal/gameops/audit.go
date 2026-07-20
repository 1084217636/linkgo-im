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
	AuditID      string `json:"audit_id"`
	OperatorID   string `json:"operator_id"`
	OperatorRole string `json:"operator_role"`
	Operation    string `json:"operation"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	RequestID    string `json:"request_id"`
	Result       string `json:"result"`
	DetailJSON   string `json:"detail_json"`
	TraceID      string `json:"trace_id"`
	ClientIP     string `json:"client_ip"`
	CreatedAt    int64  `json:"created_at"`
}

type AuditFilter struct {
	OperatorID, ResourceType, ResourceID, Result string
	Limit                                        int
}

func ListAudits(ctx context.Context, db *sql.DB, filter AuditFilter) ([]AuditEntry, error) {
	if db == nil {
		return nil, errors.New("audit store is unavailable")
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	query := `SELECT audit_id, operator_id, operator_role, operation, resource_type, resource_id, request_id, result, detail_json, trace_id, client_ip, created_at FROM operation_audit_logs WHERE 1=1`
	args := make([]any, 0, 5)
	for _, part := range []struct{ name, value string }{{"operator_id", filter.OperatorID}, {"resource_type", filter.ResourceType}, {"resource_id", filter.ResourceID}, {"result", filter.Result}} {
		if value := strings.TrimSpace(part.value); value != "" {
			query += " AND " + part.name + " = ?"
			args = append(args, value)
		}
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, filter.Limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]AuditEntry, 0)
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.AuditID, &entry.OperatorID, &entry.OperatorRole, &entry.Operation, &entry.ResourceType, &entry.ResourceID, &entry.RequestID, &entry.Result, &entry.DetailJSON, &entry.TraceID, &entry.ClientIP, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
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
