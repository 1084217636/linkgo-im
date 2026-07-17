package main

import (
	"testing"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/config"
)

func TestOverrideConfigFromEnv(t *testing.T) {
	t.Setenv("GATEWAY_PORT", "18090")
	t.Setenv("GATEWAY_ID", "gateway-test")
	t.Setenv("REDIS_ADDR", "redis.test:6379")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("DB_DSN", "root:root@tcp(mysql:3306)/linkgo_im")
	t.Setenv("DB_MAX_OPEN_CONNS", "120")
	t.Setenv("DB_MAX_IDLE_CONNS", "30")
	t.Setenv("DB_CONN_MAX_LIFETIME_SECONDS", "600")
	t.Setenv("DB_CONN_MAX_IDLE_TIME_SECONDS", "90")
	t.Setenv("ETCD_ENDPOINTS", "etcd-a:2379,etcd-b:2379")
	t.Setenv("LOGIC_ADDR", "logic:9001")
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("ROUTE_TTL_SECONDS", "120")
	t.Setenv("ACK_TIMEOUT_SECONDS", "7")
	t.Setenv("ACK_MAX_RETRIES", "4")
	t.Setenv("RETRY_INTERVAL_SECONDS", "2")
	t.Setenv("WS_ALLOWED_ORIGINS", "https://app.example.com,https://admin.example.com")
	t.Setenv("WS_ALLOW_MISSING_ORIGIN", "true")

	c := config.Config{}
	overrideConfigFromEnv(&c)

	if c.Port != 18090 {
		t.Fatalf("Port = %d, want 18090", c.Port)
	}
	if c.Gateway.ID != "gateway-test" {
		t.Fatalf("Gateway.ID = %q", c.Gateway.ID)
	}
	if c.Redis.Addr != "redis.test:6379" || c.Redis.Password != "secret" {
		t.Fatalf("Redis config = %#v", c.Redis)
	}
	if c.Database.Dsn != "root:root@tcp(mysql:3306)/linkgo_im" {
		t.Fatalf("Database.Dsn = %q", c.Database.Dsn)
	}
	if c.Database.MaxOpenConns != 120 || c.Database.MaxIdleConns != 30 {
		t.Fatalf("Database pool = open %d idle %d", c.Database.MaxOpenConns, c.Database.MaxIdleConns)
	}
	if c.Database.ConnMaxLifetimeSeconds != 600 || c.Database.ConnMaxIdleTimeSeconds != 90 {
		t.Fatalf("Database conn lifetime = %d idle = %d", c.Database.ConnMaxLifetimeSeconds, c.Database.ConnMaxIdleTimeSeconds)
	}
	if got := c.Etcd.Endpoints; len(got) != 2 || got[0] != "etcd-a:2379" || got[1] != "etcd-b:2379" {
		t.Fatalf("Etcd.Endpoints = %#v", got)
	}
	if c.Logic.Addr != "logic:9001" {
		t.Fatalf("Logic.Addr = %q", c.Logic.Addr)
	}
	if c.Auth.AccessSecret != "jwt-secret" {
		t.Fatalf("Auth.AccessSecret = %q", c.Auth.AccessSecret)
	}
	if c.Gateway.RouteTTLSeconds != 120 {
		t.Fatalf("RouteTTLSeconds = %d, want 120", c.Gateway.RouteTTLSeconds)
	}
	if c.Gateway.AckTimeoutSeconds != 7 {
		t.Fatalf("AckTimeoutSeconds = %d, want 7", c.Gateway.AckTimeoutSeconds)
	}
	if c.Gateway.AckMaxRetries != 4 {
		t.Fatalf("AckMaxRetries = %d, want 4", c.Gateway.AckMaxRetries)
	}
	if c.Gateway.RetryIntervalSeconds != 2 {
		t.Fatalf("RetryIntervalSeconds = %d, want 2", c.Gateway.RetryIntervalSeconds)
	}
	if got := c.Gateway.AllowedOrigins; len(got) != 2 || got[0] != "https://app.example.com" || got[1] != "https://admin.example.com" {
		t.Fatalf("Gateway.AllowedOrigins = %#v", got)
	}
	if !c.Gateway.AllowMissingOrigin {
		t.Fatal("Gateway.AllowMissingOrigin = false, want true")
	}
}
