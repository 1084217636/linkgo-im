package server

import (
	"context"
	"encoding/base64"
	"strconv"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

func StartPendingRetryLoop(ctx context.Context, rdb *redis.Client, gatewayID string, ackTimeout time.Duration, maxRetries int, interval time.Duration) {
	if rdb == nil || gatewayID == "" || ackTimeout <= 0 || maxRetries <= 0 {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			retryGatewayPending(ctx, rdb, gatewayID, ackTimeout, maxRetries)
		}
	}
}

func retryGatewayPending(ctx context.Context, rdb *redis.Client, gatewayID string, ackTimeout time.Duration, maxRetries int) {
	users, err := rdb.SMembers(ctx, GatewayUsersKey(gatewayID)).Result()
	if err != nil && err != redis.Nil {
		logx.Errorw("load gateway users for ack retry failed",
			logx.Field("gateway_id", gatewayID),
			logx.Field("error", err.Error()),
		)
		return
	}

	deadline := time.Now().Add(-ackTimeout).UnixMilli()
	for _, uid := range users {
		routeValue, err := rdb.Get(ctx, RouteKey(uid)).Result()
		if err == redis.Nil {
			_ = rdb.SRem(ctx, GatewayUsersKey(gatewayID), uid).Err()
			continue
		}
		if err != nil {
			continue
		}
		if ParseGatewayID(routeValue) != gatewayID {
			_ = rdb.SRem(ctx, GatewayUsersKey(gatewayID), uid).Err()
			continue
		}

		conn, ok := Manager.GetConn(uid)
		if !ok {
			continue
		}

		messageIDs, err := rdb.ZRangeByScore(ctx, PendingAckKey(uid), &redis.ZRangeBy{
			Min:    "-inf",
			Max:    strconv.FormatInt(deadline, 10),
			Offset: 0,
			Count:  50,
		}).Result()
		if err != nil {
			continue
		}

		for _, messageID := range messageIDs {
			retryOnePending(ctx, rdb, gatewayID, uid, routeValue, conn, messageID, maxRetries)
		}
	}
}

func retryOnePending(ctx context.Context, rdb *redis.Client, gatewayID, uid, routeValue string, conn *ClientConn, messageID string, maxRetries int) {
	attempts, err := rdb.HIncrBy(ctx, AckRetryKey(uid), messageID, 1).Result()
	if err != nil {
		return
	}
	if attempts > int64(maxRetries) {
		MarkOffline(ctx, rdb, uid, messageID, time.Now().UnixMilli())
		metrics.AckOperations.WithLabelValues("retry_exhausted").Inc()
		logx.Errorw("ack retry exhausted",
			logx.Field("message_id", messageID),
			logx.Field("gateway_id", gatewayID),
			logx.Field("target_id", uid),
			logx.Field("attempt", attempts),
		)
		_ = rdb.ZAdd(ctx, PendingAckKey(uid), redis.Z{
			Score:  float64(time.Now().Add(time.Minute).UnixMilli()),
			Member: messageID,
		}).Err()
		return
	}

	encoded, err := rdb.HGet(ctx, AckIndexKey(uid), messageID).Result()
	if err != nil {
		return
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return
	}
	frame := DecodeWireMessage(payload)

	if err := conn.WriteBinary(payload); err != nil {
		MarkOffline(ctx, rdb, uid, messageID, time.Now().UnixMilli())
		_ = ClearRouteIfMatch(ctx, rdb, uid, routeValue)
		_ = conn.Close()
		Manager.Remove(uid, conn)
		metrics.AckOperations.WithLabelValues("retry_write_error").Inc()
		logx.Errorw("ack retry websocket write failed",
			wireLogField("trace_id", frame),
			logx.Field("message_id", messageID),
			wireSeqField(frame),
			logx.Field("gateway_id", gatewayID),
			logx.Field("target_id", uid),
			logx.Field("attempt", attempts),
			logx.Field("error", err.Error()),
		)
		return
	}

	_ = rdb.ZAdd(ctx, PendingAckKey(uid), redis.Z{
		Score:  float64(time.Now().UnixMilli()),
		Member: messageID,
	}).Err()
	metrics.AckOperations.WithLabelValues("retry_success").Inc()
	logx.Infow("ack timeout retry pushed",
		wireLogField("trace_id", frame),
		logx.Field("message_id", messageID),
		wireSeqField(frame),
		logx.Field("gateway_id", gatewayID),
		logx.Field("target_id", uid),
		logx.Field("attempt", attempts),
	)
}

func wireLogField(key string, frame *api.WireMessage) logx.LogField {
	if frame == nil {
		return logx.Field(key, "")
	}
	return logx.Field(key, frame.TraceId)
}

func wireSeqField(frame *api.WireMessage) logx.LogField {
	if frame == nil {
		return logx.Field("seq", int64(0))
	}
	return logx.Field("seq", frame.Seq)
}
