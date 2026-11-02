package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
)

const Topic = "community.events.v1"

var EventOperations = promauto.NewCounterVec(prometheus.CounterOpts{Name: "linkgo_community_events_total", Help: "Community event delivery and projection outcomes."}, []string{"operation", "result"})
var EventDelay = promauto.NewHistogram(prometheus.HistogramOpts{Name: "linkgo_community_notification_delay_seconds", Help: "Time from committed community event to inbox projection.", Buckets: prometheus.DefBuckets})
var OutboxPending = promauto.NewGauge(prometheus.GaugeOpts{Name: "linkgo_community_outbox_pending", Help: "Pending or leased community events."})

type EventWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
}
type Worker struct {
	DB     *sql.DB
	Rdb    *redis.Client
	Writer EventWriter
}

// Task events already have a transactional outbox. Project the same stable
// event/target into the community transport; never invent a second task action.
func (w *Worker) projectTasks(ctx context.Context) error {
	_, err := w.DB.ExecContext(ctx, `INSERT IGNORE INTO community_event_outbox(event_id,group_id,actor_id,kind,object_id,recipient_id,available_at,created_at)
 SELECT CONCAT('task-',SHA2(CONCAT(o.event_id,':',o.target_type,':',o.target_id),256)),t.group_id,e.actor_uid,CONCAT('task_',e.action),t.task_id,IF(o.target_type='user',o.target_id,''),e.created_at,e.created_at
 FROM task_notification_outbox o JOIN task_events e ON e.event_id=o.event_id JOIN group_tasks t ON t.task_id=e.task_id
 LEFT JOIN community_event_outbox x ON x.event_id=CONCAT('task-',SHA2(CONCAT(o.event_id,':',o.target_type,':',o.target_id),256))
 WHERE x.event_id IS NULL ORDER BY o.id LIMIT 100`)
	return err
}
func (w *Worker) PublishBatch(ctx context.Context) error {
	if err := w.projectTasks(ctx); err != nil {
		return err
	}
	var count int
	if err := w.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM community_event_outbox WHERE status IN ('pending','processing')`).Scan(&count); err == nil {
		OutboxPending.Set(float64(count))
	}
	for i := 0; i < 10; i++ {
		tx, err := w.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		var e Event
		var attempts int
		err = tx.QueryRowContext(ctx, `SELECT event_id,group_id,actor_id,kind,object_id,recipient_id,created_at,attempts FROM community_event_outbox WHERE status IN ('pending','processing') AND available_at<=? ORDER BY created_at,event_id LIMIT 1 FOR UPDATE SKIP LOCKED`, time.Now().UnixMilli()).Scan(&e.ID, &e.GroupID, &e.Actor, &e.Kind, &e.ObjectID, &e.Recipient, &e.CreatedAt, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		token := uuid.NewString()
		_, err = tx.ExecContext(ctx, `UPDATE community_event_outbox SET status='processing',attempts=attempts+1,lease_token=?,available_at=? WHERE event_id=?`, token, time.Now().Add(30*time.Second).UnixMilli(), e.ID)
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		data, _ := json.Marshal(e)
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		publishErr := w.Writer.WriteMessages(callCtx, kafka.Message{Key: []byte(e.GroupID), Value: data})
		cancel()
		status := "done"
		reason := ""
		available := time.Now().UnixMilli()
		result := "success"
		if publishErr != nil {
			status = "pending"
			result = "retry"
			reason = publishErr.Error()
			if len(reason) > 512 {
				reason = reason[:512]
			}
			attempts++
			if attempts >= 10 {
				status = "dead"
				result = "dead"
			}
			delay := attempts
			if delay > 6 {
				delay = 6
			}
			available = time.Now().Add(time.Duration(1<<delay) * time.Second).UnixMilli()
		}
		_, err = w.DB.ExecContext(ctx, `UPDATE community_event_outbox SET status=?,available_at=?,last_error=? WHERE event_id=? AND lease_token=? AND status='processing'`, status, available, reason, e.ID, token)
		EventOperations.WithLabelValues("publish", result).Inc()
		logx.Infow("community event publish", logx.Field("event_id", e.ID), logx.Field("group_id", e.GroupID), logx.Field("result", result))
		if err != nil {
			return err
		}
		if publishErr != nil {
			return publishErr
		}
	}
	return nil
}
func (w *Worker) Project(ctx context.Context, e Event) error {
	// Re-read trusted fields: Kafka data is not allowed to choose arbitrary users.
	var actual Event
	err := w.DB.QueryRowContext(ctx, `SELECT event_id,group_id,actor_id,kind,object_id,recipient_id,created_at FROM community_event_outbox WHERE event_id=?`, e.ID).Scan(&actual.ID, &actual.GroupID, &actual.Actor, &actual.Kind, &actual.ObjectID, &actual.Recipient, &actual.CreatedAt)
	if err != nil {
		return err
	}
	e = actual
	tx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The unique event/recipient key survives redelivery and commit failures.
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO community_notifications(event_id,recipient_id,group_id,kind,object_id,created_at)
 SELECT ?,m.user_id,?,?,?,? FROM group_members m JOIN im_groups g ON g.group_id=m.group_id
 WHERE m.group_id=? AND m.status='active' AND g.status='active' AND m.user_id<>? AND (?='' OR m.user_id=?)`, e.ID, e.GroupID, e.Kind, e.ObjectID, e.CreatedAt, e.GroupID, e.Actor, e.Recipient, e.Recipient)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	EventOperations.WithLabelValues("project", "success").Inc()
	EventDelay.Observe(time.Since(time.UnixMilli(e.CreatedAt)).Seconds())
	if w.Rdb != nil {
		w.hint(ctx, e)
	}
	return nil
}
func (w *Worker) hint(ctx context.Context, e Event) {
	rows, err := w.DB.QueryContext(ctx, `SELECT n.recipient_id FROM community_notifications n JOIN group_members m ON m.group_id=n.group_id AND m.user_id=n.recipient_id AND m.status='active' WHERE n.event_id=?`, e.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if rows.Scan(&uid) != nil {
			continue
		}
		route, err := w.Rdb.Get(ctx, server.RouteKey(uid)).Result()
		if err != nil {
			continue
		}
		// Only an invalidation hint, without post/comment/answer text. Inbox is durable.
		value, _ := json.Marshal(server.PushEnvelope{Kind: "community_notice", TargetID: uid, GatewayID: server.ParseGatewayID(route), RouteValue: route})
		_ = w.Rdb.Publish(ctx, server.ChannelForGateway(server.ParseGatewayID(route)), value).Err()
	}
}
func RunPublisher(ctx context.Context, w *Worker) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 12*time.Second)
		err := w.PublishBatch(work)
		cancel()
		if err != nil && ctx.Err() == nil {
			logx.Errorw("community outbox retry", logx.Field("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func RunConsumer(ctx context.Context, w *Worker, brokers []string) {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: Topic, GroupID: "community-notifications-v1", MinBytes: 1, MaxBytes: 1 << 20, CommitInterval: 0, StartOffset: kafka.FirstOffset})
	defer reader.Close()
	dlq := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: "community.events.dlq.v1", RequiredAcks: kafka.RequireAll, AllowAutoTopicCreation: true, WriteTimeout: 5 * time.Second}
	defer dlq.Close()
	for ctx.Err() == nil {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			pause(ctx)
			continue
		}
		for ctx.Err() == nil {
			var e Event
			decodeErr := json.Unmarshal(msg.Value, &e)
			if decodeErr != nil || e.ID == "" {
				work, cancel := context.WithTimeout(ctx, 6*time.Second)
				err = dlq.WriteMessages(work, kafka.Message{Key: msg.Key, Value: msg.Value, Headers: []kafka.Header{{Key: "source-offset", Value: []byte(strconv.FormatInt(msg.Offset, 10))}}})
				cancel()
			} else {
				work, cancel := context.WithTimeout(ctx, 8*time.Second)
				err = w.Project(work, e)
				cancel()
			}
			if err == nil {
				err = reader.CommitMessages(ctx, msg)
			}
			if err == nil {
				break
			}
			EventOperations.WithLabelValues("project", "retry").Inc()
			logx.Errorw("community inbox retry", logx.Field("error", err.Error()))
			pause(ctx)
		}
	}
}
func pause(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
