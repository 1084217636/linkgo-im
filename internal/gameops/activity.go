package gameops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type ActivityStatus string

const (
	ActivityDraft      ActivityStatus = "draft"
	ActivityPending    ActivityStatus = "pending"
	ActivityPublished  ActivityStatus = "published"
	ActivitySuperseded ActivityStatus = "superseded"
	ActivityRolledBack ActivityStatus = "rolled_back"
)

var (
	ErrInvalidActivity = errors.New("invalid activity configuration")
	ErrInvalidState    = errors.New("invalid activity state transition")
	ErrSelfApproval    = errors.New("activity creator cannot approve the same version")
	ErrForbidden       = errors.New("platform role is not allowed for this operation")
	activityIDPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,63}$`)
)

type Actor struct {
	UserID string
	Role   string
}

type ActivityConfig struct {
	Title          string `json:"title"`
	StartAt        int64  `json:"start_at"`
	EndAt          int64  `json:"end_at"`
	RewardItemID   string `json:"reward_item_id"`
	RewardQuantity int64  `json:"reward_quantity"`
}

type ActivityVersion struct {
	ActivityID     string         `json:"activity_id"`
	Version        int            `json:"version"`
	Status         ActivityStatus `json:"status"`
	Config         ActivityConfig `json:"config"`
	RolloutPercent int            `json:"rollout_percent"`
	CreatedBy      string         `json:"created_by"`
	ApprovedBy     string         `json:"approved_by,omitempty"`
}

type ActivityService struct {
	db  *sql.DB
	rdb *redis.Client
}

type activityOutbox struct {
	EventID    string          `json:"event_id"`
	ActivityID string          `json:"activity_id"`
	Operation  string          `json:"operation"`
	Payload    json.RawMessage `json:"payload"`
}

func NewActivityService(db *sql.DB, rdb *redis.Client) *ActivityService {
	return &ActivityService{db: db, rdb: rdb}
}

func ValidateActivityDraft(activityID string, config ActivityConfig, rolloutPercent int) error {
	if !activityIDPattern.MatchString(strings.TrimSpace(activityID)) || strings.TrimSpace(config.Title) == "" {
		return ErrInvalidActivity
	}
	if config.StartAt <= 0 || config.EndAt <= config.StartAt || strings.TrimSpace(config.RewardItemID) == "" || config.RewardQuantity <= 0 {
		return ErrInvalidActivity
	}
	if rolloutPercent < 0 || rolloutPercent > 100 {
		return ErrInvalidActivity
	}
	return nil
}

func (s *ActivityService) CreateDraft(ctx context.Context, actor Actor, activityID string, config ActivityConfig, rolloutPercent int, requestID, traceID, clientIP string) (*ActivityVersion, error) {
	if !roleAllowed(actor.Role, "operator") {
		return nil, ErrForbidden
	}
	if err := ValidateActivityDraft(activityID, config, rolloutPercent); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, errors.New("activity store is unavailable")
	}
	configJSON, _ := json.Marshal(config)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM game_activity_versions WHERE activity_id = ? FOR UPDATE`, activityID).Scan(&latest); err != nil {
		return nil, err
	}
	version := latest + 1
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO game_activities (activity_id, name, status, current_version, published_version, rollout_percent, created_by, created_at, updated_at)
VALUES (?, ?, 'draft', ?, 0, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE name = VALUES(name), status = 'draft', current_version = VALUES(current_version), rollout_percent = VALUES(rollout_percent), updated_at = VALUES(updated_at)
`, activityID, config.Title, version, rolloutPercent, actor.UserID, now, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO game_activity_versions (activity_id, version, status, config_json, rollout_percent, created_by, approved_by, created_at, updated_at)
VALUES (?, ?, 'draft', ?, ?, ?, '', ?, ?)
`, activityID, version, string(configJSON), rolloutPercent, actor.UserID, now, now); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, buildActivityAudit(actor, "activity.create_draft", activityID, requestID, traceID, clientIP, map[string]any{"version": version, "rollout_percent": rolloutPercent})); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ActivityVersion{ActivityID: activityID, Version: version, Status: ActivityDraft, Config: config, RolloutPercent: rolloutPercent, CreatedBy: actor.UserID}, nil
}

