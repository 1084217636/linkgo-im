package logic

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
)

func (h *LogicHandler) GetHistory(ctx context.Context, req *api.GetHistoryReq) (*api.GetHistoryReply, error) {
	toType := targetType(req.TargetId)
	targetID := req.TargetId
	if toType == "group" {
		targetID = normalizeGroupID(req.TargetId)
		allowed, err := h.isCurrentGroupMember(ctx, targetID, req.UserId)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("user is not an active group member")
		}
	}
	sessionID := buildSessionID(req.UserId, targetID, toType)
	limit := int(req.Limit)
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	queryLimit := limit + 1
	var rows *sql.Rows
	var err error
	if req.BeforeSeq > 0 {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, client_msg_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ? AND seq < ?
ORDER BY seq DESC
LIMIT ?
`, sessionID, req.BeforeSeq, queryLimit)
	} else {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, client_msg_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
	LIMIT ?
`, sessionID, queryLimit)
	}
	if isUnknownColumn(err, "client_msg_id") {
		metrics.HistoryQueries.WithLabelValues("mysql_legacy", "success").Inc()
		return h.getHistoryLegacy(ctx, sessionID, req.BeforeSeq, limit)
	}
	if err != nil {
		metrics.HistoryQueries.WithLabelValues("mysql", "error").Inc()
		return nil, err
	}
	defer rows.Close()

	res := make([]*api.WireMessage, 0, queryLimit)
	for rows.Next() {
		var msg api.WireMessage
		var body string
		if err := rows.Scan(
			&msg.MessageId,
			&msg.ClientMsgId,
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
	hasMore := len(res) > limit
	if hasMore {
		res = res[:limit]
	}

	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	reply := &api.GetHistoryReply{Messages: res, HasMore: hasMore}
	if len(res) > 0 && hasMore {
		reply.NextBeforeSeq = res[0].Seq
	}
	metrics.HistoryQueries.WithLabelValues("mysql", "success").Inc()
	return reply, nil
}

func (h *LogicHandler) getHistoryLegacy(ctx context.Context, sessionID string, beforeSeq int64, limit int) (*api.GetHistoryReply, error) {
	queryLimit := limit + 1
	var rows *sql.Rows
	var err error
	if beforeSeq > 0 {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ? AND seq < ?
ORDER BY seq DESC
	LIMIT ?
`, sessionID, beforeSeq, queryLimit)
	} else {
		rows, err = h.DB.QueryContext(ctx, `
SELECT message_id, session_id, seq, from_uid, to_id, to_type, content, create_time
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
LIMIT ?
`, sessionID, queryLimit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*api.WireMessage, 0, queryLimit)
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
	hasMore := len(res) > limit
	if hasMore {
		res = res[:limit]
	}

	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	reply := &api.GetHistoryReply{Messages: res, HasMore: hasMore}
	if len(res) > 0 && hasMore {
		reply.NextBeforeSeq = res[0].Seq
	}
	return reply, nil
}
