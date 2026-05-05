package server

import (
	"context"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

var pushPool = NewPushWorkerPool(64, 4096)

func StartClientLoop(
	ctx context.Context,
	uid string,
	conn *ClientConn,
	logic api.LogicClient,
	rdb *redis.Client,
	routeValue string,
	routeTTL time.Duration,
) {
	conn.Conn.SetReadLimit(64 << 10)
	_ = conn.Conn.SetReadDeadline(time.Now().Add(routeTTL))

	for {
		_, msg, err := conn.Conn.ReadMessage()
		if err != nil {
			return
		}

		var frame api.WireMessage
		if err := proto.Unmarshal(msg, &frame); err != nil {
			logx.Errorf("decode wire message failed user=%s: %v", uid, err)
			metrics.InboundMessages.WithLabelValues("gateway", "decode_error").Inc()
			continue
		}

		switch frame.MsgType {
		case api.MsgType_ACK:
			metrics.InboundMessages.WithLabelValues("gateway", "ack").Inc()
			AckMessage(ctx, rdb, uid, frame.AckMessageId)
			continue
		case api.MsgType_HEARTBEAT:
			metrics.InboundMessages.WithLabelValues("gateway", "heartbeat").Inc()
			if err := RefreshRoute(ctx, rdb, uid, routeValue, routeTTL); err != nil {
				logx.Errorf("refresh route failed user=%s: %v", uid, err)
			}
			_ = conn.Conn.SetReadDeadline(time.Now().Add(routeTTL))
			pong, _ := proto.Marshal(&api.WireMessage{
				MsgType: api.MsgType_HEARTBEAT,
				Body:    "PONG",
				SentAt:  time.Now().UnixMilli(),
			})
			if err := conn.WriteBinary(pong); err != nil {
				return
			}
			continue
		default:
			metrics.InboundMessages.WithLabelValues("gateway", "normal").Inc()
			if ok := pushPool.Submit(uid, logic, msg); !ok {
				logx.Errorf("push queue full user=%s", uid)
				metrics.OutboundMessages.WithLabelValues("logic", "queue_full").Inc()
			}
		}
	}
}