func (s *ActivityService) Submit(ctx context.Context, actor Actor, activityID string, version int, requestID, traceID, clientIP string) error {
	if !roleAllowed(actor.Role, "operator") {
		return ErrForbidden
	}
	if s == nil || s.db == nil {
		return errors.New("activity store is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE game_activity_versions SET status = 'pending', updated_at = ? WHERE activity_id = ? AND version = ? AND status = 'draft'`
	args := []any{time.Now().UnixMilli(), activityID, version}
	if actor.Role != "admin" {
		query += " AND created_by = ?"
		args = append(args, actor.UserID)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_activities SET status = 'pending', updated_at = ? WHERE activity_id = ? AND current_version = ?`, time.Now().UnixMilli(), activityID, version); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, buildActivityAudit(actor, "activity.submit", activityID, requestID, traceID, clientIP, map[string]any{"version": version})); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ActivityService) Publish(ctx context.Context, actor Actor, activityID string, version int, requestID, traceID, clientIP string) (*ActivityVersion, error) {
	if !roleAllowed(actor.Role, "reviewer") {
		return nil, ErrForbidden
	}
	if s == nil || s.db == nil {
		return nil, errors.New("activity store is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var status, createdBy, configJSON string
	var rolloutPercent int
	if err := tx.QueryRowContext(ctx, `
SELECT status, created_by, config_json, rollout_percent
FROM game_activity_versions
WHERE activity_id = ? AND version = ?
FOR UPDATE
`, activityID, version).Scan(&status, &createdBy, &configJSON, &rolloutPercent); err != nil {
		return nil, err
	}
	if ActivityStatus(status) != ActivityPending {
		return nil, ErrInvalidState
	}
	if createdBy == actor.UserID && actor.Role != "admin" {
		return nil, ErrSelfApproval
	}
	var config ActivityConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("decode activity config: %w", err)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE game_activity_versions SET status = 'superseded', updated_at = ? WHERE activity_id = ? AND status = 'published' AND version <> ?`, now, activityID, version); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_activity_versions SET status = 'published', approved_by = ?, updated_at = ? WHERE activity_id = ? AND version = ?`, actor.UserID, now, activityID, version); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_activities SET status = 'published', published_version = ?, rollout_percent = ?, updated_at = ? WHERE activity_id = ?`, version, rolloutPercent, now, activityID); err != nil {
		return nil, err
	}
	published := &ActivityVersion{ActivityID: activityID, Version: version, Status: ActivityPublished, Config: config, RolloutPercent: rolloutPercent, CreatedBy: createdBy, ApprovedBy: actor.UserID}
	if err := InsertAudit(ctx, tx, buildActivityAudit(actor, "activity.publish", activityID, requestID, traceID, clientIP, map[string]any{"version": version, "rollout_percent": rolloutPercent})); err != nil {
		return nil, err
	}
	event, err := insertActivityOutbox(ctx, tx, "set", published)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.applyOutbox(ctx, event); err != nil {
		return published, fmt.Errorf("activity published; cache sync pending: %w", err)
	}
	return published, nil
}

func (s *ActivityService) Rollback(ctx context.Context, actor Actor, activityID, requestID, traceID, clientIP string) error {
	if actor.Role != "admin" {
		return ErrForbidden
	}
	if s == nil || s.db == nil {
		return errors.New("activity store is unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT published_version FROM game_activities WHERE activity_id = ? AND status = 'published' FOR UPDATE`, activityID).Scan(&current); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE game_activity_versions SET status = 'rolled_back', updated_at = ? WHERE activity_id = ? AND version = ? AND status = 'published'`, now, activityID, current); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_activities SET status = 'rolled_back', published_version = 0, rollout_percent = 0, updated_at = ? WHERE activity_id = ?`, now, activityID); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, buildActivityAudit(actor, "activity.rollback", activityID, requestID, traceID, clientIP, map[string]any{"version": current})); err != nil {
		return err
	}
	event, err := insertActivityDeleteOutbox(ctx, tx, activityID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.applyOutbox(ctx, event); err != nil {
		return fmt.Errorf("activity rolled back; cache sync pending: %w", err)
	}
	return nil
}

func ActivityCacheKey(activityID string) string {
	return "gameops:activity:" + activityID + ":active"
}

func insertActivityOutbox(ctx context.Context, tx *sql.Tx, operation string, version *ActivityVersion) (activityOutbox, error) {
	payload, _ := json.Marshal(version)
	event := activityOutbox{EventID: newAuditID(), ActivityID: version.ActivityID, Operation: operation, Payload: payload}
	eventJSON, _ := json.Marshal(event)
	_, err := tx.ExecContext(ctx, `INSERT INTO gameops_outbox (event_id, event_type, aggregate_id, payload_json, status, created_at, updated_at) VALUES (?, 'activity_cache', ?, ?, 'pending', ?, ?)`, event.EventID, event.ActivityID, string(eventJSON), time.Now().UnixMilli(), time.Now().UnixMilli())
	return event, err
}

func insertActivityDeleteOutbox(ctx context.Context, tx *sql.Tx, activityID string) (activityOutbox, error) {
	event := activityOutbox{EventID: newAuditID(), ActivityID: activityID, Operation: "delete", Payload: json.RawMessage(`{}`)}
	payload, _ := json.Marshal(event)
	_, err := tx.ExecContext(ctx, `INSERT INTO gameops_outbox (event_id, event_type, aggregate_id, payload_json, status, created_at, updated_at) VALUES (?, 'activity_cache', ?, ?, 'pending', ?, ?)`, event.EventID, activityID, string(payload), time.Now().UnixMilli(), time.Now().UnixMilli())
	return event, err
}

func (s *ActivityService) applyOutbox(ctx context.Context, event activityOutbox) error {
	if s.rdb == nil {
		return errors.New("activity cache is unavailable")
	}
	key := ActivityCacheKey(event.ActivityID)
	var err error
	if event.Operation == "delete" {
		err = s.rdb.Del(ctx, key).Err()
	} else {
		err = s.rdb.Set(ctx, key, string(event.Payload), 0).Err()
	}
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE gameops_outbox SET status = 'processed', processed_at = ?, updated_at = ? WHERE event_id = ? AND status = 'pending'`, time.Now().UnixMilli(), time.Now().UnixMilli(), event.EventID)
	return err
}

func (s *ActivityService) SyncPendingOutbox(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil || s.rdb == nil {
		return 0, errors.New("activity outbox dependencies are unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM gameops_outbox WHERE event_type = 'activity_cache' AND status = 'pending' ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	events := make([]activityOutbox, 0, limit)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return len(events), err
		}
		var event activityOutbox
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return len(events), err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	processed := 0
	for _, event := range events {
		if err := s.applyOutbox(ctx, event); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (s *ActivityService) StartOutboxLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.SyncPendingOutbox(ctx, 50)
		}
	}
}

func buildActivityAudit(actor Actor, operation, activityID, requestID, traceID, clientIP string, detail map[string]any) AuditEntry {
	detailJSON, _ := json.Marshal(detail)
	return AuditEntry{OperatorID: actor.UserID, OperatorRole: actor.Role, Operation: operation, ResourceType: "activity", ResourceID: activityID, RequestID: requestID, Result: "success", DetailJSON: string(detailJSON), TraceID: traceID, ClientIP: clientIP}
}

func roleAllowed(actual, required string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	if actual == "admin" {
		return true
	}
	return actual == required
}
