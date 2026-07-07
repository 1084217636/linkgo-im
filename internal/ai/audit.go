package ai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

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

func saveProviderAttempts(ctx context.Context, db *sql.DB, callID string, attempts []ProviderAttempt) error {
	if db == nil {
		return ErrDatabaseRequired
	}
	for idx, attempt := range attempts {
		_, err := db.ExecContext(ctx, `
INSERT INTO ai_provider_attempt_logs
  (attempt_id, call_id, attempt_order, provider, status, duration_ms, error_message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, newAIRecordID("aia", time.Now().UnixMilli()), callID, idx+1, attempt.Provider, attempt.Status, attempt.DurationMs, truncateRunes(RedactSensitive(attempt.ErrorMessage), 512), attempt.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func newAIRecordID(prefix string, now int64) string {
	if prefix == "" {
		prefix = "ai"
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err == nil {
		return fmt.Sprintf("%s_%d_%x", prefix, now, suffix)
	}
	return fmt.Sprintf("%s_%d", prefix, now)
}
