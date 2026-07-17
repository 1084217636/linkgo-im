package server

import (
	"context"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/ids"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

var pushPool = NewPushWorkerPool(64, 64)

func ShutdownPushWorkerPool(ctx context.Context) error {
	return pushPool.Close(ctx)
}

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
	gatewayID := ParseGatewayID(routeValue)

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
				logx.Errorw("refresh route failed",
					logx.Field("trace_id", frame.TraceId),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", uid),
					logx.Field("error", err.Error()),
				)
			}
			if frame.SessionId != "" {
				SyncSessionMessagesAfterSeq(ctx, rdb, uid, conn, frame.SessionId, frame.LastSeq, nil)
			}
			_ = conn.Conn.SetReadDeadline(time.Now().Add(routeTTL))
			pong, _ := proto.Marshal(&api.WireMessage{
				MsgType: api.MsgType_HEARTBEAT,
				Body:    "PONG",
				SentAt:  time.Now().UnixMilli(),
				TraceId: frame.TraceId,
			})
			if err := conn.WriteBinary(pong); err != nil {
				return
			}
			continue
		default:
			metrics.InboundMessages.WithLabelValues("gateway", "normal").Inc()
			if frame.TraceId == "" {
				frame.TraceId = ids.NewTraceID()
				encoded, err := proto.Marshal(&frame)
				if err != nil {
					logx.Errorf("encode wire message failed user=%s: %v", uid, err)
					continue
				}
				msg = encoded
			}
			logx.Infow("gateway received client message",
				logx.Field("trace_id", frame.TraceId),
				logx.Field("message_id", frame.MessageId),
				logx.Field("client_msg_id", frame.ClientMsgId),
				logx.Field("seq", frame.Seq),
				logx.Field("gateway_id", gatewayID),
				logx.Field("target_id", frame.To),
			)
			logicCtx := ctx
			if result := pushPool.Submit(logicCtx, uid, logic, msg, &frame, gatewayID); result != SubmitAccepted {
				logx.Errorw("push queue rejected",
					logx.Field("trace_id", frame.TraceId),
					logx.Field("message_id", frame.MessageId),
					logx.Field("client_msg_id", frame.ClientMsgId),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", frame.To),
					logx.Field("result", string(result)),
				)
				metrics.OutboundMessages.WithLabelValues("logic", string(result)).Inc()
			}
		}
	}
}
