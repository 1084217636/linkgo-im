package svc

import (
	"context"
	"sync"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
	"github.com/1084217636/linkgo-im/internal/discovery"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type logicClientEntry struct {
	conn   *grpc.ClientConn
	client api.LogicClient
}

type LogicRouterPool struct {
	directAddr string
	resolver   *discovery.Resolver
	mu         sync.Mutex
	clients    map[string]*logicClientEntry
}

func NewLogicRouter(c config.Config) *LogicRouterPool {
	if c.Logic.Addr != "" {
		return &LogicRouterPool{
			directAddr: c.Logic.Addr,
			clients:    map[string]*logicClientEntry{},
		}
	}

	client, err := discovery.NewClient(c.Etcd.Endpoints)
	if err != nil {
		logx.Must(err)
	}

	return &LogicRouterPool{
		resolver: discovery.NewResolver(client, "logic"),
		clients:  map[string]*logicClientEntry{},
	}
}

func (p *LogicRouterPool) GetClient(ctx context.Context, key string) (api.LogicClient, error) {
	addr := p.directAddr
	var err error
	if addr == "" {
		addr, err = p.resolver.Pick(ctx, key)
		if err != nil {
			return nil, err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.clients[addr]; ok {
		return entry.client, nil
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	entry := &logicClientEntry{
		conn:   conn,
		client: api.NewLogicClient(conn),
	}
	p.clients[addr] = entry
	return entry.client, nil
}

func (p *LogicRouterPool) Close() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, entry := range p.clients {
		if entry != nil && entry.conn != nil {
			_ = entry.conn.Close()
		}
		delete(p.clients, addr)
	}
	if p.resolver != nil {
		_ = p.resolver.Close()
	}
}
