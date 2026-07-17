package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type ctxRoleKey struct{}

type RoleMiddleware struct {
	db      *sql.DB
	allowed map[string]struct{}
}

func NewRoleMiddleware(db *sql.DB, roles ...string) *RoleMiddleware {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if normalized := strings.ToLower(strings.TrimSpace(role)); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return &RoleMiddleware{db: db, allowed: allowed}
}

func (m *RoleMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		if uid == "" {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		if m == nil || m.db == nil {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusServiceUnavailable, map[string]string{"error": "authorization service unavailable"})
			return
		}

		var role string
		err := m.db.QueryRowContext(r.Context(), `
SELECT role
FROM platform_user_roles
WHERE user_id = ? AND status = 'active'
LIMIT 1
`, uid).Scan(&role)
		if err != nil {
			if err != sql.ErrNoRows {
				logx.Errorw("load platform role failed", logx.Field("target_id", uid), logx.Field("error", err.Error()))
			}
			httpx.WriteJsonCtx(r.Context(), w, http.StatusForbidden, map[string]string{"error": "insufficient platform role"})
			return
		}

		role = strings.ToLower(strings.TrimSpace(role))
		_, allowed := m.allowed[role]
		if role != "admin" && !allowed {
			httpx.WriteJsonCtx(r.Context(), w, http.StatusForbidden, map[string]string{"error": "insufficient platform role"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxRoleKey{}, role)))
	}
}

func RoleFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	role, _ := ctx.Value(ctxRoleKey{}).(string)
	return role
}
