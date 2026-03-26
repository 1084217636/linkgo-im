package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func WebSocketHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := gwmiddleware.UserIDFromContext(r.Context())
		if userID == "" {
			writeError(r, w, http.StatusUnauthorized, "missing user context")
			return
		}

		client, err := svcCtx.LogicRouter.GetClient(r.Context(), userID)
		if err != nil {
			writeError(r, w, http.StatusServiceUnavailable, err.Error())
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
			log.Printf("set route failed for user=%s: %v", userID, err)
		}
		defer func() {
			if err := server.ClearRouteIfMatch(ctx, svcCtx.Rdb, userID, routeValue); err != nil {
				log.Printf("delete route failed for user=%s: %v", userID, err)
			}
		}()

		server.SyncOfflineMessages(ctx, svcCtx.Rdb, userID, clientConn)
		server.StartClientLoop(ctx, userID, clientConn, client, svcCtx.Rdb, routeValue, svcCtx.RouteTTL)
	}
}

func newConnectionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
}
