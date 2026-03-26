package discovery

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type Registry struct {
	client    *clientv3.Client
	service   string
	nodeID    string
	addr      string
	leaseID   clientv3.LeaseID
	cancel    context.CancelFunc
	leaseTTL  int64
	keyPrefix string
}

type Resolver struct {
	client    *clientv3.Client
	keyPrefix string
}

func NewClient(endpoints []string) (*clientv3.Client, error) {
	return clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
}

func NewRegistry(client *clientv3.Client, service, nodeID, addr string) *Registry {
	return &Registry{
		client:    client,
		service:   service,
		nodeID:    nodeID,
		addr:      addr,
		leaseTTL:  10,
		keyPrefix: fmt.Sprintf("/services/%s/", service),
	}
}

func (r *Registry) Register(ctx context.Context) error {
	leaseResp, err := r.client.Grant(ctx, r.leaseTTL)
	if err != nil {
		return err
	}
	r.leaseID = leaseResp.ID

	key := r.keyPrefix + r.nodeID
	if _, err := r.client.Put(ctx, key, r.addr, clientv3.WithLease(r.leaseID)); err != nil {
		return err
	}

	keepCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	keepAliveCh, err := r.client.KeepAlive(keepCtx, r.leaseID)
	if err != nil {
		return err
	}

	go func() {
		for range keepAliveCh {
		}
	}()

	return nil
}

func (r *Registry) Close() {
	if r.cancel != nil {
		r.cancel()
	}
}

func NewResolver(client *clientv3.Client, service string) *Resolver {
	return &Resolver{
		client:    client,
		keyPrefix: fmt.Sprintf("/services/%s/", service),
	}
}

func (r *Resolver) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *Resolver) Pick(ctx context.Context, key string) (string, error) {
	resp, err := r.client.Get(ctx, r.keyPrefix, clientv3.WithPrefix())
	if err != nil {
		return "", err
	}
	if len(resp.Kvs) == 0 {
		return "", fmt.Errorf("no endpoint for %s", r.keyPrefix)
	}

	var picked string
	var maxScore uint64
	for _, kv := range resp.Kvs {
		candidate := string(kv.Value)
		score := rendezvousHash(key, candidate)
		if picked == "" || score > maxScore {
			picked = candidate
			maxScore = score
		}
	}
	return picked, nil
}

func ParseEndpoints(raw string) []string {
	parts := strings.Split(raw, ",")
	res := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			res = append(res, part)
		}
	}
	return res
}

func rendezvousHash(key, node string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte("::"))
	_, _ = h.Write([]byte(node))
	return h.Sum64()
}
