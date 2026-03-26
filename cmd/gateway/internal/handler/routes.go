package handler

import (
	"net/http"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/rest"
)

func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext) {
	server.AddRoute(rest.Route{
		Method: http.MethodGet,
		Path:   "/metrics",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			metrics.Handler().ServeHTTP(w, r)
		},
	})

	server.AddRoutes(
		rest.WithMiddlewares([]rest.Middleware{
			gwmiddleware.RateLimitMiddleware(serverCtx.RestLimiter),
		},
			rest.Route{
				Method:  http.MethodPost,
				Path:    "/login",
				Handler: LoginHandler(serverCtx),
			},
		),
		rest.WithPrefix("/api/v1"),
	)

	authMiddlewares := []rest.Middleware{
		gwmiddleware.AuthMiddleware(),
		gwmiddleware.RateLimitMiddleware(serverCtx.RestLimiter),
	}

	server.AddRoutes(
		rest.WithMiddlewares(authMiddlewares,
			rest.Route{
				Method:  http.MethodGet,
				Path:    "/history",
				Handler: HistoryHandler(serverCtx),
			},
			rest.Route{
				Method:  http.MethodPost,
				Path:    "/group/create",
				Handler: GroupCreateHandler(serverCtx),
			},
			rest.Route{
				Method:  http.MethodGet,
				Path:    "/user/groups",
				Handler: UserGroupsHandler(serverCtx),
			},
		),
		rest.WithPrefix("/api/v1"),
	)

	server.AddRoutes(
		rest.WithMiddlewares([]rest.Middleware{
			gwmiddleware.AuthMiddleware(),
			gwmiddleware.RateLimitMiddleware(serverCtx.WsLimiter),
		},
			rest.Route{
				Method:  http.MethodGet,
				Path:    "/ws",
				Handler: WebSocketHandler(serverCtx),
			},
		),
	)
}
