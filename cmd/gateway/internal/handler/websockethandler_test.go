package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestWebSocketOriginAllowed(t *testing.T) {
	tests := []struct {
		name               string
		host               string
		origin             string
		allowed            []string
		allowMissingOrigin bool
		want               bool
	}{
		{name: "missing origin rejected by default", host: "api.example.com", want: false},
		{name: "missing origin explicitly allowed", host: "api.example.com", allowMissingOrigin: true, want: true},
		{name: "same origin still requires allowlist", host: "api.example.com", origin: "https://api.example.com", want: false},
		{name: "configured same origin", host: "api.example.com", origin: "https://api.example.com", allowed: []string{"https://api.example.com"}, want: true},
		{name: "configured origin", host: "api.example.com", origin: "https://app.example.com", allowed: []string{"https://app.example.com"}, want: true},
		{name: "unlisted cross origin", host: "api.example.com", origin: "https://evil.example.com", allowed: []string{"https://app.example.com"}, want: false},
		{name: "similar domain rejected", host: "api.example.com", origin: "https://app.example.com.evil", allowed: []string{"https://app.example.com"}, want: false},
		{name: "scheme must match", host: "api.example.com", origin: "https://app.example.com", allowed: []string{"http://app.example.com"}, want: false},
		{name: "port must match", host: "api.example.com", origin: "https://app.example.com:444", allowed: []string{"https://app.example.com:443"}, want: false},
		{name: "malformed origin", host: "api.example.com", origin: "://bad", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://"+tc.host+"/ws", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := webSocketOriginAllowed(req, tc.allowed, tc.allowMissingOrigin); got != tc.want {
				t.Fatalf("webSocketOriginAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRejectInvalidWebSocketOriginReturnsForbidden(t *testing.T) {
	req := httptest.NewRequest("GET", "http://api.example.com/ws", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	resp := httptest.NewRecorder()

	rejected := rejectInvalidWebSocketOrigin(resp, req, []string{"https://app.example.com"}, false)

	if !rejected {
		t.Fatal("rejectInvalidWebSocketOrigin() = false, want true")
	}
	if resp.Code != 403 {
		t.Fatalf("status = %d, want 403", resp.Code)
	}
}

func TestAuthorizeReplaySessionC2C(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		sessionID string
		wantErr   bool
	}{
		{name: "first participant", userID: "1001", sessionID: "c2c:1001:1002"},
		{name: "second participant", userID: "1002", sessionID: "c2c:1001:1002"},
		{name: "unrelated user", userID: "1003", sessionID: "c2c:1001:1002", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := authorizeReplaySession(context.Background(), nil, tc.userID, tc.sessionID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("authorizeReplaySession() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAuthorizeReplaySessionGroup(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		noRows  bool
		wantErr bool
	}{
		{name: "active member", status: "active"},
		{name: "not a member", noRows: true, wantErr: true},
		{name: "left member", status: "left", wantErr: true},
		{name: "removed member", status: "removed", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New(): %v", err)
			}
			defer db.Close()

			expectation := mock.ExpectQuery("SELECT status").WithArgs("G100", "1001")
			if tc.noRows {
				expectation.WillReturnError(sql.ErrNoRows)
			} else {
				expectation.WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(tc.status))
			}

			err = authorizeReplaySession(context.Background(), db, "1001", "group:G100")
			if (err != nil) != tc.wantErr {
				t.Fatalf("authorizeReplaySession() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestAuthorizeReplaySessionRejectsMalformedSessions(t *testing.T) {
	for _, sessionID := range []string{
		"c2c:1001",
		"c2c::1002",
		"c2c:1001:1002:1003",
		"group:",
		"unknown:1001",
		" c2c:1001:1002",
	} {
		t.Run(sessionID, func(t *testing.T) {
			if err := authorizeReplaySession(context.Background(), nil, "1001", sessionID); err == nil {
				t.Fatal("authorizeReplaySession() error = nil, want malformed session rejection")
			}
		})
	}
}

func TestAuthorizeReplaySessionFailsClosedOnGroupStoreErrors(t *testing.T) {
	t.Run("database unavailable", func(t *testing.T) {
		if err := authorizeReplaySession(context.Background(), nil, "1001", "group:G100"); err == nil {
			t.Fatal("authorizeReplaySession() error = nil, want unavailable database rejection")
		}
	})

	t.Run("query error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New(): %v", err)
		}
		defer db.Close()

		queryErr := errors.New("database unavailable")
		mock.ExpectQuery("SELECT status").WithArgs("G100", "1001").WillReturnError(queryErr)
		if err := authorizeReplaySession(context.Background(), db, "1001", "group:G100"); !errors.Is(err, queryErr) {
			t.Fatalf("authorizeReplaySession() error = %v, want wrapped %v", err, queryErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
	})
}

func TestAuthorizeReplaySessionAllowsConnectionWithoutSession(t *testing.T) {
	if err := authorizeReplaySession(context.Background(), nil, "1001", ""); err != nil {
		t.Fatalf("authorizeReplaySession() error = %v, want nil for ordinary connection", err)
	}
}

func TestWebSocketHandlerRejectsUnauthorizedReplayBeforeUpgrade(t *testing.T) {
	token, err := authutil.GenerateToken("1003")
	if err != nil {
		t.Fatalf("GenerateToken(): %v", err)
	}

	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			Gateway: config.GatewayConf{
				AllowedOrigins: []string{"https://app.example.com"},
			},
		},
	}
	handler := gwmiddleware.NewAuthMiddleware().Handle(WebSocketHandler(svcCtx))
	req := httptest.NewRequest(
		http.MethodGet,
		"http://api.example.com/ws?token="+url.QueryEscape(token)+"&session_id="+url.QueryEscape("c2c:1001:1002")+"&last_seq=0",
		nil,
	)
	req.Header.Set("Origin", "https://app.example.com")
	resp := httptest.NewRecorder()

	handler(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}
