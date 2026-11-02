package logic

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/core/logx"
)

type deliveryOutboxEvent struct {
	ID         int64
	Attempts   int
	Recipients []string
	Frame      *api.WireMessage
	DecodeErr  error
}

const (
	deliveryOutboxLease       = 30 * time.Second
	maxDeliveryOutboxAttempts = 10
)

// ProcessDeliveryOutboxMessage performs the low-latency first attempt after
// the message and its event have committed. A failure remains durable and is
// retried by ProcessDeliveryOutbox; callers must not turn it into a rejected
// client message after the transaction has already committed.
func (h *LogicHandler) ProcessDeliveryOutboxMessage(ctx context.Context, messageID string) (bool, error) {
	if h == nil || h.DB == nil || messageID == "" {
		return false, nil
	}
	events, err := h.loadDeliveryOutboxEvents(ctx, `d.message_id = ? AND (d.status = 'pending' OR d.status = 'processing') AND d.available_at <= ?`, 1, messageID, time.Now().UnixMilli())
	if err != nil || len(events) == 0 {
		return false, err
	}
	return h.processDeliveryOutboxEvent(ctx, events[0])
}

// ProcessDeliveryOutbox retries external Redis/Kafka side effects. Multiple
// Logic replicas may run this worker: the conditional lease update lets only
// one replica own a due event at a time, and downstream operations remain
// idempotent for the crash-after-side-effect-before-done window.
func (h *LogicHandler) ProcessDeliveryOutbox(ctx context.Context, limit int) (int, error) {
	if h == nil || h.DB == nil {
		return 0, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	events, err := h.loadDeliveryOutboxEvents(ctx, `(d.status = 'pending' OR d.status = 'processing') AND d.available_at <= ?`, limit, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, event := range events {
		ok, err := h.processDeliveryOutboxEvent(ctx, event)
		if err != nil {
			return processed, err
		}
		if ok {
			processed++
		}
	}
	return processed, nil
}

func (h *LogicHandler) loadDeliveryOutboxEvents(ctx context.Context, condition string, limit int, args ...any) ([]deliveryOutboxEvent, error) {
	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, limit)
	rows, err := h.DB.QueryContext(ctx, `
SELECT d.id, d.attempts, d.recipients_json, d.trace_id,
       m.message_id, m.client_msg_id, m.session_id, m.seq,
       m.from_uid, m.to_id, m.to_type, m.content, m.create_time
FROM delivery_outbox d
JOIN messages m ON m.message_id = d.message_id
WHERE `+condition+`
ORDER BY d.id
LIMIT ?
`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]deliveryOutboxEvent, 0, limit)
	for rows.Next() {
		event := deliveryOutboxEvent{Frame: &api.WireMessage{}}
		var recipientsJSON string
		if err := rows.Scan(
			&event.ID,
			&event.Attempts,
			&recipientsJSON,
			&event.Frame.TraceId,
			&event.Frame.MessageId,
			&event.Frame.ClientMsgId,
			&event.Frame.SessionId,
			&event.Frame.Seq,
			&event.Frame.From,
			&event.Frame.To,
			&event.Frame.ToType,
			&event.Frame.Body,
			&event.Frame.SentAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(recipientsJSON), &event.Recipients); err != nil {
			event.DecodeErr = err
		}
		event.Frame.MsgType = api.MsgType_NORMAL
		events = append(events, event)
	}
	return events, rows.Err()
}

func (h *LogicHandler) processDeliveryOutboxEvent(ctx context.Context, event deliveryOutboxEvent) (bool, error) {
	now := time.Now()
	result, err := h.DB.ExecContext(ctx, `
UPDATE delivery_outbox
SET status = 'processing', attempts = attempts + 1, available_at = ?, updated_at = ?
WHERE id = ? AND (status = 'pending' OR status = 'processing') AND available_at <= ?
`, now.Add(deliveryOutboxLease).UnixMilli(), now.UnixMilli(), event.ID, now.UnixMilli())
	if err != nil {
		return false, err
	}
	claimed, err := result.RowsAffected()
	if err != nil || claimed == 0 {
		return false, err
	}
	// attempts is advanced by the successful claim. Treat that value as a
	// generation: an older worker must not finish or reschedule a newer claim.
	claimGeneration := event.Attempts + 1

	deliveryErr := event.DecodeErr
	if deliveryErr == nil {
		deliveryErr = h.deliverMessageToRecipients(ctx, event.Frame, event.Recipients)
	}
	if deliveryErr != nil {
		status := "pending"
		availableAt := time.Now().Add(deliveryOutboxRetryDelay(event.Attempts + 1)).UnixMilli()
		if event.Attempts+1 >= maxDeliveryOutboxAttempts {
			status = "dead"
			availableAt = time.Now().UnixMilli()
		}
		lastError := deliveryErr.Error()
		if len(lastError) > 512 {
			lastError = lastError[:512]
		}
		// The external call commonly fails because ctx reached its deadline.
		// Persist retry state with a fresh, short database context; otherwise the
		// row would remain processing until the lease expires on every timeout.
		updateCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		updateResult, updateErr := h.DB.ExecContext(updateCtx, `
UPDATE delivery_outbox
SET status = ?, available_at = ?, last_error = ?, updated_at = ?
WHERE id = ? AND status = 'processing' AND attempts = ?
`, status, availableAt, lastError, time.Now().UnixMilli(), event.ID, claimGeneration)
		if updateErr != nil {
			return false, updateErr
		}
		updated, updateErr := updateResult.RowsAffected()
		if updateErr != nil {
			return false, updateErr
		}
		if updated == 0 {
			return false, errors.New("delivery outbox lease was lost before retry update")
		}
		resultLabel := "retry"
		if status == "dead" {
			resultLabel = "dead"
		}
		metrics.DeliveryOutbox.WithLabelValues(resultLabel).Inc()
		logx.WithContext(ctx).Errorw("delivery outbox dispatch failed",
			logx.Field("trace_id", event.Frame.TraceId), logx.Field("message_id", event.Frame.MessageId),
			logx.Field("outbox_id", event.ID), logx.Field("attempt", claimGeneration),
			logx.Field("result", resultLabel), logx.Field("error", lastError))
		return false, nil
	}

	result, err = h.DB.ExecContext(ctx, `
UPDATE delivery_outbox
SET status = 'done', processed_at = ?, updated_at = ?, last_error = ''
WHERE id = ? AND status = 'processing' AND attempts = ?
`, time.Now().UnixMilli(), time.Now().UnixMilli(), event.ID, claimGeneration)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if updated == 0 {
		return false, errors.New("delivery outbox lease was lost before completion")
	}
	metrics.DeliveryOutbox.WithLabelValues("processed").Inc()
	logx.WithContext(ctx).Infow("delivery outbox dispatched",
		logx.Field("trace_id", event.Frame.TraceId), logx.Field("message_id", event.Frame.MessageId),
		logx.Field("outbox_id", event.ID), logx.Field("attempt", claimGeneration))
	return true, nil
}

func deliveryOutboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
