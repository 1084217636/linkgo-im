package svc

import (
	"database/sql"
	"time"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	"github.com/1084217636/linkgo-im/internal/ai"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config      config.Config
	Rdb         *redis.Client
	DB          *sql.DB
	LogicRouter *LogicRouterPool
	RestLimiter *authutil.TokenBucketLimiter
	WsLimiter   *authutil.TokenBucketLimiter
	GatewayID   string
	RouteTTL    time.Duration
	AckTimeout  time.Duration
	AckRetries  int
	RetryEvery  time.Duration
	AIProvider  ai.Provider
	AISummary   *ai.SummaryService
	AIAsk       *ai.AskService
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
	ackTimeoutSeconds := c.Gateway.AckTimeoutSeconds
	if ackTimeoutSeconds <= 0 {
		ackTimeoutSeconds = 5
	}
	ackRetries := c.Gateway.AckMaxRetries
	if ackRetries <= 0 {
		ackRetries = 3
	}
	retryIntervalSeconds := c.Gateway.RetryIntervalSeconds
	if retryIntervalSeconds <= 0 {
		retryIntervalSeconds = 1
	}

	var db *sql.DB
	if c.Database.Dsn != "" {
		var err error
		db, err = sql.Open("mysql", c.Database.Dsn)
		if err != nil {
			logx.Must(err)
		}
		maxOpen := c.Database.MaxOpenConns
		if maxOpen <= 0 {
			maxOpen = 80
		}
		maxIdle := c.Database.MaxIdleConns
		if maxIdle <= 0 {
			maxIdle = 20
		}
		if maxIdle > maxOpen {
			maxIdle = maxOpen
		}
		connMaxLifetime := c.Database.ConnMaxLifetimeSeconds
		if connMaxLifetime <= 0 {
			connMaxLifetime = 300
		}
		connMaxIdleTime := c.Database.ConnMaxIdleTimeSeconds
		if connMaxIdleTime <= 0 {
			connMaxIdleTime = 60
		}
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxIdle)
		db.SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)
		db.SetConnMaxIdleTime(time.Duration(connMaxIdleTime) * time.Second)
		logx.Must(db.Ping())
	}

	aiProvider := ai.NewProviderWithOptions(ai.ProviderOptions{
		Name:           c.AI.Provider,
		Model:          c.AI.Model,
		BaseURL:        c.AI.BaseURL,
		APIKey:         c.AI.APIKey,
		Timeout:        time.Duration(c.AI.TimeoutSeconds) * time.Second,
		FallbackToMock: c.AI.FallbackToMock,
	})
	knowledgeBase, err := ai.NewKnowledgeBase(c.AI.KnowledgePaths)
	if err != nil {
		logx.Errorf("load ai knowledge base: %v", err)
	}

	return &ServiceContext{
		Config: c,
		Rdb: redis.NewClient(&redis.Options{
			Addr:     c.Redis.Addr,
			Password: c.Redis.Password,
			DB:       0,
		}),
		DB:          db,
		LogicRouter: NewLogicRouter(c),
		RestLimiter: authutil.NewTokenBucketLimiter(20, 40),
		WsLimiter:   authutil.NewTokenBucketLimiter(5, 10),
		GatewayID:   gatewayID,
		RouteTTL:    time.Duration(ttlSeconds) * time.Second,
		AckTimeout:  time.Duration(ackTimeoutSeconds) * time.Second,
		AckRetries:  ackRetries,
		RetryEvery:  time.Duration(retryIntervalSeconds) * time.Second,
		AIProvider:  aiProvider,
		AISummary:   ai.NewSummaryService(db, aiProvider, c.AI.MaxMessages),
		AIAsk:       ai.NewAskService(db, aiProvider, knowledgeBase, c.AI.KnowledgeTopK),
	}
}

func (s *ServiceContext) Close() {
	if s == nil {
		return
	}
	if s.LogicRouter != nil {
		s.LogicRouter.Close()
	}
	if s.DB != nil {
		_ = s.DB.Close()
	}
	if s.Rdb != nil {
		_ = s.Rdb.Close()
	}
}
