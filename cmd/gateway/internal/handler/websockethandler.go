package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/zrpc"
)

func WebSocketHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return webSocketOriginAllowed(
				r,
				svcCtx.Config.Gateway.AllowedOrigins,
				svcCtx.Config.Gateway.AllowMissingOrigin,
			)
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectInvalidWebSocketOrigin(
			w,
			r,
			svcCtx.Config.Gateway.AllowedOrigins,
			svcCtx.Config.Gateway.AllowMissingOrigin,
		) {
			return
		}
		userID := gwmiddleware.UserIDFromContext(r.Context())
		if userID == "" {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusUnauthorized, map[string]string{"error": "missing user context"})
			return
		}

		client, err := svcCtx.LogicRouter.GetClient(r.Context(), userID)
		if err != nil {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		clientConn := server.NewClientConn(conn, newConnectionID())
		defer clientConn.Close()

		routeValue := server.BuildRouteValue(svcCtx.GatewayID, clientConn.SessionID)
		server.Manager.Add(userID, clientConn)
		metrics.WSConnections.Inc()
		defer server.Manager.Remove(userID, clientConn)
		defer metrics.WSConnections.Dec()

		ctx := context.Background()
		if err := server.RefreshRoute(ctx, svcCtx.Rdb, userID, routeValue, svcCtx.RouteTTL); err != nil {
			logx.Errorf("set route failed user=%s: %v", userID, err)
		}
		defer func() {
			if err := server.ClearRouteIfMatch(ctx, svcCtx.Rdb, userID, routeValue); err != nil {
				logx.Errorf("delete route failed user=%s: %v", userID, err)
			}
		}()

		sessionID := r.URL.Query().Get("session_id")
		lastSeq := parseLastSeq(r.URL.Query().Get("last_seq"))
		server.SyncOfflineMessages(ctx, svcCtx.Rdb, userID, clientConn, sessionID, lastSeq)
		ctx = zrpc.SetHashKey(ctx, userID)
		server.StartClientLoop(ctx, userID, clientConn, client, svcCtx.Rdb, routeValue, svcCtx.RouteTTL)
	}
}

func rejectInvalidWebSocketOrigin(
	w http.ResponseWriter,
	r *http.Request,
	allowedOrigins []string,
	allowMissingOrigin bool,
) bool {
	if webSocketOriginAllowed(r, allowedOrigins, allowMissingOrigin) {
		return false
	}
	http.Error(w, "websocket origin is not allowed", http.StatusForbidden)
	return true
}

func webSocketOriginAllowed(r *http.Request, allowedOrigins []string, allowMissingOrigin bool) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return allowMissingOrigin
	}

	normalized, ok := canonicalWebOrigin(origin)
	if !ok {
		return false
	}

	for _, allowed := range allowedOrigins {
		candidate, ok := canonicalWebOrigin(allowed)
		if ok && candidate == normalized {
			return true
		}
	}
	return false
}

func canonicalWebOrigin(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(parsed.Host), true
}

func parseLastSeq(raw string) int64 {
	if raw == "" {
		return -1
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return -1
	}
	return value
}

func newConnectionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
}
