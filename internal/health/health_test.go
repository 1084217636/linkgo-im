package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	LiveHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyHandler(t *testing.T) {
	tests := []struct {
		name   string
		checks map[string]Check
		want   int
	}{
		{
			name: "ready",
			checks: map[string]Check{
				"redis": func(ctx context.Context) error { return nil },
			},
			want: http.StatusOK,
		},
		{
			name: "not ready",
			checks: map[string]Check{
				"redis": func(ctx context.Context) error { return errors.New("down") },
			},
			want: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			rec := httptest.NewRecorder()

			ReadyHandler(tt.checks).ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
