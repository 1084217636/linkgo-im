package ai

import (
	"context"
	"sync"
	"time"
)

type ProviderAttempt struct {
	Provider     string
	Status       string
	DurationMs   int64
	ErrorMessage string
	CreatedAt    int64
}

type attemptRecorderKey struct{}

type AttemptRecorder struct {
	mu       sync.Mutex
	attempts []ProviderAttempt
}

func NewAttemptRecorder() *AttemptRecorder {
	return &AttemptRecorder{}
}

func WithAttemptRecorder(ctx context.Context, recorder *AttemptRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, attemptRecorderKey{}, recorder)
}

func RecordProviderAttempt(ctx context.Context, attempt ProviderAttempt) {
	recorder, ok := ctx.Value(attemptRecorderKey{}).(*AttemptRecorder)
	if !ok || recorder == nil {
		return
	}
	if attempt.CreatedAt == 0 {
		attempt.CreatedAt = time.Now().UnixMilli()
	}
	attempt.ErrorMessage = RedactSensitive(attempt.ErrorMessage)
	recorder.mu.Lock()
	recorder.attempts = append(recorder.attempts, attempt)
	recorder.mu.Unlock()
}

func (r *AttemptRecorder) Attempts() []ProviderAttempt {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProviderAttempt, len(r.attempts))
	copy(out, r.attempts)
	return out
}
