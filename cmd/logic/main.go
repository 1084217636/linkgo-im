package main

import (
	"flag"
	"os"
	"strconv"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/internal/config"
	logicserver "github.com/1084217636/linkgo-im/cmd/logic/internal/server"
	"github.com/1084217636/linkgo-im/cmd/logic/internal/svc"
	"github.com/1084217636/linkgo-im/internal/discovery"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

var configFile = flag.String("f", "cmd/logic/etc/logic.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	overrideConfigFromEnv(&c)
	authutil.SetJWTSecret(c.JWT.AccessSecret)

	ctx := svc.NewServiceContext(c)
	defer ctx.Close()

	server := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		api.RegisterLogicServer(grpcServer, logicserver.NewLogicServer(ctx))
	})
	defer server.Stop()

	logx.Infof("starting logic rpc server on %s", c.ListenOn)
	server.Start()
}

func overrideConfigFromEnv(c *config.Config) {
	if c == nil {
		return
	}

	if value := os.Getenv("RPC_PORT"); value != "" {
		c.ListenOn = "0.0.0.0:" + value
	}
	if value := os.Getenv("ETCD_ENDPOINTS"); value != "" {
		c.Etcd.Hosts = parseEndpoints(value)
	}
	if value := os.Getenv("REDIS_ADDR"); value != "" {
		c.Cache.Addr = value
	}
	if value := os.Getenv("REDIS_PASSWORD"); value != "" {
		c.Cache.Password = value
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
	if value := os.Getenv("KAFKA_BROKERS"); value != "" {
		c.Kafka.Brokers = parseEndpoints(value)
	}
	if value := os.Getenv("KAFKA_GROUP_TOPIC"); value != "" {
		c.Kafka.GroupTopic = value
	}
	if value := os.Getenv("JWT_SECRET"); value != "" {
		c.JWT.AccessSecret = value
	}
	if value := os.Getenv("CPU_THRESHOLD"); value != "" {
		if threshold, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.CpuThreshold = threshold
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
	if value := os.Getenv("AI_BOT_USER_ID"); value != "" {
		c.AI.BotUserID = value
	}
}

func parseEndpoints(raw string) []string {
	return discovery.ParseEndpoints(raw)
}
