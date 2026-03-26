package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
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
			log.Printf("invalid pubsub payload: %v", err)
			continue
		}

		conn, ok := m.GetConn(envelope.TargetID)
		if !ok {
			continue
		}

		payload, err := base64.StdEncoding.DecodeString(envelope.PayloadB64)
		if err != nil {
			log.Printf("decode pubsub payload failed: %v", err)
			continue
		}

		if err := conn.WriteBinary(payload); err != nil {
			log.Printf("push websocket failed for user=%s: %v", envelope.TargetID, err)
			m.Remove(envelope.TargetID, conn)
		}
	}
}
