package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/ids"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

var pushPool = NewPushWorkerPool(64, 64)

// ReplaySessionAuthorizer decides whether uid may read the supplied session
// timeline. Callers must provide an authorizer before accepting a non-empty
// session ID from a client.
type ReplaySessionAuthorizer func(ctx context.Context, uid, sessionID string) error

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
	authorizeReplay ReplaySessionAuthorizer,
) {
	StartClientLoopWithDB(ctx, uid, conn, logic, rdb, nil, routeValue, routeTTL, authorizeReplay)
}

func StartClientLoopWithDB(
	ctx context.Context,
	uid string,
	conn *ClientConn,
	logic api.LogicClient,
	rdb *redis.Client,
	db *sql.DB,
	routeValue string,
	routeTTL time.Duration,
	authorizeReplay ReplaySessionAuthorizer,
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
			AckMessageWithDB(ctx, rdb, db, uid, frame.AckMessageId)
			continue
		case api.MsgType_HEARTBEAT:
			metrics.InboundMessages.WithLabelValues("gateway", "heartbeat").Inc()
			owned, err := RefreshRouteIfMatch(ctx, rdb, uid, routeValue, routeTTL)
			if err != nil {
				logx.Errorw("refresh route failed",
					logx.Field("trace_id", frame.TraceId),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", uid),
					logx.Field("error", err.Error()),
				)
				return
			}
			if !owned {
				logx.Infow("close stale websocket after route ownership changed",
					logx.Field("trace_id", frame.TraceId),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", uid),
					logx.Field("connection_id", conn.ConnectionID),
				)
				return
			}
			if frame.SessionId != "" {
				if err := authorizeSessionReplay(ctx, authorizeReplay, uid, frame.SessionId); err != nil {
					logx.Errorw("heartbeat session replay forbidden",
						logx.Field("trace_id", frame.TraceId),
						logx.Field("gateway_id", gatewayID),
						logx.Field("target_id", uid),
						logx.Field("session_id", frame.SessionId),
						logx.Field("error", err.Error()),
					)
					return
				}
				if err := SyncSessionMessagesAfterSeqWithDB(ctx, rdb, db, uid, conn, frame.SessionId, frame.LastSeq, nil); err != nil {
					return
				}
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
			requestFrame, ok := proto.Clone(&frame).(*api.WireMessage)
			if !ok {
				logx.Errorf("clone wire message failed user=%s", uid)
				continue
			}
			logicCtx := ctx
			onComplete := func(processErr error) {
				if err := writePushResult(conn, requestFrame, processErr); err != nil {
					metrics.OutboundMessages.WithLabelValues("gateway", "result_write_error").Inc()
					logx.Errorw("write push processing result failed",
						logx.Field("trace_id", requestFrame.TraceId),
						logx.Field("client_msg_id", requestFrame.ClientMsgId),
						logx.Field("gateway_id", gatewayID),
						logx.Field("error", err.Error()),
					)
					_ = conn.Close()
					return
				}
				result := "accepted"
				if processErr != nil {
					result = "rejected"
				}
				metrics.OutboundMessages.WithLabelValues("gateway", "message_"+result).Inc()
			}
			if result := pushPool.SubmitWithResult(logicCtx, uid, logic, msg, requestFrame, gatewayID, onComplete); result != SubmitAccepted {
				logx.Errorw("push queue rejected",
					logx.Field("trace_id", frame.TraceId),
					logx.Field("message_id", frame.MessageId),
					logx.Field("client_msg_id", frame.ClientMsgId),
					logx.Field("gateway_id", gatewayID),
					logx.Field("target_id", frame.To),
					logx.Field("result", string(result)),
				)
				metrics.OutboundMessages.WithLabelValues("logic", string(result)).Inc()
				if err := writePushRejection(conn, &frame, result); err != nil {
					metrics.OutboundMessages.WithLabelValues("gateway", "rejection_write_error").Inc()
					logx.Errorw("write push rejection failed",
						logx.Field("trace_id", frame.TraceId),
						logx.Field("client_msg_id", frame.ClientMsgId),
						logx.Field("gateway_id", gatewayID),
						logx.Field("error", err.Error()),
					)
					return
				}
				metrics.OutboundMessages.WithLabelValues("gateway", "rejection_sent").Inc()
			}
		}
	}
}

func authorizeSessionReplay(
	ctx context.Context,
	authorize ReplaySessionAuthorizer,
	uid string,
	sessionID string,
) error {
	if sessionID == "" {
		return nil
	}
	if authorize == nil {
		return errors.New("session replay authorizer is required")
	}
	return authorize(ctx, uid, sessionID)
}
