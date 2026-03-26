package logic

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

var sessionSeqScript = redis.NewScript(`
local nextSeq = redis.call("INCR", KEYS[1])
redis.call("PEXPIRE", KEYS[1], ARGV[1])
return nextSeq
`)

type GroupDispatcher interface {
	PublishGroupDispatch(ctx context.Context, frame *api.WireMessage, recipients []string) error
}

type LogicHandler struct {
	api.UnimplementedLogicServer

	Rdb             *redis.Client
	DB              *sql.DB
	Delivery        *delivery.RedisDelivery
	GroupDispatcher GroupDispatcher
}

func (h *LogicHandler) PushMessage(ctx context.Context, req *api.PushMsgReq) (*api.PushMsgReply, error) {
	var frame api.WireMessage
	if err := proto.Unmarshal(req.Content, &frame); err != nil {
		metrics.InboundMessages.WithLabelValues("logic", "decode_error").Inc()
		return nil, fmt.Errorf("invalid protobuf payload: %w", err)
	}
	if frame.MsgType == api.MsgType_HEARTBEAT || frame.MsgType == api.MsgType_ACK {
		metrics.InboundMessages.WithLabelValues("logic", "control").Inc()
		return &api.PushMsgReply{}, nil
	}
	metrics.InboundMessages.WithLabelValues("logic", "normal").Inc()

	if err := normalizeFrame(req.UserId, &frame); err != nil {
		return nil, err
	}

	frame.SessionId = buildSessionID(frame.From, frame.To, frame.ToType)
	seq, err := h.nextSequence(ctx, frame.SessionId)
	if err != nil {
		return nil, err
	}
	frame.Seq = seq
	frame.SentAt = time.Now().UnixMilli()
	frame.MessageId = fmt.Sprintf("%s-%d", frame.SessionId, frame.Seq)

	recipients, err := h.resolveRecipients(ctx, &frame)
	if err != nil {
		return nil, err
	}

	if frame.ToType == "group" && h.GroupDispatcher != nil {
		if err := h.GroupDispatcher.PublishGroupDispatch(ctx, &frame, recipients); err != nil {
			return nil, err
		}
	} else {
		if err := h.dispatchToRecipients(ctx, &frame, recipients); err != nil {
			return nil, err
		}
	}

	go h.saveMessage(&frame)
	return &api.PushMsgReply{}, nil
}

func (h *LogicHandler) GetHistory(ctx context.Context, req *api.GetHistoryReq) (*api.GetHistoryReply, error) {
	sessionID := buildSessionID(req.UserId, req.TargetId, targetType(req.TargetId))
	rows, err := h.DB.QueryContext(ctx, `
SELECT message_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
LIMIT 50
`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*api.WireMessage, 0, 50)
	for rows.Next() {
		var msg api.WireMessage
		var body string
		if err := rows.Scan(
			&msg.MessageId,
			&msg.SessionId,
			&msg.Seq,
			&msg.From,
			&msg.To,
			&msg.ToType,
			&body,
			&msg.SentAt,
		); err != nil {
			return nil, err
		}
		msg.Body = body
		msg.MsgType = api.MsgType_NORMAL
		res = append(res, &msg)
	}

	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return &api.GetHistoryReply{Messages: res}, nil
}

func (h *LogicHandler) Login(ctx context.Context, req *api.LoginReq) (*api.LoginReply, error) {
	var uid string
	var pwdInDB string

	query := "SELECT user_id, password FROM users WHERE username = ? LIMIT 1"
	if err := h.DB.QueryRowContext(ctx, query, req.Username).Scan(&uid, &pwdInDB); err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if pwdInDB != req.Password {
		return nil, fmt.Errorf("invalid password")
	}

	token, err := middleware.GenerateToken(uid)
	if err != nil {
		return nil, err
	}

	return &api.LoginReply{
		Token:  token,
		UserId: uid,
	}, nil
}

func normalizeFrame(sender string, frame *api.WireMessage) error {
	if frame.From == "" {
		frame.From = sender
	}
	if frame.From != sender {
		return fmt.Errorf("sender mismatch")
	}
	if frame.To == "" || frame.Body == "" {
		return fmt.Errorf("to and body are required")
	}
	if frame.ToType == "" {
		frame.ToType = targetType(frame.To)
	}
	if frame.ToType != "user" && frame.ToType != "group" {
		return fmt.Errorf("unsupported to_type")
	}
	frame.MsgType = api.MsgType_NORMAL
	return nil
}

func (h *LogicHandler) resolveRecipients(ctx context.Context, frame *api.WireMessage) ([]string, error) {
	if frame.ToType == "user" {
		return []string{frame.To}, nil
	}

	members, err := h.Rdb.SMembers(ctx, "group_members:"+frame.To).Result()
	if err != nil {
		return nil, err
	}

	recipients := make([]string, 0, len(members))
	for _, member := range members {
		if member == "" || member == frame.From {
			continue
		}
		recipients = append(recipients, member)
	}
	return recipients, nil
}

func (h *LogicHandler) dispatchToRecipients(ctx context.Context, frame *api.WireMessage, recipients []string) error {
	payload, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := h.Delivery.Deliver(ctx, recipient, frame.MessageId, payload, frame.SentAt); err != nil {
			return err
		}
	}
	return nil
}

func (h *LogicHandler) nextSequence(ctx context.Context, sessionID string) (int64, error) {
	return sessionSeqScript.Run(ctx, h.Rdb, []string{"seq:" + sessionID}, 7*24*time.Hour.Milliseconds()).Int64()
}

func (h *LogicHandler) saveMessage(frame *api.WireMessage) {
	if h.DB == nil {
		return
	}

	query := `
INSERT INTO messages (message_id, session_id, seq, from_uid, to_id, to_type, content, create_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`
	if _, err := h.DB.Exec(
		query,
		frame.MessageId,
		frame.SessionId,
		frame.Seq,
		frame.From,
		frame.To,
		frame.ToType,
		frame.Body,
		frame.SentAt,
	); err != nil {
		log.Printf("persist message failed, session=%s seq=%d: %v", frame.SessionId, frame.Seq, err)
	}
}

func buildSessionID(from, to, toType string) string {
	if toType == "group" {
		return "group:" + to
	}
	ids := []string{from, to}
	sort.Strings(ids)
	return "c2c:" + strings.Join(ids, ":")
}

func targetType(target string) string {
	if strings.HasPrefix(strings.ToUpper(target), "G") || strings.HasPrefix(target, "group:") {
		return "group"
	}
	return "user"
}
