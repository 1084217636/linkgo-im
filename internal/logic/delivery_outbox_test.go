package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestSaveMessageCommitsDeliveryEventAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	frame := persistedTestFrame()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO messages").
		WithArgs(frame.MessageId, frame.ClientMsgId, frame.SessionId, frame.SessionId,
			frame.Seq, frame.From, frame.To, frame.ToType, frame.Body, frame.SentAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO conversation_outbox").
		WithArgs(frame.MessageId, frame.SessionId, frame.From, frame.To, frame.ToType,
			frame.Seq, frame.SentAt, frame.SentAt, frame.SentAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO delivery_outbox").
		WithArgs(frame.MessageId, frame.TraceId, `["1002"]`, frame.SentAt, frame.SentAt, frame.SentAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	h := &LogicHandler{DB: db, ConversationOutbox: true, DeliveryOutbox: true}
	persisted, err := h.saveMessageWithRecipients(context.Background(), frame, []string{"1002"})
	if err != nil {
		t.Fatalf("saveMessageWithRecipients error = %v", err)
	}
	if !persisted {
		t.Fatal("new message was reported as duplicate")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestProcessDeliveryOutboxPublishesSnapshotAndMarksDone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.Now().UnixMilli()
	mock.ExpectQuery("SELECT d.id, d.attempts, d.recipients_json").
		WithArgs("group:G1-7", sqlmock.AnyArg(), 1).
		WillReturnRows(deliveryOutboxRows().AddRow(
			int64(9), 0, `["1002","1003"]`, "trace-group-7",
			"group:G1-7", "client-7", "group:G1", int64(7),
			"1001", "G1", "group", "hello group", now,
		))
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(9), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(9), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	dispatcher := &outboxTestDispatcher{}
	h := &LogicHandler{
		DB:                 db,
		GroupDispatcher:    dispatcher,
		ConversationOutbox: true,
		DeliveryOutbox:     true,
	}
	processed, err := h.ProcessDeliveryOutboxMessage(context.Background(), "group:G1-7")
	if err != nil {
		t.Fatalf("ProcessDeliveryOutboxMessage error = %v", err)
	}
	if !processed || dispatcher.calls != 1 {
		t.Fatalf("processed=%v dispatcher calls=%d", processed, dispatcher.calls)
	}
	if len(dispatcher.recipients) != 2 || dispatcher.recipients[0] != "1002" || dispatcher.recipients[1] != "1003" {
		t.Fatalf("recipients = %#v", dispatcher.recipients)
	}
	if dispatcher.traceID != "trace-group-7" {
		t.Fatalf("dispatched trace_id = %q", dispatcher.traceID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestProcessDeliveryOutboxSchedulesRetryAfterKafkaFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.Now().UnixMilli()
	mock.ExpectQuery("SELECT d.id, d.attempts, d.recipients_json").
		WithArgs(sqlmock.AnyArg(), 10).
		WillReturnRows(deliveryOutboxRows().AddRow(
			int64(10), 0, `["1002"]`, "trace-group-8",
			"group:G1-8", "client-8", "group:G1", int64(8),
			"1001", "G1", "group", "retry me", now,
		))
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(10), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs("pending", sqlmock.AnyArg(), "kafka unavailable", sqlmock.AnyArg(), int64(10), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx, cancel := context.WithCancel(context.Background())
	h := &LogicHandler{
		DB:                 db,
		GroupDispatcher:    &outboxTestDispatcher{beforeReturn: cancel, err: errors.New("kafka unavailable")},
		ConversationOutbox: true,
		DeliveryOutbox:     true,
	}
	processed, err := h.ProcessDeliveryOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("ProcessDeliveryOutbox error = %v", err)
	}
	if processed != 0 {
		t.Fatalf("processed = %d, want 0 while event is scheduled for retry", processed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDeliveryOutboxOldWorkerCannotCompleteNewClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()
	frame := persistedTestFrame()
	frame.ToType = "group"
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(11), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Another worker has since reclaimed the row, advancing attempts to 2.
	mock.ExpectExec("UPDATE delivery_outbox").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(11), 1).
		WillReturnResult(sqlmock.NewResult(0, 0))
	h := &LogicHandler{DB: db, GroupDispatcher: &outboxTestDispatcher{}}
	processed, err := h.processDeliveryOutboxEvent(context.Background(), deliveryOutboxEvent{
		ID: 11, Attempts: 0, Recipients: []string{"1002"}, Frame: frame,
	})
	if processed || err == nil {
		t.Fatalf("processed=%v err=%v; stale worker must not complete", processed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func deliveryOutboxRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "attempts", "recipients_json", "trace_id",
		"message_id", "client_msg_id", "session_id", "seq",
		"from_uid", "to_id", "to_type", "content", "create_time",
	})
}

type outboxTestDispatcher struct {
	calls        int
	recipients   []string
	traceID      string
	beforeReturn func()
	err          error
}

func (d *outboxTestDispatcher) PublishGroupDispatch(_ context.Context, frame *api.WireMessage, recipients []string) error {
	d.calls++
	d.recipients = append([]string(nil), recipients...)
	d.traceID = frame.TraceId
	if d.beforeReturn != nil {
		d.beforeReturn()
	}
	return d.err
}
