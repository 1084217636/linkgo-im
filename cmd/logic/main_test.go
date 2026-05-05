package main

import (
	"testing"

	"github.com/1084217636/linkgo-im/cmd/logic/internal/config"
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
	if c.Redis.Addr != "redis.test:6379" || c.Redis.Password != "secret" {
		t.Fatalf("Redis config = %#v", c.Redis)
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
	if c.Auth.AccessSecret != "jwt-secret" {
		t.Fatalf("Auth.AccessSecret = %q", c.Auth.AccessSecret)
	}
	if c.CpuThreshold != 900 {
		t.Fatalf("CpuThreshold = %d, want 900", c.CpuThreshold)
	}
}
