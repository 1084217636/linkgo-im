package middleware

import (
	"context"
	"net/http"

	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type ctxUserIDKey struct{}

func AuthMiddleware() func(next http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := authutil.ExtractBearerToken(r.Header.Get("Authorization"))
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			claims, err := authutil.ParseToken(token)
			if err != nil {
				httpx.WriteJsonCtx(r.Context(), w, http.StatusUnauthorized, map[string]string{
					"error": err.Error(),
				})
				return
			}

			ctx := context.WithValue(r.Context(), ctxUserIDKey{}, claims.UserID)
			next(w, r.WithContext(ctx))
		}
	}
}

func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if uid, ok := ctx.Value(ctxUserIDKey{}).(string); ok {
		return uid
	}
	return ""
}
