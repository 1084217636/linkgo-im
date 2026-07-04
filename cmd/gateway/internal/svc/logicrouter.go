package svc

import (
	"context"
	"errors"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/credentials/insecure"
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

func (p *LogicRouterPool) Close() {
	if p == nil {
		return
	}
	if p.zrpcClient != nil {
		_ = p.zrpcClient.Conn().Close()
	}
}
