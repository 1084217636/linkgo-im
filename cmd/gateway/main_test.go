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
	t.Setenv("ETCD_ENDPOINTS", "etcd-a:2379,etcd-b:2379")
	t.Setenv("LOGIC_ADDR", "logic:9001")
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("ROUTE_TTL_SECONDS", "120")

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
}
