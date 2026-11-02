package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
)

const taskSystemUserID = "9002"
const taskNotificationLease = 30 * time.Second
const taskNotificationAttempts = 10

var errTaskNotificationSkipped = errors.New("task notification target is no longer active")

type taskNotice struct {
	ID         int64
	EventID    string
	TargetType string
	TargetID   string
	Attempts   int
}

// ProcessTaskNotifications bridges committed task events into the existing
// durable IM message pipeline. A task transaction never depends on Kafka or Redis.
func (h *LogicHandler) ProcessTaskNotifications(ctx context.Context, limit int) (int, error) {
	if h == nil || h.DB == nil {
		return 0, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := h.DB.QueryContext(ctx, `
SELECT id,event_id,target_type,target_id,attempts
FROM task_notification_outbox
WHERE status IN ('pending','processing') AND available_at<=?
ORDER BY id LIMIT ?`, time.Now().UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	var notices []taskNotice
	for rows.Next() {
		var n taskNotice
		if err := rows.Scan(&n.ID, &n.EventID, &n.TargetType, &n.TargetID, &n.Attempts); err != nil {
			rows.Close()
			return 0, err
		}
		notices = append(notices, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, n := range notices {
		ok, err := h.processTaskNotification(ctx, n)
		if err != nil {
			return processed, err
		}
		if ok {
			processed++
		}
	}
	return processed, nil
}

func (h *LogicHandler) processTaskNotification(ctx context.Context, n taskNotice) (bool, error) {
	now := time.Now().UnixMilli()
	claimed, err := h.DB.ExecContext(ctx, `UPDATE task_notification_outbox SET status='processing',attempts=attempts+1,available_at=?,updated_at=? WHERE id=? AND status IN ('pending','processing') AND available_at<=?`, now+taskNotificationLease.Milliseconds(), now, n.ID, now)
	if err != nil {
		return false, err
	}
	count, err := claimed.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	generation := n.Attempts + 1
	messageID, publishErr := h.publishTaskEventMessage(ctx, n)
	status := "done"
	nextAt := time.Now().UnixMilli()
	lastError := ""
	if errors.Is(publishErr, errTaskNotificationSkipped) {
		status = "skipped"
		lastError = publishErr.Error()
	}
	if publishErr != nil && status != "skipped" {
		status = "pending"
		if generation >= taskNotificationAttempts {
			status = "dead"
		} else {
			nextAt = time.Now().Add(taskNotificationDelay(generation)).UnixMilli()
		}
		lastError = publishErr.Error()
	}
	if len(lastError) > 512 {
		lastError = lastError[:512]
	}
	var mid any
	if messageID != "" {
		mid = messageID
	}
	// A timed-out external call must not prevent persistence of retry state.
	updateCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := h.DB.ExecContext(updateCtx, `UPDATE task_notification_outbox SET status=?,available_at=?,im_message_id=?,last_error=?,updated_at=? WHERE id=? AND status='processing' AND attempts=?`, status, nextAt, mid, lastError, time.Now().UnixMilli(), n.ID, generation)
	if err != nil {
		return false, err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, errors.New("task notification lease was lost")
	}
	return status == "done", nil
}

func taskNotificationDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

// publishTaskEventMessage accepts only an event and target already persisted by
// the task domain service. It is intentionally not exposed as a client RPC.
func (h *LogicHandler) publishTaskEventMessage(ctx context.Context, n taskNotice) (string, error) {
	if h.Rdb == nil || !h.ConversationOutbox || !h.DeliveryOutbox {
		return "", errors.New("durable IM message pipeline is unavailable")
	}
	var systemName string
	var systemStatus int
	if err := h.DB.QueryRowContext(ctx, `SELECT username,status FROM users WHERE user_id=?`, taskSystemUserID).Scan(&systemName, &systemStatus); err != nil {
		return "", fmt.Errorf("task notifier account is unavailable: %w", err)
	}
	if systemName != "task_notifier" || systemStatus != 0 {
		return "", errors.New("task notifier account is not reserved and disabled")
	}
	var taskID, groupID, title, action string
	var resultJSON []byte
	err := h.DB.QueryRowContext(ctx, `
SELECT e.task_id,t.group_id,t.title,e.action,e.result_json
FROM task_events e JOIN group_tasks t ON t.task_id=e.task_id
JOIN task_notification_outbox o ON o.event_id=e.event_id
WHERE o.id=? AND o.target_type=? AND o.target_id=?`, n.ID, n.TargetType, n.TargetID).Scan(&taskID, &groupID, &title, &action, &resultJSON)
	if err != nil {
		return "", err
	}
	if n.TargetType != "group" && n.TargetType != "user" {
		return "", errors.New("invalid notification target type")
	}
	if n.TargetType == "group" && n.TargetID != groupID {
		return "", errors.New("notification group mismatch")
	}
	var groupStatus string
	if err := h.DB.QueryRowContext(ctx, `SELECT status FROM im_groups WHERE group_id=?`, groupID).Scan(&groupStatus); err != nil {
		return "", err
	}
	if groupStatus != "active" {
		return "", errTaskNotificationSkipped
	}
	if n.TargetType == "user" {
		allowed, err := h.isCurrentGroupMember(ctx, groupID, n.TargetID)
		if err != nil {
			return "", err
		}
		if !allowed {
			return "", errTaskNotificationSkipped
		}
	}
	clientID := fmt.Sprintf("task_event:%s:%s", n.EventID, n.TargetID)
	if existing, ok, err := h.loadMessageByClientMsgID(ctx, taskSystemUserID, clientID); err != nil {
		return "", err
	} else if ok {
		return existing.MessageId, nil
	}
	verb, err := taskNoticeVerb(action, resultJSON)
	if err != nil {
		return "", err
	}
	// These notices contain no answers or private reviewer notes. The UI must
	// fetch the authoritative task state after following task://<id>.
	body := fmt.Sprintf("[学习任务] %s：%s task://%s", verb, strings.TrimSpace(title), taskID)
	frame := &api.WireMessage{From: taskSystemUserID, To: n.TargetID, ToType: n.TargetType, Body: body, ClientMsgId: clientID, MsgType: api.MsgType_NORMAL, SentAt: time.Now().UnixMilli()}
	frame.SessionId = buildSessionID(frame.From, frame.To, frame.ToType)
	seq, err := h.nextSequence(ctx, frame.SessionId)
	if err != nil {
		return "", err
	}
	frame.Seq = seq
	frame.MessageId = fmt.Sprintf("%s-%d", frame.SessionId, seq)
	var recipients []string
	if n.TargetType == "group" {
		recipients, err = h.loadGroupRecipientsFromDB(ctx, n.TargetID, taskSystemUserID)
		if err != nil {
			return "", err
		}
	} else {
		recipients = []string{n.TargetID}
	}
	_, handled, err := h.saveMessageWithOutbox(ctx, frame, recipients)
	if err != nil {
		return "", err
	}
	if !handled {
		return "", errors.New("required IM outbox schema is missing")
	}
	if _, err := h.ProcessDeliveryOutboxMessage(ctx, frame.MessageId); err != nil {
		// The message and its delivery job are already durable. Task handoff is
		// complete even when this immediate external attempt fails.
		_ = err
	}
	return frame.MessageId, nil
}

func taskNoticeVerb(action string, resultJSON []byte) (string, error) {
	switch action {
	case "publish":
		return "已发布", nil
	case "submit":
		return "有新提交", nil
	case "close":
		return "已关闭", nil
	case "review":
		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return "", err
		}
		switch result.Status {
		case "approved":
			return "审核通过", nil
		case "returned":
			return "审核退回", nil
		}
	}
	return "", errors.New("unsupported task notification action or review status")
}
