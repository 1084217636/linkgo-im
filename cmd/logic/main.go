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
	authutil.SetJWTSecret(c.Auth.AccessSecret)

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
		c.Redis.Addr = value
	}
	if value := os.Getenv("REDIS_PASSWORD"); value != "" {
		c.Redis.Password = value
	}
	if value := os.Getenv("DB_DSN"); value != "" {
		c.Database.Dsn = value
	}
	if value := os.Getenv("KAFKA_BROKERS"); value != "" {
		c.Kafka.Brokers = parseEndpoints(value)
	}
	if value := os.Getenv("KAFKA_GROUP_TOPIC"); value != "" {
		c.Kafka.GroupTopic = value
	}
	if value := os.Getenv("JWT_SECRET"); value != "" {
		c.Auth.AccessSecret = value
	}
	if value := os.Getenv("CPU_THRESHOLD"); value != "" {
		if threshold, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.CpuThreshold = threshold
		}
	}
}

func parseEndpoints(raw string) []string {
	return discovery.ParseEndpoints(raw)
}
