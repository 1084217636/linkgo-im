package handler

import (
	"net/http"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func WebSocketTicketHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := gwmiddleware.UserIDFromContext(r.Context())
		ticket, err := gwmiddleware.IssueWebSocketTicket(r.Context(), svcCtx.Rdb, uid)
		if err != nil {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusServiceUnavailable, map[string]string{"error": "cannot issue websocket ticket"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		httpx.OkJsonCtx(r.Context(), w, map[string]any{"ticket": ticket, "expires_in": int(gwmiddleware.WebSocketTicketTTL.Seconds())})
	}
}
