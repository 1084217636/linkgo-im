package logic

import (
	"context"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func TestReserveClientMessageUsesShortPendingTTL(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()

	h := &LogicHandler{Rdb: rdb}
	frame := &api.WireMessage{From: "1001", ClientMsgId: "client-1"}

	duplicate, err := h.reserveClientMessage(ctx, frame)
	if err != nil {
		t.Fatalf("reserveClientMessage first error = %v", err)
	}
	if duplicate {
		t.Fatal("first reservation reported duplicate")
	}

	duplicate, err = h.reserveClientMessage(ctx, frame)
	if err != nil {
		t.Fatalf("reserveClientMessage second error = %v", err)
	}
	if !duplicate {
		t.Fatal("second reservation did not report duplicate")
	}

	cleanup.fastForward(clientMessagePendingTTL + time.Second)
	duplicate, err = h.reserveClientMessage(ctx, frame)
	if err != nil {
		t.Fatalf("reserveClientMessage after pending ttl error = %v", err)
	}
	if duplicate {
		t.Fatal("reservation stayed blocked after pending ttl")
	}
}

func TestNextSequenceInitializesFromDBMaxSeq(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\)").
		WithArgs("c2c:1001:1002").
		WillReturnRows(sqlmock.NewRows([]string{"max_seq"}).AddRow(41))

	h := &LogicHandler{Rdb: rdb, DB: db}
	seq, err := h.nextSequence(ctx, "c2c:1001:1002")
	if err != nil {
		t.Fatalf("nextSequence error = %v", err)
	}
	if seq != 42 {
		t.Fatalf("nextSequence = %d, want 42", seq)
	}

	seq, err = h.nextSequence(ctx, "c2c:1001:1002")
	if err != nil {
		t.Fatalf("nextSequence second error = %v", err)
	}
	if seq != 43 {
		t.Fatalf("nextSequence second = %d, want 43", seq)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSaveMessagePersistsClientMsgID(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	frame := persistedTestFrame()
	mock.ExpectExec("INSERT INTO messages").
		WithArgs(
			frame.MessageId,
			frame.ClientMsgId,
			frame.SessionId,
			frame.SessionId,
			frame.Seq,
			frame.From,
			frame.To,
			frame.ToType,
			frame.Body,
			frame.SentAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := &LogicHandler{DB: db}
	persisted, err := h.saveMessage(ctx, frame)
	if err != nil {
		t.Fatalf("saveMessage error = %v", err)
	}
	if !persisted {
		t.Fatal("saveMessage reported duplicate for new row")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSaveMessageDuplicateClientMsgIDLoadsExistingMessage(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	frame := persistedTestFrame()
	dupErr := &mysql.MySQLError{
		Number:  1062,
		Message: "Duplicate entry '1001-client-1' for key 'uk_sender_client_msg'",
	}
	mock.ExpectExec("INSERT INTO messages").
		WithArgs(
			frame.MessageId,
			frame.ClientMsgId,
			frame.SessionId,
			frame.SessionId,
			frame.Seq,
			frame.From,
			frame.To,
			frame.ToType,
			frame.Body,
			frame.SentAt,
		).
		WillReturnError(dupErr)
	mock.ExpectQuery("SELECT message_id, client_msg_id, conversation_id, session_id, seq").
		WithArgs(frame.From, frame.ClientMsgId).
		WillReturnRows(sqlmock.NewRows([]string{
			"message_id",
			"client_msg_id",
			"conversation_id",
			"session_id",
			"seq",
			"from_uid",
			"to_id",
			"to_type",
			"content",
			"create_time",
		}).AddRow(
			"c2c:1001:1002-7",
			frame.ClientMsgId,
			frame.SessionId,
			frame.SessionId,
			int64(7),
			frame.From,
			frame.To,
			frame.ToType,
			"stored body",
			int64(1710100000000),
		))

	h := &LogicHandler{DB: db}
	persisted, err := h.saveMessage(ctx, frame)
	if err != nil {
		t.Fatalf("saveMessage duplicate error = %v", err)
	}
	if persisted {
		t.Fatal("saveMessage duplicate reported a new row")
	}
	if frame.MessageId != "c2c:1001:1002-7" || frame.Seq != 7 || frame.Body != "stored body" {
		t.Fatalf("frame was not replaced with existing message: %#v", frame)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestValidateSendPermissionRequiresNormalFriend(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	h := &LogicHandler{DB: db}
	frame := &api.WireMessage{From: "1001", To: "1002", ToType: "user"}
	mock.ExpectQuery("SELECT status").
		WithArgs("1001", "1002").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("normal"))

	if err := h.validateSendPermission(ctx, frame); err != nil {
		t.Fatalf("validateSendPermission normal friend error = %v", err)
	}

	blocked := &api.WireMessage{From: "1001", To: "1003", ToType: "user"}
	mock.ExpectQuery("SELECT status").
		WithArgs("1001", "1003").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("blocked"))

	if err := h.validateSendPermission(ctx, blocked); err == nil {
		t.Fatal("validateSendPermission allowed blocked friend")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestLoadGroupRecipientsFromDBSkipsSender(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT user_id").
		WithArgs("G1", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("1002").AddRow("1003"))

	h := &LogicHandler{DB: db}
	recipients, err := h.loadGroupRecipientsFromDB(ctx, "G1", "1001")
	if err != nil {
		t.Fatalf("loadGroupRecipientsFromDB error = %v", err)
	}
	if len(recipients) != 2 || recipients[0] != "1002" || recipients[1] != "1003" {
		t.Fatalf("recipients = %#v, want [1002 1003]", recipients)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func persistedTestFrame() *api.WireMessage {
	return &api.WireMessage{
		MessageId:   "c2c:1001:1002-1",
		ClientMsgId: "client-1",
		SessionId:   "c2c:1001:1002",
		Seq:         1,
		From:        "1001",
		To:          "1002",
		ToType:      "user",
		Body:        "hello",
		SentAt:      1710100000000,
		MsgType:     api.MsgType_NORMAL,
	}
}

type testRedisCleanup struct {
	close       func()
	fastForward func(time.Duration)
}

func (c testRedisCleanup) Close() {
	c.close()
}

func newTestRedis(t *testing.T) (*redis.Client, testRedisCleanup) {
	t.Helper()

	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run error = %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	return rdb, testRedisCleanup{
		close: func() {
			_ = rdb.Close()
			srv.Close()
		},
		fastForward: srv.FastForward,
	}
}
