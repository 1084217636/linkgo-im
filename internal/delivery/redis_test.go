package delivery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

func TestDeliverTracksPendingBeforePublishingToGateway(t *testing.T) {
	redisServer := miniredis.RunT(t)
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	const (
		uid        = "1002"
		messageID  = "c2c:1001:1002-1"
		gatewayID  = "gateway-b"
		routeValue = "gateway-b|conn-1"
	)
	if err := server.ClaimRoute(ctx, rdb, uid, routeValue, time.Minute); err != nil {
		t.Fatalf("ClaimRoute error = %v", err)
	}
	pubsub := rdb.Subscribe(ctx, server.ChannelForGateway(gatewayID))
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe error = %v", err)
	}

	payload := mustWirePayload(t, messageID)
	delivery := &RedisDelivery{Rdb: rdb}
	if err := delivery.Deliver(ctx, uid, messageID, payload, 100); err != nil {
		t.Fatalf("Deliver error = %v", err)
	}

	message, err := pubsub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("ReceiveMessage error = %v", err)
	}
	var envelope server.PushEnvelope
	if err := json.Unmarshal([]byte(message.Payload), &envelope); err != nil {
		t.Fatalf("decode push envelope: %v", err)
	}
	if envelope.TargetID != uid || envelope.MessageID != messageID || envelope.RouteValue != routeValue {
		t.Fatalf("push envelope = %#v", envelope)
	}
	assertPendingState(t, ctx, rdb, uid, messageID)
	if _, err := rdb.ZScore(ctx, server.OfflineMessageKey(uid), messageID).Result(); err != redis.Nil {
		t.Fatalf("online message was marked offline: %v", err)
	}
}

func TestDeliverWithoutRouteStoresOfflineState(t *testing.T) {
	redisServer := miniredis.RunT(t)
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	const (
		uid       = "1002"
		messageID = "c2c:1001:1002-2"
	)
	delivery := &RedisDelivery{Rdb: rdb}
	if err := delivery.Deliver(ctx, uid, messageID, mustWirePayload(t, messageID), 200); err != nil {
		t.Fatalf("Deliver error = %v", err)
	}

	assertPendingState(t, ctx, rdb, uid, messageID)
	if _, err := rdb.ZScore(ctx, server.OfflineMessageKey(uid), messageID).Result(); err != nil {
		t.Fatalf("offline marker missing: %v", err)
	}
}

func TestDeliverWithNoGatewaySubscriberClearsOnlyMatchingRoute(t *testing.T) {
	redisServer := miniredis.RunT(t)
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	const (
		uid        = "1002"
		messageID  = "c2c:1001:1002-3"
		routeValue = "gateway-dead|conn-1"
	)
	if err := server.ClaimRoute(ctx, rdb, uid, routeValue, time.Minute); err != nil {
		t.Fatalf("ClaimRoute error = %v", err)
	}
	delivery := &RedisDelivery{Rdb: rdb}
	if err := delivery.Deliver(ctx, uid, messageID, mustWirePayload(t, messageID), 300); err != nil {
		t.Fatalf("Deliver error = %v", err)
	}
	if _, err := rdb.Get(ctx, server.RouteKey(uid)).Result(); err != redis.Nil {
		t.Fatalf("dead route was not cleared: %v", err)
	}
	if _, err := rdb.ZScore(ctx, server.OfflineMessageKey(uid), messageID).Result(); err != nil {
		t.Fatalf("offline marker missing: %v", err)
	}

	newRoute := "gateway-new|conn-2"
	if err := server.ClaimRoute(ctx, rdb, uid, newRoute, time.Minute); err != nil {
		t.Fatalf("ClaimRoute(new) error = %v", err)
	}
	if err := server.ClearRouteIfMatch(ctx, rdb, uid, routeValue); err != nil {
		t.Fatalf("ClearRouteIfMatch(stale) error = %v", err)
	}
	got, err := rdb.Get(ctx, server.RouteKey(uid)).Result()
	if err != nil {
		t.Fatalf("read current route: %v", err)
	}
	if got != newRoute {
		t.Fatalf("current route = %q, want %q", got, newRoute)
	}
}

func mustWirePayload(t *testing.T, messageID string) []byte {
	t.Helper()
	payload, err := proto.Marshal(&api.WireMessage{
		MessageId: messageID,
		SessionId: "c2c:1001:1002",
		Seq:       1,
		From:      "1001",
		To:        "1002",
		ToType:    "user",
		Body:      "hello",
		TraceId:   "trace-1",
	})
	if err != nil {
		t.Fatalf("proto.Marshal error = %v", err)
	}
	return payload
}

func assertPendingState(t *testing.T, ctx context.Context, rdb *redis.Client, uid, messageID string) {
	t.Helper()
	if _, err := rdb.ZScore(ctx, server.PendingAckKey(uid), messageID).Result(); err != nil {
		t.Fatalf("pending ACK missing: %v", err)
	}
	exists, err := rdb.HExists(ctx, server.AckIndexKey(uid), messageID).Result()
	if err != nil {
		t.Fatalf("read ACK index: %v", err)
	}
	if !exists {
		t.Fatalf("ACK payload missing for %s", messageID)
	}
}
