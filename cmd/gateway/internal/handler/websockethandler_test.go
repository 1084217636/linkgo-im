package handler

import (
	"net/http/httptest"
	"testing"
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
