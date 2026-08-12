package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/1084217636/linkgo-im/cmd/logic/internal/svc"
	"github.com/1084217636/linkgo-im/internal/health"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const defaultLogicHealthPort = "9002"

// newLogicHealthServer exposes a separate HTTP health surface from the gRPC
// API. Kubernetes uses /readyz to remove a Logic pod from service when one of
// its message dependencies is unavailable, while /healthz only answers
// whether the process is alive.
func newLogicHealthServer(svcCtx *svc.ServiceContext, brokers []string, addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health.LiveHandler())
	mux.HandleFunc("/readyz", health.ReadyHandler(logicReadinessChecks(svcCtx, brokers)))
	mux.Handle("/metrics", metrics.Handler())
	return &http.Server{Addr: addr, Handler: mux}
}

func logicReadinessChecks(svcCtx *svc.ServiceContext, brokers []string) map[string]health.Check {
	return map[string]health.Check{
		"redis": func(ctx context.Context) error {
			if svcCtx == nil || svcCtx.Rdb == nil {
				return errors.New("redis client is nil")
			}
			return svcCtx.Rdb.Ping(ctx).Err()
		},
		"mysql": func(ctx context.Context) error {
			if svcCtx == nil || svcCtx.DB == nil {
				return errors.New("mysql client is nil")
			}
			return svcCtx.DB.PingContext(ctx)
		},
		"kafka": func(ctx context.Context) error {
			if len(brokers) == 0 {
				return errors.New("kafka brokers are empty")
			}
			conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
			if err != nil {
				return err
			}
			return conn.Close()
		},
	}
}

// logicGRPCHealthService lets a Gateway discover dependency failure through
// the same gRPC connection it uses for business calls. A TCP-ready channel is
// not enough: without this check a Logic process whose Redis/Kafka is down
// could still receive traffic from an Etcd-discovered client.
type logicGRPCHealthService struct {
	healthpb.UnimplementedHealthServer
	checks map[string]health.Check
}

func newLogicGRPCHealthService(svcCtx *svc.ServiceContext, brokers []string) *logicGRPCHealthService {
	return &logicGRPCHealthService{checks: logicReadinessChecks(svcCtx, brokers)}
}

func (s *logicGRPCHealthService) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	for name, check := range s.checks {
		if err := check(ctx); err != nil {
			return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}, status.Error(codes.Unavailable, name+" not ready")
		}
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func startLogicHealthServer(svcCtx *svc.ServiceContext, brokers []string, port string) func() {
	server := newLogicHealthServer(svcCtx, brokers, ":"+port)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("logic health server stopped: %v", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}
