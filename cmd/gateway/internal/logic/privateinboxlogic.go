package logic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/zeromicro/go-zero/core/logx"
)

var ErrPrivateConversationUnavailable = errors.New("private conversation unavailable")
var ErrInvalidReadSequence = errors.New("invalid read sequence")

type PrivateInboxLogic struct {
	DB  *sql.DB
	Svc *svc.ServiceContext
}

// Unread counts persisted incoming messages after each conversation's read
// cursor. ACK is intentionally not used here: delivery does not mean reading.
func (l *PrivateInboxLogic) Unread(ctx context.Context, uid string) (map[string]int64, error) {
	counts := make(map[string]int64)
	if l.DB == nil || uid == "" {
		return counts, ErrPrivateConversationUnavailable
	}
	rows, err := l.DB.QueryContext(ctx, `
SELECT m.from_uid, COUNT(*)
FROM friend_relations fr
JOIN messages m ON m.from_uid = fr.friend_id AND m.to_id = fr.user_id AND m.to_type = 'user'
LEFT JOIN conversation_members cm ON cm.conversation_id = m.session_id AND cm.user_id = fr.user_id
WHERE fr.user_id = ? AND fr.status = 'normal' AND m.seq > COALESCE(cm.read_seq, 0)
GROUP BY m.from_uid
`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var peer string
		var count int64
		if err := rows.Scan(&peer, &count); err != nil {
			return nil, err
		}
		counts[peer] = count
	}
	return counts, rows.Err()
}

// MarkRead advances only this authenticated user's cursor in a normal friend
// conversation. The requested seq is capped at the latest persisted message.
func (l *PrivateInboxLogic) MarkRead(ctx context.Context, uid, peer string, requestedSeq int64) error {
	peer = strings.TrimSpace(peer)
	if l.DB == nil || uid == "" || peer == "" || peer == uid {
		return ErrPrivateConversationUnavailable
	}
	if requestedSeq <= 0 {
		return ErrInvalidReadSequence
	}
	var status string
	if err := l.DB.QueryRowContext(ctx, `SELECT status FROM friend_relations WHERE user_id = ? AND friend_id = ? LIMIT 1`, uid, peer).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPrivateConversationUnavailable
		}
		return err
	}
	if status != "normal" {
		return ErrPrivateConversationUnavailable
	}
	sessionID := buildGatewayConversationID(uid, peer, "user")
	var latestSeq int64
	if err := l.DB.QueryRowContext(ctx, `SELECT seq FROM messages WHERE session_id = ? ORDER BY seq DESC LIMIT 1`, sessionID).Scan(&latestSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPrivateConversationUnavailable
		}
		return err
	}
	if requestedSeq > latestSeq {
		requestedSeq = latestSeq
	}
	_, err := l.DB.ExecContext(ctx, `
INSERT INTO conversation_members (conversation_id, user_id, read_seq, joined_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE read_seq = GREATEST(read_seq, VALUES(read_seq))
`, sessionID, uid, requestedSeq, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	if l.Svc != nil && l.Svc.Rdb != nil {
		if err := server.MarkConversationRead(ctx, l.Svc.Rdb, uid, sessionID, requestedSeq); err != nil {
			logx.Errorw("private read cursor cache update failed", logx.Field("user_id", uid), logx.Field("session_id", sessionID), logx.Field("error", err.Error()))
		}
	}
	return nil
}
