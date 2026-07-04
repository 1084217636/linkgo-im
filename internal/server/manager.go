package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

type ClientManager struct {
	UserConns sync.Map
}

type ClientConn struct {
	Conn      *websocket.Conn
	SessionID string
	writeMu   sync.Mutex
}

type PushEnvelope struct {
	TargetID   string `json:"target_id"`
	MessageID  string `json:"message_id"`
	SessionID  string `json:"session_id"`
	Seq        int64  `json:"seq"`
	TraceID    string `json:"trace_id"`
	GatewayID  string `json:"gateway_id"`
	RouteValue string `json:"route_value"`
	SentAt     int64  `json:"sent_at"`
	PayloadB64 string `json:"payload_b64"`
}

var Manager = &ClientManager{}

func ChannelForGateway(gatewayID string) string {
	return fmt.Sprintf("im_message_push:%s", gatewayID)
}

func NewClientConn(conn *websocket.Conn, sessionID string) *ClientConn {
	return &ClientConn{
		Conn:      conn,
		SessionID: sessionID,
	}
}

func (c *ClientConn) WriteBinary(payload []byte) error {
	if c == nil || c.Conn == nil {
		return errors.New("websocket connection is nil")
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.Conn.WriteMessage(websocket.BinaryMessage, payload)
}

func (c *ClientConn) Close() error {
	if c == nil || c.Conn == nil {
		return nil
	}
	return c.Conn.Close()
}

func (m *ClientManager) Add(uid string, conn *ClientConn) {
	if previous, loaded := m.UserConns.Swap(uid, conn); loaded {
		if stale, ok := previous.(*ClientConn); ok && stale != conn {
			_ = stale.Close()
		}
	}
}

func (m *ClientManager) Remove(uid string, conn *ClientConn) {
	m.UserConns.CompareAndDelete(uid, conn)
}

func (m *ClientManager) GetConn(uid string) (*ClientConn, bool) {
	val, ok := m.UserConns.Load(uid)
	if !ok {
		return nil, false
	}
	conn, ok := val.(*ClientConn)
	return conn, ok
}

func (m *ClientManager) SubscribeRedis(ctx context.Context, rdb *redis.Client, gatewayID string) {
	pubsub := rdb.Subscribe(ctx, ChannelForGateway(gatewayID))
	defer pubsub.Close()

	for msg := range pubsub.Channel() {
		var envelope PushEnvelope
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			logx.Errorf("invalid pubsub payload: %v", err)
			continue
		}

		conn, ok := m.GetConn(envelope.TargetID)
		if !ok {
			MarkOffline(ctx, rdb, envelope.TargetID, envelope.MessageID, envelope.SentAt)
			if envelope.RouteValue != "" {
				_ = ClearRouteIfMatch(ctx, rdb, envelope.TargetID, envelope.RouteValue)
			}
			metrics.OutboundMessages.WithLabelValues("gateway", "missing_conn").Inc()
			logx.Errorw("gateway push missed local connection",
				logx.Field("trace_id", envelope.TraceID),
				logx.Field("message_id", envelope.MessageID),
				logx.Field("seq", envelope.Seq),
				logx.Field("gateway_id", gatewayID),
				logx.Field("target_id", envelope.TargetID),
			)
			continue
		}

		payload, err := base64.StdEncoding.DecodeString(envelope.PayloadB64)
		if err != nil {
			logx.Errorw("decode pubsub payload failed",
				logx.Field("trace_id", envelope.TraceID),
				logx.Field("message_id", envelope.MessageID),
				logx.Field("seq", envelope.Seq),
				logx.Field("gateway_id", gatewayID),
				logx.Field("error", err.Error()),
			)
			continue
		}

		if err := conn.WriteBinary(payload); err != nil {
			MarkOffline(ctx, rdb, envelope.TargetID, envelope.MessageID, envelope.SentAt)
			if envelope.RouteValue != "" {
				_ = ClearRouteIfMatch(ctx, rdb, envelope.TargetID, envelope.RouteValue)
			}
			_ = conn.Close()
			m.Remove(envelope.TargetID, conn)
			metrics.OutboundMessages.WithLabelValues("gateway", "write_error").Inc()
			logx.Errorw("push websocket failed",
				logx.Field("trace_id", envelope.TraceID),
				logx.Field("message_id", envelope.MessageID),
				logx.Field("seq", envelope.Seq),
				logx.Field("gateway_id", gatewayID),
				logx.Field("target_id", envelope.TargetID),
				logx.Field("error", err.Error()),
			)
			continue
		}
		metrics.OutboundMessages.WithLabelValues("gateway", "success").Inc()
		logx.Infow("gateway pushed websocket message",
			logx.Field("trace_id", envelope.TraceID),
			logx.Field("message_id", envelope.MessageID),
			logx.Field("seq", envelope.Seq),
			logx.Field("gateway_id", gatewayID),
			logx.Field("target_id", envelope.TargetID),
		)
	}
}

func MarkOffline(ctx context.Context, rdb *redis.Client, uid, messageID string, sentAt int64) {
	if rdb == nil || uid == "" || messageID == "" {
		return
	}
	if sentAt <= 0 {
		sentAt = timeNowMillis()
	}
	if err := rdb.ZAdd(ctx, OfflineMessageKey(uid), redis.Z{
		Score:  float64(sentAt),
		Member: messageID,
	}).Err(); err != nil {
		logx.Errorw("save offline message failed",
			logx.Field("message_id", messageID),
			logx.Field("target_id", uid),
			logx.Field("error", err.Error()),
		)
	}
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
