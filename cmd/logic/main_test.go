package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/1084217636/linkgo-im/cmd/logic/internal/config"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestOverrideConfigFromEnv(t *testing.T) {
	t.Setenv("RPC_PORT", "19001")
	t.Setenv("ETCD_ENDPOINTS", "etcd-a:2379,etcd-b:2379")
	t.Setenv("REDIS_ADDR", "redis.test:6379")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("DB_DSN", "root:root@tcp(mysql:3306)/linkgo_im")
	t.Setenv("KAFKA_BROKERS", "kafka-a:9092,kafka-b:9092")
	t.Setenv("KAFKA_GROUP_TOPIC", "group-topic")
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("CPU_THRESHOLD", "900")

	c := config.Config{}
	overrideConfigFromEnv(&c)

	if c.ListenOn != "0.0.0.0:19001" {
		t.Fatalf("ListenOn = %q", c.ListenOn)
	}
	if got := c.Etcd.Hosts; len(got) != 2 || got[0] != "etcd-a:2379" || got[1] != "etcd-b:2379" {
		t.Fatalf("Etcd.Hosts = %#v", got)
	}
	if c.Cache.Addr != "redis.test:6379" || c.Cache.Password != "secret" {
		t.Fatalf("Cache config = %#v", c.Cache)
	}
	if c.Database.Dsn != "root:root@tcp(mysql:3306)/linkgo_im" {
		t.Fatalf("Database.Dsn = %q", c.Database.Dsn)
	}
	if got := c.Kafka.Brokers; len(got) != 2 || got[0] != "kafka-a:9092" || got[1] != "kafka-b:9092" {
		t.Fatalf("Kafka.Brokers = %#v", got)
	}
	if c.Kafka.GroupTopic != "group-topic" {
		t.Fatalf("Kafka.GroupTopic = %q", c.Kafka.GroupTopic)
	}
	if c.JWT.AccessSecret != "jwt-secret" {
		t.Fatalf("JWT.AccessSecret = %q", c.JWT.AccessSecret)
	}
	if c.CpuThreshold != 900 {
		t.Fatalf("CpuThreshold = %d, want 900", c.CpuThreshold)
	}
}

func TestLogicHealthServerSeparatesLivenessAndReadiness(t *testing.T) {
	server := newLogicHealthServer(nil, nil, ":0")

	live := httptest.NewRecorder()
	server.Handler.ServeHTTP(live, httptest.NewRequest("GET", "/healthz", nil))
	if live.Code != 200 {
		t.Fatalf("healthz status = %d, want 200", live.Code)
	}

	ready := httptest.NewRecorder()
	server.Handler.ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 503 {
		t.Fatalf("readyz status = %d, want 503 when dependencies are unavailable", ready.Code)
	}
}

func TestLogicGRPCHealthFailsClosedWithoutDependencies(t *testing.T) {
	service := newLogicGRPCHealthService(nil, nil)
	response, err := service.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err == nil {
		t.Fatal("Check() error = nil, want dependency failure")
	}
	if response.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health status = %s, want NOT_SERVING", response.GetStatus())
	}
}
