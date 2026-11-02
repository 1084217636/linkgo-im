package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecureEdgeServesPageAndProxiesAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Errorf("forwarded scheme = %q", r.Header.Get("X-Forwarded-Proto"))
		}
		_, _ = io.WriteString(w, r.Method+" "+r.URL.RequestURI())
	}))
	defer upstream.Close()
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("secure client"), 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(publicDir, upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		code         int
		body         string
	}{
		{"GET", "/", 200, "secure client"},
		{"POST", "/api/v1/login", 200, "POST /api/v1/login"},
		{"GET", "/ws?token=long-lived-jwt", 401, "WebSocket ticket required"},
		{"GET", "/ws?ticket=short-lived", 200, "GET /ws?ticket=short-lived"},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, "https://localhost:8443"+tc.path, nil))
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("%s %s: status=%d body=%q", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
