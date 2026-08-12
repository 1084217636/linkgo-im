package svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type LogicRouterPool struct {
	client     api.LogicClient
	zrpcClient zrpc.Client
}

func NewLogicRouter(c config.Config) *LogicRouterPool {
	var conf zrpc.RpcClientConf
	if c.Logic.Addr != "" {
		conf = zrpc.NewDirectClientConf([]string{c.Logic.Addr}, "", "")
	} else {
		conf = zrpc.NewEtcdClientConf(c.Etcd.Endpoints, "/services/logic", "", "")
	}
	conf.NonBlock = true
	conf.Timeout = int64((2 * time.Second).Milliseconds())
	conf.BalancerName = "p2c_ewma"

	client, err := zrpc.NewClient(conf, zrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logx.Must(err)
	}

	return &LogicRouterPool{
		client:     api.NewLogicClient(client.Conn()),
		zrpcClient: client,
	}
}

func (p *LogicRouterPool) GetClient(ctx context.Context, key string) (api.LogicClient, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("logic client unavailable")
	}
	return p.client, nil
}

func (p *LogicRouterPool) Ready(ctx context.Context) error {
	if p == nil || p.zrpcClient == nil || p.zrpcClient.Conn() == nil {
		return errors.New("logic connection unavailable")
	}
	conn := p.zrpcClient.Conn()
	if err := waitForLogicReady(ctx, conn); err != nil {
		return err
	}
	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("logic dependency health check failed: %w", err)
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("logic dependency health status is %s", response.GetStatus().String())
	}
	return nil
}

type logicConnectionState interface {
	Connect()
	GetState() connectivity.State
	WaitForStateChange(context.Context, connectivity.State) bool
}

func waitForLogicReady(ctx context.Context, conn logicConnectionState) error {
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return errors.New("logic connection is shut down")
		}
		if !conn.WaitForStateChange(ctx, state) {
			return fmt.Errorf("logic connection not ready: %s: %w", state, ctx.Err())
		}
	}
}

func (p *LogicRouterPool) Close() {
	if p == nil {
		return
	}
	if p.zrpcClient != nil {
		_ = p.zrpcClient.Conn().Close()
	}
}
