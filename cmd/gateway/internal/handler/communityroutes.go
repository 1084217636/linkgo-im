package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	gw "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/community"
	coreServer "github.com/1084217636/linkgo-im/internal/server"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

var communityRequests = promauto.NewCounterVec(prometheus.CounterOpts{Name: "linkgo_community_requests_total", Help: "Community API outcomes."}, []string{"operation", "status_class"})
var communityLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: "linkgo_community_duration_seconds", Help: "Community API latency.", Buckets: prometheus.DefBuckets}, []string{"operation"})

type communityAPI struct {
	service *community.Service
	svc     *svc.ServiceContext
}
type communityHandler func(context.Context, string, map[string]string, *http.Request) (any, error)

func (a *communityAPI) wrap(op string, fn communityHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		requestID := uuid.NewString()
		w.Header().Set("X-Request-ID", requestID)
		value, err := fn(ctx, gw.UserIDFromContext(ctx), pathvar.Vars(r), r)
		status := 200
		if err != nil {
			status = community.Status(err)
			message := err.Error()
			if status >= 500 {
				message = "社区服务暂不可用"
			}
			value = map[string]string{"error": message, "request_id": requestID}
		}
		communityRequests.WithLabelValues(op, strconv.Itoa(status/100)+"xx").Inc()
		communityLatency.WithLabelValues(op).Observe(time.Since(started).Seconds())
		logx.Infow("community request", logx.Field("request_id", requestID), logx.Field("operation", op), logx.Field("status", status), logx.Field("duration_ms", time.Since(started).Milliseconds()))
		taskWrite(w, status, value)
	}
}
func communityBody(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 96<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return &community.Error{Code: 400, Message: "invalid JSON: " + err.Error()}
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		return &community.Error{Code: 400, Message: "expected one JSON object"}
	}
	return nil
}
func RegisterCommunityRoutes(server *rest.Server, s *svc.ServiceContext) {
	a := &communityAPI{service: &community.Service{DB: s.DB}, svc: s}
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{gw.NewRateLimitMiddleware(s.RestLimiter).Handle}, rest.Route{Method: "POST", Path: "/register", Handler: a.wrap("register", func(ctx context.Context, _ string, _ map[string]string, r *http.Request) (any, error) {
		var b struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if e := communityBody(r, &b); e != nil {
			return nil, e
		}
		return a.service.Register(ctx, b.Username, b.Password)
	})}), rest.WithPrefix("/api/v1"))
	routes := []rest.Route{
		{Method: "GET", Path: "/communities", Handler: a.wrap("list", func(ctx context.Context, u string, _ map[string]string, r *http.Request) (any, error) {
			items, e := a.service.List(ctx, u, r.URL.Query().Get("mine") == "1")
			return map[string]any{"communities": items}, e
		})},
		{Method: "POST", Path: "/communities", Handler: a.wrap("create", func(ctx context.Context, u string, _ map[string]string, r *http.Request) (any, error) {
			var in community.Input
			if e := communityBody(r, &in); e != nil {
				return nil, e
			}
			return a.service.Create(ctx, u, in)
		})},
		{Method: "GET", Path: "/communities/:group_id/posts", Handler: a.wrap("feed", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.Posts(ctx, u, p["group_id"], r.URL.Query().Get("cursor"), r.URL.Query().Get("kind"), r.URL.Query().Get("filter"))
		})},
		{Method: "POST", Path: "/communities/:group_id/posts", Handler: a.wrap("post", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			var in community.Input
			if e := communityBody(r, &in); e != nil {
				return nil, e
			}
			return a.service.CreatePost(ctx, u, p["group_id"], in)
		})},
		{Method: "GET", Path: "/posts/:post_id", Handler: a.wrap("detail", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.Post(ctx, u, p["post_id"])
		})},
		{Method: "PATCH", Path: "/posts/:post_id", Handler: a.wrap("edit", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			var in community.Input
			if e := communityBody(r, &in); e != nil {
				return nil, e
			}
			return a.service.EditPost(ctx, u, p["post_id"], in)
		})},
		{Method: "GET", Path: "/posts/:post_id/comments", Handler: a.wrap("comments", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.Comments(ctx, u, p["post_id"])
		})},
		{Method: "POST", Path: "/posts/:post_id/comments", Handler: a.wrap("comment", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			var in community.Input
			if e := communityBody(r, &in); e != nil {
				return nil, e
			}
			return a.service.Comment(ctx, u, p["post_id"], in)
		})},
		{Method: "PUT", Path: "/posts/:post_id/like", Handler: a.wrap("like", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.Like(ctx, u, p["post_id"], true)
		})},
		{Method: "DELETE", Path: "/posts/:post_id/like", Handler: a.wrap("unlike", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.Like(ctx, u, p["post_id"], false)
		})},
		{Method: "GET", Path: "/notifications", Handler: a.wrap("notifications", func(ctx context.Context, u string, _ map[string]string, r *http.Request) (any, error) {
			after, e := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			if r.URL.Query().Get("after") == "" {
				after = 0
				e = nil
			}
			if e != nil || after < 0 {
				return nil, &community.Error{Code: 400, Message: "invalid cursor"}
			}
			return a.service.Notifications(ctx, u, after)
		})},
		{Method: "POST", Path: "/notifications/:notification_id/read", Handler: a.wrap("read", func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			return a.service.ReadNotification(ctx, u, p["notification_id"])
		})},
	}
	for _, action := range []string{"join", "leave", "remove", "transfer"} {
		action := action
		routes = append(routes, rest.Route{Method: "POST", Path: "/communities/:group_id/" + action, Handler: a.wrap(action, func(ctx context.Context, u string, p map[string]string, r *http.Request) (any, error) {
			var in community.Input
			if e := communityBody(r, &in); e != nil {
				return nil, e
			}
			result, e := a.service.Membership(ctx, u, p["group_id"], action, in)
			if e == nil {
				cache, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if s.Rdb != nil {
					for _, uid := range []string{u, in.UserID} {
						if uid == "" {
							continue
						}
						_ = s.Rdb.Del(cache, "group_members:"+p["group_id"], "user_groups:"+uid, coreServer.UserConversationsKey(uid), coreServer.UserConversationReadKey(uid), coreServer.UserConversationAckedKey(uid)).Err()
					}
				}
			}
			return result, e
		})})
	}
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{gw.NewAuthMiddleware().Handle, gw.NewRateLimitMiddleware(s.RestLimiter).Handle}, routes...), rest.WithPrefix("/api/v1"))
}
