package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type RedisDelivery struct {
	Rdb *redis.Client
}

var trackPendingScript = redis.NewScript(`
redis.call("ZADD", KEYS[1], "NX", ARGV[1], ARGV[2])
redis.call("HSETNX", KEYS[2], ARGV[2], ARGV[3])
redis.call("HSETNX", KEYS[3], ARGV[2], "0")
redis.call("EXPIRE", KEYS[1], ARGV[4])
redis.call("EXPIRE", KEYS[2], ARGV[4])
redis.call("EXPIRE", KEYS[3], ARGV[4])
return 1
`)

const pendingAckTTL = 7 * 24 * time.Hour

func (d *RedisDelivery) Deliver(ctx context.Context, targetID, messageID string, payload []byte, now int64) error {
	encoded := base64.StdEncoding.EncodeToString(payload)
	frame := server.DecodeWireMessage(payload)
	if err := d.trackPendingAck(ctx, targetID, messageID, encoded, now); err != nil {
		metrics.OutboundMessages.WithLabelValues("pending_ack", "error").Inc()
		return err
	}

	routeKey := server.RouteKey(targetID)
	if routeValue, err := d.Rdb.Get(ctx, routeKey).Result(); err == nil && routeValue != "" {
		gatewayID := server.ParseGatewayID(routeValue)
		if gatewayID == "" {
			goto fallbackOffline
		}
		envelope, err := json.Marshal(server.PushEnvelope{
			TargetID:   targetID,
			MessageID:  messageID,
			SessionID:  sessionIDFromFrame(frame),
			Seq:        seqFromFrame(frame),
			TraceID:    traceIDFromFrame(frame),
			GatewayID:  gatewayID,
			RouteValue: routeValue,
			SentAt:     now,
			PayloadB64: encoded,
		})
		if err == nil {
			channel := server.ChannelForGateway(gatewayID)
			subscribers, err := d.Rdb.Publish(ctx, channel, envelope).Result()
			if err == nil && subscribers > 0 {
				metrics.OutboundMessages.WithLabelValues("pubsub", "success").Inc()
				logx.Infow("message published to gateway",
					logx.Field("trace_id", traceIDFromFrame(frame)),
					logx.Field("message_id", messageID),
					logx.Field("seq", seqFromFrame(frame)),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", targetID),
				)
				return nil
			}
			metrics.OutboundMessages.WithLabelValues("pubsub", "error").Inc()
			if subscribers == 0 {
				_ = server.ClearRouteIfMatch(ctx, d.Rdb, targetID, routeValue)
			}
			if err != nil {
				logx.Errorw("publish to gateway failed",
					logx.Field("trace_id", traceIDFromFrame(frame)),
					logx.Field("message_id", messageID),
					logx.Field("seq", seqFromFrame(frame)),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", targetID),
					logx.Field("error", err.Error()),
				)
			}
		}
	}

fallbackOffline:
	server.MarkOffline(ctx, d.Rdb, targetID, messageID, now)
	metrics.OutboundMessages.WithLabelValues("offline", "success").Inc()
	logx.Infow("message saved for offline delivery",
		logx.Field("trace_id", traceIDFromFrame(frame)),
		logx.Field("message_id", messageID),
		logx.Field("seq", seqFromFrame(frame)),
		logx.Field("target_id", targetID),
	)
	return nil
}

func (d *RedisDelivery) trackPendingAck(ctx context.Context, targetID, messageID, encoded string, now int64) error {
	return trackPendingScript.Run(ctx, d.Rdb, []string{
		server.PendingAckKey(targetID),
		server.AckIndexKey(targetID),
		server.AckRetryKey(targetID),
	}, now, messageID, encoded, strconv.FormatInt(int64(pendingAckTTL.Seconds()), 10)).Err()
}

func traceIDFromFrame(frame interface{ GetTraceId() string }) string {
	if frame == nil {
		return ""
	}
	return frame.GetTraceId()
}

func sessionIDFromFrame(frame interface{ GetSessionId() string }) string {
	if frame == nil {
		return ""
	}
	return frame.GetSessionId()
}

func seqFromFrame(frame interface{ GetSeq() int64 }) int64 {
	if frame == nil {
		return 0
	}
	return frame.GetSeq()
}
