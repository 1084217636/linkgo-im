package main

import (
	"context"
	"flag"
	"os"
	"strconv"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/handler"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/discovery"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "cmd/gateway/etc/gateway-api.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	overrideConfigFromEnv(&c)
	authutil.SetJWTSecret(c.Auth.AccessSecret)

	svcCtx := svc.NewServiceContext(c)
	defer svcCtx.Close()

	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cleaned, err := server.CleanupGatewayRoutes(runtimeCtx, svcCtx.Rdb, svcCtx.GatewayID); err != nil {
		logx.Errorf("cleanup gateway routes failed: %v", err)
	} else if cleaned > 0 {
		logx.Infow("cleanup stale gateway routes",
			logx.Field("gateway_id", svcCtx.GatewayID),
			logx.Field("count", cleaned),
		)
	}
	go server.StartGatewayHeartbeat(runtimeCtx, svcCtx.Rdb, svcCtx.GatewayID, svcCtx.RouteTTL)
	go server.StartPendingRetryLoop(runtimeCtx, svcCtx.Rdb, svcCtx.GatewayID, svcCtx.AckTimeout, svcCtx.AckRetries, svcCtx.RetryEvery)
	go serverSubscribe(runtimeCtx, svcCtx)

	restServer := rest.MustNewServer(c.RestConf, rest.WithCors())
	defer restServer.Stop()

	handler.RegisterHandlers(restServer, svcCtx)

	logx.Infof("Starting gateway server at %s:%d", c.Host, c.Port)
	restServer.Start()
}

func serverSubscribe(runtimeCtx context.Context, svcCtx *svc.ServiceContext) {
	if svcCtx == nil || svcCtx.Rdb == nil {
		return
	}
	server.Manager.SubscribeRedis(runtimeCtx, svcCtx.Rdb, svcCtx.GatewayID)
}

func overrideConfigFromEnv(c *config.Config) {
	if c == nil {
		return
	}

	if value := os.Getenv("GATEWAY_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			c.Port = port
		}
	}
	if value := os.Getenv("GATEWAY_ID"); value != "" {
		c.Gateway.ID = value
	}
	if value := os.Getenv("REDIS_ADDR"); value != "" {
		c.Redis.Addr = value
	}
	if value := os.Getenv("REDIS_PASSWORD"); value != "" {
		c.Redis.Password = value
	}
	if value := os.Getenv("DB_DSN"); value != "" {
		c.Database.Dsn = value
	}
	if value := os.Getenv("DB_MAX_OPEN_CONNS"); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			c.Database.MaxOpenConns = n
		}
	}
	if value := os.Getenv("DB_MAX_IDLE_CONNS"); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			c.Database.MaxIdleConns = n
		}
	}
	if value := os.Getenv("DB_CONN_MAX_LIFETIME_SECONDS"); value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.Database.ConnMaxLifetimeSeconds = n
		}
	}
	if value := os.Getenv("DB_CONN_MAX_IDLE_TIME_SECONDS"); value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.Database.ConnMaxIdleTimeSeconds = n
		}
	}
	if value := os.Getenv("ETCD_ENDPOINTS"); value != "" {
		c.Etcd.Endpoints = parseEndpoints(value)
	}
	if value := os.Getenv("LOGIC_ADDR"); value != "" {
		c.Logic.Addr = value
	}
	if value := os.Getenv("JWT_SECRET"); value != "" {
		c.Auth.AccessSecret = value
	}
	if value := os.Getenv("ROUTE_TTL_SECONDS"); value != "" {
		if ttl, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.Gateway.RouteTTLSeconds = ttl
		}
	}
	if value := os.Getenv("ACK_TIMEOUT_SECONDS"); value != "" {
		if ttl, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.Gateway.AckTimeoutSeconds = ttl
		}
	}
	if value := os.Getenv("ACK_MAX_RETRIES"); value != "" {
		if retries, err := strconv.Atoi(value); err == nil {
			c.Gateway.AckMaxRetries = retries
		}
	}
	if value := os.Getenv("RETRY_INTERVAL_SECONDS"); value != "" {
		if ttl, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.Gateway.RetryIntervalSeconds = ttl
		}
	}
	if value := os.Getenv("AI_PROVIDER"); value != "" {
		c.AI.Provider = value
	}
	if value := os.Getenv("AI_MODEL"); value != "" {
		c.AI.Model = value
	}
	if value := os.Getenv("AI_BASE_URL"); value != "" {
		c.AI.BaseURL = value
	}
	if value := os.Getenv("AI_API_KEY"); value != "" {
		c.AI.APIKey = value
	} else if value := os.Getenv("DEEPSEEK_API_KEY"); value != "" {
		c.AI.APIKey = value
	}
	if value := os.Getenv("AI_TIMEOUT_SECONDS"); value != "" {
		if ttl, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.AI.TimeoutSeconds = ttl
		}
	}
	if value := os.Getenv("AI_MAX_MESSAGES"); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			c.AI.MaxMessages = n
		}
	}
	if value := os.Getenv("AI_FALLBACK_TO_MOCK"); value != "" {
		if enabled, err := strconv.ParseBool(value); err == nil {
			c.AI.FallbackToMock = enabled
		}
	}
	if value := os.Getenv("AI_KNOWLEDGE_PATHS"); value != "" {
		c.AI.KnowledgePaths = parseEndpoints(value)
	}
	if value := os.Getenv("AI_KNOWLEDGE_TOP_K"); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			c.AI.KnowledgeTopK = n
		}
	}
}

func parseEndpoints(raw string) []string {
	return discovery.ParseEndpoints(raw)
}
