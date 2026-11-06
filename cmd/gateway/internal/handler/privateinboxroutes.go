package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/logic"
	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func RegisterPrivateInboxRoutes(server *rest.Server, s *svc.ServiceContext) {
	inbox := &logic.PrivateInboxLogic{DB: s.DB, Svc: s}
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{
		gwmiddleware.NewAuthMiddleware().Handle,
		gwmiddleware.NewRateLimitMiddleware(s.RestLimiter).Handle,
	},
		rest.Route{Method: http.MethodGet, Path: "/private/unread", Handler: func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			counts, err := inbox.Unread(ctx, gwmiddleware.UserIDFromContext(ctx))
			if err != nil {
				logx.Errorw("list private unread failed", logx.Field("error", err.Error()))
				httpx.WriteJsonCtx(ctx, w, http.StatusServiceUnavailable, map[string]string{"error": "unread messages unavailable"})
				return
			}
			httpx.OkJsonCtx(ctx, w, map[string]any{"unread": counts})
		}},
		rest.Route{Method: http.MethodPost, Path: "/private/read", Handler: func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				PeerUserID string `json:"peer_user_id"`
				ThroughSeq int64  `json:"through_seq"`
			}
			if err := communityBody(r, &body); err != nil {
				httpx.WriteJsonCtx(r.Context(), w, http.StatusBadRequest, map[string]string{"error": "invalid read request"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			err := inbox.MarkRead(ctx, gwmiddleware.UserIDFromContext(ctx), body.PeerUserID, body.ThroughSeq)
			if err != nil {
				if errors.Is(err, logic.ErrInvalidReadSequence) {
					httpx.WriteJsonCtx(ctx, w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				if errors.Is(err, logic.ErrPrivateConversationUnavailable) {
					httpx.WriteJsonCtx(ctx, w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
				logx.Errorw("mark private read failed", logx.Field("error", err.Error()))
				httpx.WriteJsonCtx(ctx, w, http.StatusServiceUnavailable, map[string]string{"error": "cannot mark conversation read"})
				return
			}
			httpx.OkJsonCtx(ctx, w, map[string]string{"status": "ok"})
		}},
	), rest.WithPrefix("/api/v1"))
}
