package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/redis/go-redis/v9"
)

type RedisDelivery struct {
	Rdb *redis.Client
}

func (d *RedisDelivery) Deliver(ctx context.Context, targetID, messageID string, payload []byte, now int64) error {
	encoded := base64.StdEncoding.EncodeToString(payload)
	if err := d.trackPendingAck(ctx, targetID, messageID, encoded, now); err != nil {
		metrics.OutboundMessages.WithLabelValues("pending_ack", "error").Inc()
		return err
	}

	routeKey := "route:" + targetID
	if routeValue, err := d.Rdb.Get(ctx, routeKey).Result(); err == nil && routeValue != "" {
		gatewayID := server.ParseGatewayID(routeValue)
		if gatewayID == "" {
			goto fallbackOffline
		}
		envelope, err := json.Marshal(server.PushEnvelope{
			TargetID:   targetID,
			PayloadB64: encoded,
		})
		if err == nil {
			channel := server.ChannelForGateway(gatewayID)
			if err := d.Rdb.Publish(ctx, channel, envelope).Err(); err == nil {
				metrics.OutboundMessages.WithLabelValues("pubsub", "success").Inc()
				return nil
			}
			metrics.OutboundMessages.WithLabelValues("pubsub", "error").Inc()
		}
	}

fallbackOffline:
	if err := d.Rdb.ZAdd(ctx, "offline_msg:"+targetID, redis.Z{
		Score:  float64(now),
		Member: encoded,
	}).Err(); err != nil {
		log.Printf("save offline message failed for user=%s: %v", targetID, err)
		metrics.OutboundMessages.WithLabelValues("offline", "error").Inc()
		return err
	}
	metrics.OutboundMessages.WithLabelValues("offline", "success").Inc()
	return nil
}

func (d *RedisDelivery) trackPendingAck(ctx context.Context, targetID, messageID, encoded string, now int64) error {
	if err := d.Rdb.ZAdd(ctx, "pending_ack:"+targetID, redis.Z{
		Score:  float64(now),
		Member: encoded,
	}).Err(); err != nil {
		log.Printf("save pending ack failed for user=%s: %v", targetID, err)
		return err
	}
	if err := d.Rdb.HSet(ctx, "ack_idx:"+targetID, messageID, encoded).Err(); err != nil {
		log.Printf("save ack index failed for user=%s: %v", targetID, err)
		return err
	}
	return nil
}
