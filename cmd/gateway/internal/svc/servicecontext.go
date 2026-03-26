package svc

import (
	"time"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Config      config.Config
	Rdb         *redis.Client
	LogicRouter *LogicRouterPool
	RestLimiter *authutil.TokenBucketLimiter
	WsLimiter   *authutil.TokenBucketLimiter
	GatewayID   string
	RouteTTL    time.Duration
}

func NewServiceContext(c config.Config) *ServiceContext {
	ttlSeconds := c.Gateway.RouteTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 75
	}

	gatewayID := c.Gateway.ID
	if gatewayID == "" {
		gatewayID = "gateway-a"
	}

	return &ServiceContext{
		Config: c,
		Rdb: redis.NewClient(&redis.Options{
			Addr:     c.Redis.Addr,
			Password: c.Redis.Password,
			DB:       0,
		}),
		LogicRouter: NewLogicRouter(c),
		RestLimiter: authutil.NewTokenBucketLimiter(20, 40),
		WsLimiter:   authutil.NewTokenBucketLimiter(5, 10),
		GatewayID:   gatewayID,
		RouteTTL:    time.Duration(ttlSeconds) * time.Second,
	}
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}
	if s.LogicRouter != nil {
		s.LogicRouter.Close()
	}
	if s.Rdb != nil {
		_ = s.Rdb.Close()
	}
}
