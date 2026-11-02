package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest/httpx"
)

const WebSocketTicketTTL = 30 * time.Second

func ticketKey(ticket string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(raw) != 32 {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return "ws_ticket:" + hex.EncodeToString(sum[:]), true
}

// IssueWebSocketTicket creates a short-lived, single-use handshake credential.
// Redis stores only its digest, so a Redis dump does not reveal the URL value.
func IssueWebSocketTicket(ctx context.Context, rdb *redis.Client, uid string) (string, error) {
	if uid == "" || rdb == nil {
		return "", errors.New("missing authenticated user or Redis")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	key, _ := ticketKey(ticket)
	return ticket, rdb.Set(ctx, key, uid, WebSocketTicketTTL).Err()
}

// NewWebSocketAuthMiddleware consumes the ticket before upgrading the socket.
// Legacy JWT query auth remains for the existing protocol benchmark; the TLS
// demo edge rejects it, so its browser never puts a long-lived JWT in a URL.
func NewWebSocketAuthMiddleware(rdb *redis.Client) *WebSocketAuthMiddleware {
	return &WebSocketAuthMiddleware{rdb: rdb}
}

type WebSocketAuthMiddleware struct{ rdb *redis.Client }

func (m *WebSocketAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ticket := r.URL.Query().Get("ticket"); ticket != "" {
			key, ok := ticketKey(ticket)
			if !ok {
				httpx.WriteJsonCtx(r.Context(), w, http.StatusUnauthorized, map[string]string{"error": "invalid websocket ticket"})
				return
			}
			uid, err := m.rdb.GetDel(r.Context(), key).Result()
			if err != nil || uid == "" {
				code := http.StatusUnauthorized
				if err != nil && !errors.Is(err, redis.Nil) {
					code = http.StatusServiceUnavailable
				}
				httpx.WriteJsonCtx(r.Context(), w, code, map[string]string{"error": "invalid or unavailable websocket ticket"})
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserIDKey{}, uid)
			next(w, r.WithContext(ctx))
			return
		}
		// The old benchmark and API clients use JWT in the URL. Keep that path
		// reachable only through loopback in the local Compose configuration.
		legacy := authutil.ExtractBearerToken(r.Header.Get("Authorization"))
		if legacy == "" {
			legacy = r.URL.Query().Get("token")
		}
		claims, err := authutil.ParseToken(legacy)
		if err != nil {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusUnauthorized, map[string]string{"error": "invalid websocket credentials"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserIDKey{}, claims.UserID)
		next(w, r.WithContext(ctx))
	}
}
