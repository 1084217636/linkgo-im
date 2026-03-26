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

	ctx := svc.NewServiceContext(c)
	defer ctx.Close()

	go serverSubscribe(ctx)

	server := rest.MustNewServer(c.RestConf, rest.WithCors())
	defer server.Stop()

	handler.RegisterHandlers(server, ctx)

	logx.Infof("starting gateway server at %s:%d", c.Host, c.Port)
	server.Start()
}

func serverSubscribe(ctx *svc.ServiceContext) {
	if ctx == nil || ctx.Rdb == nil {
		return
	}
	server.Manager.SubscribeRedis(context.Background(), ctx.Rdb, ctx.GatewayID)
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
}

func parseEndpoints(raw string) []string {
	return discovery.ParseEndpoints(raw)
}
