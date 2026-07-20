package gameops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/internal/metrics"
)

var (
	ErrInvalidGrant  = errors.New("invalid item grant request")
	grantIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,127}$`)
	grantPartPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
)

type GrantItem struct {
	UserID   string `json:"user_id"`
	ItemID   string `json:"item_id"`
	Quantity int64  `json:"quantity"`
}

type GrantRequest struct {
	GrantRequestID string      `json:"grant_request_id"`
	Items          []GrantItem `json:"items"`
}

type GrantResult struct {
	GrantRequestID   string      `json:"grant_request_id"`
	Status           string      `json:"status"`
	AlreadyProcessed bool        `json:"already_processed"`
	Items            []GrantItem `json:"items"`
}

type GrantService struct {
	db *sql.DB
}

func NewGrantService(db *sql.DB) *GrantService {
	return &GrantService{db: db}
}

func ValidateGrantRequest(request GrantRequest) error {
	if !grantIDPattern.MatchString(strings.TrimSpace(request.GrantRequestID)) || len(request.Items) == 0 || len(request.Items) > 1000 {
		return ErrInvalidGrant
	}
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if !grantPartPattern.MatchString(strings.TrimSpace(item.UserID)) || !grantPartPattern.MatchString(strings.TrimSpace(item.ItemID)) || item.Quantity <= 0 {
			return ErrInvalidGrant
		}
		key := item.UserID + "\x00" + item.ItemID
		if _, exists := seen[key]; exists {
			return ErrInvalidGrant
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (s *GrantService) GrantItems(ctx context.Context, actor Actor, request GrantRequest, traceID, clientIP string) (result *GrantResult, err error) {
	if !roleAllowed(actor.Role, "operator") {
		return nil, ErrForbidden
	}
	if err := ValidateGrantRequest(request); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, errors.New("item grant store is unavailable")
	}
	defer func() {
		if err != nil {
			s.recordFailure(ctx, actor, request, traceID, clientIP, err)
		}
	}()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var existingStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM game_item_grant_requests WHERE grant_request_id = ? FOR UPDATE`, request.GrantRequestID).Scan(&existingStatus)
	if err == nil {
		items, loadErr := loadGrantItems(ctx, tx, request.GrantRequestID)
		if loadErr != nil {
			return nil, loadErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &GrantResult{GrantRequestID: request.GrantRequestID, Status: existingStatus, AlreadyProcessed: true, Items: items}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO game_item_grant_requests (grant_request_id, operator_id, status, item_count, created_at, updated_at)
VALUES (?, ?, 'processing', ?, ?, ?)
`, request.GrantRequestID, actor.UserID, len(request.Items), now, now); err != nil {
		return nil, err
	}
	for _, item := range request.Items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO game_item_grants (grant_request_id, user_id, item_id, quantity, status, operator_id, created_at)
VALUES (?, ?, ?, ?, 'success', ?, ?)
`, request.GrantRequestID, item.UserID, item.ItemID, item.Quantity, actor.UserID, now); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO player_items (user_id, item_id, quantity, updated_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE quantity = quantity + VALUES(quantity), updated_at = VALUES(updated_at)
`, item.UserID, item.ItemID, item.Quantity, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_item_grant_requests SET status = 'success', updated_at = ? WHERE grant_request_id = ?`, now, request.GrantRequestID); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, grantAudit(actor, request, traceID, clientIP, "success", "")); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	metrics.GameOpsGrantedItems.WithLabelValues("success").Add(float64(len(request.Items)))
	return &GrantResult{GrantRequestID: request.GrantRequestID, Status: "success", Items: request.Items}, nil
}

func (s *GrantService) GetResult(ctx context.Context, grantRequestID string) (*GrantResult, error) {
	if s == nil || s.db == nil || !grantIDPattern.MatchString(grantRequestID) {
		return nil, ErrInvalidGrant
	}
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM game_item_grant_requests WHERE grant_request_id = ?`, grantRequestID).Scan(&status); err != nil {
		return nil, err
	}
	items, err := loadGrantItems(ctx, s.db, grantRequestID)
	if err != nil {
		return nil, err
	}
	return &GrantResult{GrantRequestID: grantRequestID, Status: status, AlreadyProcessed: true, Items: items}, nil
}

type grantQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadGrantItems(ctx context.Context, queryer grantQueryer, requestID string) ([]GrantItem, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT user_id, item_id, quantity FROM game_item_grants WHERE grant_request_id = ? ORDER BY id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GrantItem, 0)
	for rows.Next() {
		var item GrantItem
		if err := rows.Scan(&item.UserID, &item.ItemID, &item.Quantity); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *GrantService) recordFailure(ctx context.Context, actor Actor, request GrantRequest, traceID, clientIP string, cause error) {
	metrics.GameOpsGrantedItems.WithLabelValues("failed").Add(float64(len(request.Items)))
	message := cause.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO game_item_grant_failures (failure_id, grant_request_id, operator_id, error_message, created_at)
VALUES (?, ?, ?, ?, ?)
`, newAuditID(), request.GrantRequestID, actor.UserID, message, time.Now().UnixMilli())
	_ = InsertAudit(ctx, s.db, grantAudit(actor, request, traceID, clientIP, "failed", message))
}

func grantAudit(actor Actor, request GrantRequest, traceID, clientIP, result, errorMessage string) AuditEntry {
	detail, _ := json.Marshal(map[string]any{"item_count": len(request.Items), "error": errorMessage})
	return AuditEntry{OperatorID: actor.UserID, OperatorRole: actor.Role, Operation: "item.batch_grant", ResourceType: "grant_request", ResourceID: request.GrantRequestID, RequestID: request.GrantRequestID, Result: result, DetailJSON: string(detail), TraceID: traceID, ClientIP: clientIP}
}
