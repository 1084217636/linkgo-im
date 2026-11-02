package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestWebSocketTicketSingleUseAndExpiry(t *testing.T) {
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mini.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer rdb.Close()
	issue := func() string {
		ticket, err := IssueWebSocketTicket(context.Background(), rdb, "1002")
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	serve := NewWebSocketAuthMiddleware(rdb).Handle(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(UserIDFromContext(r.Context())))
	})
	request := func(ticket string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		serve(w, httptest.NewRequest(http.MethodGet, "/ws?ticket="+ticket, nil))
		return w
	}
	ticket := issue()
	if got := request(ticket); got.Code != http.StatusOK || got.Body.String() != "1002" {
		t.Fatalf("first ticket use: status=%d body=%q", got.Code, got.Body.String())
	}
	if got := request(ticket); got.Code != http.StatusUnauthorized {
		t.Fatalf("replayed ticket: status=%d", got.Code)
	}
	if got := request("invalid"); got.Code != http.StatusUnauthorized {
		t.Fatalf("malformed ticket: status=%d", got.Code)
	}
	ticket = issue()
	mini.FastForward(WebSocketTicketTTL + time.Second)
	if got := request(ticket); got.Code != http.StatusUnauthorized {
		t.Fatalf("expired ticket: status=%d", got.Code)
	}
}
