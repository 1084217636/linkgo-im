package middleware

import (
	"net"
	"net/http"

	"github.com/1084217636/linkgo-im/internal/metrics"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func RateLimitMiddleware(limiter *authutil.TokenBucketLimiter) func(next http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next(w, r)
				return
			}

			key := UserIDFromContext(r.Context())
			if key == "" {
				key = clientIP(r)
			}

			if !limiter.Allow(key) {
				metrics.RateLimitHits.WithLabelValues(r.URL.Path).Inc()
				httpx.WriteJsonCtx(r.Context(), w, http.StatusTooManyRequests, map[string]string{
					"error": "rate limit exceeded",
				})
				return
			}

			next(w, r)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
