package logic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

func TestLoginUsesGenericCredentialError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		row      *sqlmock.Rows
		err      error
	}{
		{
			name:     "unknown user",
			username: "missing",
			err:      sql.ErrNoRows,
		},
		{
			name:     "wrong password",
			username: "userA",
			row:      sqlmock.NewRows([]string{"user_id", "password", "status"}).AddRow("1001", "123456", 1),
		},
		{
			name:     "disabled user",
			username: "userA",
			row:      sqlmock.NewRows([]string{"user_id", "password", "status"}).AddRow("1001", "123456", 0),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New error = %v", err)
			}
			defer db.Close()

			expectation := mock.ExpectQuery("SELECT user_id, password, status").WithArgs(tc.username)
			if tc.err != nil {
				expectation.WillReturnError(tc.err)
			} else {
				expectation.WillReturnRows(tc.row)
			}

			h := &LogicHandler{DB: db}
			_, err = h.Login(context.Background(), &api.LoginReq{Username: tc.username, Password: "wrong"})
			if err == nil || err.Error() != "invalid credentials" {
				t.Fatalf("Login error = %v, want invalid credentials", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestLoginUpgradesLegacyPlaintextPassword(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT user_id, password, status").
		WithArgs("userA").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "password", "status"}).AddRow("1001", "123456", 1))
	mock.ExpectExec("UPDATE users SET password").
		WithArgs(sqlmock.AnyArg(), "1001", "123456").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT c.id, c.type").
		WithArgs("1001", defaultConversationLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "updated_at", "last_seq", "read_seq", "acked_seq", "content"}))

	h := &LogicHandler{DB: db}
	reply, err := h.Login(context.Background(), &api.LoginReq{Username: "userA", Password: "123456"})
	if err != nil {
		t.Fatalf("Login error = %v", err)
	}
	if reply.UserId != "1001" || strings.TrimSpace(reply.Token) == "" {
		t.Fatalf("Login reply = %#v", reply)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestVerifyPasswordSupportsBcrypt(t *testing.T) {
	const hash = "$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK"
	if valid, legacy := verifyPassword(hash, "123456"); !valid || legacy {
		t.Fatalf("verifyPassword(valid bcrypt) = (%v, %v)", valid, legacy)
	}
	if valid, legacy := verifyPassword(hash, "wrong"); valid || legacy {
		t.Fatalf("verifyPassword(invalid bcrypt) = (%v, %v)", valid, legacy)
	}
}

func TestGetHistoryRejectsInactiveGroupMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status").
		WithArgs("G100", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("removed"))

	h := &LogicHandler{DB: db}
	_, err = h.GetHistory(context.Background(), &api.GetHistoryReq{UserId: "1001", TargetId: "group:G100"})
	if err == nil || !strings.Contains(err.Error(), "active group member") {
		t.Fatalf("GetHistory error = %v, want group membership error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetHistoryAllowsActiveGroupMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status").
		WithArgs("G100", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectQuery("SELECT message_id, client_msg_id").
		WithArgs("group:G100", 51).
		WillReturnRows(sqlmock.NewRows([]string{
			"message_id", "client_msg_id", "session_id", "seq", "from_uid", "to_id", "to_type", "content", "create_time",
		}).AddRow("group:G100-7", "client-7", "group:G100", int64(7), "1002", "G100", "group", "hello", int64(1710100000000)))

	h := &LogicHandler{DB: db}
	reply, err := h.GetHistory(context.Background(), &api.GetHistoryReq{UserId: "1001", TargetId: "group:G100"})
	if err != nil {
		t.Fatalf("GetHistory error = %v", err)
	}
	if len(reply.Messages) != 1 {
		t.Fatalf("GetHistory messages = %#v", reply.Messages)
	}
	message := reply.Messages[0]
	if message.MessageId != "group:G100-7" || message.Body != "hello" || message.ToType != "group" || message.Seq != 7 {
		t.Fatalf("GetHistory message = %#v", message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetHistoryUsesBeforeSeqCursorAndHasMore(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT message_id, client_msg_id").
		WithArgs("c2c:1001:1002", 3).
		WillReturnRows(sqlmock.NewRows([]string{
			"message_id", "client_msg_id", "session_id", "seq", "from_uid", "to_id", "to_type", "content", "create_time",
		}).AddRow("m-5", "c-5", "c2c:1001:1002", int64(5), "1001", "1002", "user", "five", int64(5)).
			AddRow("m-4", "c-4", "c2c:1001:1002", int64(4), "1002", "1001", "user", "four", int64(4)).
			AddRow("m-3", "c-3", "c2c:1001:1002", int64(3), "1001", "1002", "user", "three", int64(3)))

	h := &LogicHandler{DB: db}
	reply, err := h.GetHistory(context.Background(), &api.GetHistoryReq{UserId: "1001", TargetId: "1002", Limit: 2})
	if err != nil {
		t.Fatalf("GetHistory error = %v", err)
	}
	if len(reply.Messages) != 2 || reply.Messages[0].Seq != 4 || reply.Messages[1].Seq != 5 {
		t.Fatalf("messages = %#v, want seq [4 5]", reply.Messages)
	}
	if !reply.HasMore || reply.NextBeforeSeq != 4 {
		t.Fatalf("cursor = has_more:%v next:%d, want true/4", reply.HasMore, reply.NextBeforeSeq)
	}
	mock.ExpectQuery("SELECT message_id, client_msg_id").
		WithArgs("c2c:1001:1002", int64(4), 3).
		WillReturnRows(sqlmock.NewRows([]string{
			"message_id", "client_msg_id", "session_id", "seq", "from_uid", "to_id", "to_type", "content", "create_time",
		}).AddRow("m-3", "c-3", "c2c:1001:1002", int64(3), "1001", "1002", "user", "three", int64(3)).
			AddRow("m-2", "c-2", "c2c:1001:1002", int64(2), "1002", "1001", "user", "two", int64(2)))
	reply, err = h.GetHistory(context.Background(), &api.GetHistoryReq{UserId: "1001", TargetId: "1002", BeforeSeq: reply.NextBeforeSeq, Limit: 2})
	if err != nil || len(reply.Messages) != 2 || reply.Messages[0].Seq != 2 || reply.Messages[1].Seq != 3 || reply.HasMore {
		t.Fatalf("second page = reply:%#v err:%v, want seq [2 3] and no more", reply, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

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
	if !errors.Is(err, ErrClientMessageInFlight) {
		t.Fatalf("reserveClientMessage second error = %v, want ErrClientMessageInFlight", err)
	}
	if duplicate {
		t.Fatal("in-flight reservation was reported as completed duplicate")
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

func TestPendingClientMessageReservationReusesSequence(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()
	h := &LogicHandler{Rdb: rdb}
	first := &api.WireMessage{From: "1001", To: "1002", ToType: "user", ClientMsgId: "client-reserved"}
	duplicate, err := h.reserveClientMessage(ctx, first)
	if err != nil || duplicate {
		t.Fatalf("initial reserve = duplicate:%v err:%v", duplicate, err)
	}
	first.SessionId = "c2c:1001:1002"
	first.Seq = 41
	first.MessageId = "c2c:1001:1002-41"
	h.commitClientMessageReservation(ctx, first)

	retry := &api.WireMessage{From: "1001", To: "1002", ToType: "user", ClientMsgId: "client-reserved"}
	duplicate, err = h.reserveClientMessage(ctx, retry)
	if err != nil || duplicate {
		t.Fatalf("retry reserve = duplicate:%v err:%v", duplicate, err)
	}
	if retry.Seq != 41 || retry.MessageId != first.MessageId || retry.SessionId != first.SessionId {
		t.Fatalf("retry reservation = %#v, want seq/message/session reused", retry)
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

func TestSaveMessageWithOutboxIsAtomic(t *testing.T) {
	ctx := context.Background()
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
	mock.ExpectCommit()

	h := &LogicHandler{DB: db, ConversationOutbox: true}
	persisted, err := h.saveMessage(ctx, frame)
	if err != nil {
		t.Fatalf("saveMessage with outbox error = %v", err)
	}
	if !persisted {
		t.Fatal("saveMessage with outbox reported duplicate for new row")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestProcessConversationOutboxRetriesSummaryEvent(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.Now().UnixMilli()
	mock.ExpectQuery("SELECT id, message_id, session_id, from_uid, to_id, to_type, seq, sent_at").
		WithArgs(sqlmock.AnyArg(), 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "message_id", "session_id", "from_uid", "to_id", "to_type", "seq", "sent_at"}).
			AddRow(int64(7), "group:G1-9", "group:G1", "1001", "G1", "group", int64(9), now))
	mock.ExpectExec("UPDATE conversation_outbox").
		WithArgs(sqlmock.AnyArg(), int64(7), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO conversations").
		WithArgs("group:G1", "group", now, now, int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE conversation_outbox").
		WithArgs(sqlmock.AnyArg(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	h := &LogicHandler{DB: db}
	processed, err := h.ProcessConversationOutbox(ctx, 100)
	if err != nil {
		t.Fatalf("ProcessConversationOutbox error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
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

func TestValidateSendPermissionFailsClosedWithoutRelationshipStore(t *testing.T) {
	h := &LogicHandler{}
	frame := &api.WireMessage{From: "1001", To: "1002", ToType: "user"}
	if err := h.validateSendPermission(context.Background(), frame); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("validateSendPermission error = %v, want unavailable relationship store", err)
	}
}

func TestValidateSendPermissionFailsClosedWhenRelationTableIsMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status").
		WithArgs("1001", "1002").
		WillReturnError(&mysql.MySQLError{Number: 1146, Message: "table friend_relations does not exist"})
	h := &LogicHandler{DB: db}
	frame := &api.WireMessage{From: "1001", To: "1002", ToType: "user"}
	if err := h.validateSendPermission(context.Background(), frame); err == nil {
		t.Fatal("validateSendPermission allowed send with missing relation table")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestPushMessageRedisCompletedDuplicateStillShortCircuits(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()

	frame := &api.WireMessage{
		From:        "1001",
		To:          "1002",
		ToType:      "user",
		Body:        "hello",
		ClientMsgId: "client-complete",
	}
	if err := rdb.Set(ctx, clientMessageKey(frame.From, frame.ClientMsgId), `{"message_id":"existing"}`, clientMessageIDTTL).Err(); err != nil {
		t.Fatalf("seed completed idempotency key: %v", err)
	}

	h := &LogicHandler{Rdb: rdb}
	if _, err := h.PushMessage(ctx, pushMessageRequest(t, frame)); err != nil {
		t.Fatalf("PushMessage completed duplicate error = %v, want short-circuit success", err)
	}
}

func TestPushMessageDBRecoveryRejectsBlockedFriend(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	frame := &api.WireMessage{
		From:        "1001",
		To:          "1002",
		ToType:      "user",
		Body:        "retry body",
		ClientMsgId: "client-blocked",
	}
	existing := &api.WireMessage{
		MessageId:   "c2c:1001:1002-7",
		ClientMsgId: frame.ClientMsgId,
		SessionId:   "c2c:1001:1002",
		Seq:         7,
		From:        frame.From,
		To:          frame.To,
		ToType:      frame.ToType,
		Body:        "persisted body",
		SentAt:      1710100000000,
	}
	expectLoadMessageByClientMsgID(mock, existing)
	mock.ExpectQuery("SELECT status").
		WithArgs(existing.From, existing.To).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("blocked"))

	h := &LogicHandler{Rdb: rdb, DB: db, Delivery: &delivery.RedisDelivery{Rdb: rdb}}
	_, err = h.PushMessage(ctx, pushMessageRequest(t, frame))
	if err == nil || !strings.Contains(err.Error(), "normal friend") {
		t.Fatalf("PushMessage error = %v, want blocked-friend rejection", err)
	}
	assertClientMessageReservationReleased(t, ctx, rdb, frame.From, frame.ClientMsgId)
	assertNoPendingDelivery(t, ctx, rdb, existing.To)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPushMessageDBRecoveryRejectsInactiveGroupSender(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		muteUntil int64
	}{
		{name: "removed member", status: "removed"},
		{name: "muted member", status: "active", muteUntil: time.Now().Add(time.Hour).UnixMilli()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			rdb, cleanup := newTestRedis(t)
			defer cleanup.Close()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New error = %v", err)
			}
			defer db.Close()

			frame := &api.WireMessage{
				From:        "1001",
				To:          "G1",
				ToType:      "group",
				Body:        "retry group body",
				ClientMsgId: "client-group-recovery",
			}
			existing := &api.WireMessage{
				MessageId:   "group:G1-9",
				ClientMsgId: frame.ClientMsgId,
				SessionId:   "group:G1",
				Seq:         9,
				From:        frame.From,
				To:          frame.To,
				ToType:      frame.ToType,
				Body:        "persisted group body",
				SentAt:      1710100000000,
			}
			expectLoadMessageByClientMsgID(mock, existing)
			mock.ExpectQuery("SELECT status, mute_until").
				WithArgs(existing.To, existing.From).
				WillReturnRows(sqlmock.NewRows([]string{"status", "mute_until"}).AddRow(tc.status, tc.muteUntil))

			dispatcher := &recordingGroupDispatcher{}
			h := &LogicHandler{Rdb: rdb, DB: db, GroupDispatcher: dispatcher}
			_, err = h.PushMessage(ctx, pushMessageRequest(t, frame))
			if err == nil || !strings.Contains(err.Error(), "active group member") {
				t.Fatalf("PushMessage error = %v, want inactive-group-sender rejection", err)
			}
			if dispatcher.calls != 0 {
				t.Fatalf("group dispatcher calls = %d, want 0", dispatcher.calls)
			}
			assertClientMessageReservationReleased(t, ctx, rdb, frame.From, frame.ClientMsgId)
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestPushMessageUniqueRaceReauthorizesLoadedMessage(t *testing.T) {
	ctx := context.Background()
	rdb, cleanup := newTestRedis(t)
	defer cleanup.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	frame := &api.WireMessage{
		From:        "1001",
		To:          "1002",
		ToType:      "user",
		Body:        "racing body",
		ClientMsgId: "client-race",
	}
	mock.ExpectQuery("SELECT message_id, client_msg_id, conversation_id, session_id, seq").
		WithArgs(frame.From, frame.ClientMsgId).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT status").
		WithArgs(frame.From, frame.To).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("normal"))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\)").
		WithArgs("c2c:1001:1002").
		WillReturnRows(sqlmock.NewRows([]string{"max_seq"}).AddRow(0))
	dupErr := &mysql.MySQLError{
		Number:  1062,
		Message: "Duplicate entry '1001-client-race' for key 'uk_sender_client_msg'",
	}
	mock.ExpectExec("INSERT INTO messages").
		WithArgs(
			"c2c:1001:1002-1",
			frame.ClientMsgId,
			"c2c:1001:1002",
			"c2c:1001:1002",
			int64(1),
			frame.From,
			frame.To,
			frame.ToType,
			frame.Body,
			sqlmock.AnyArg(),
		).
		WillReturnError(dupErr)
	existing := &api.WireMessage{
		MessageId:   "c2c:1001:1002-8",
		ClientMsgId: frame.ClientMsgId,
		SessionId:   "c2c:1001:1002",
		Seq:         8,
		From:        frame.From,
		To:          frame.To,
		ToType:      frame.ToType,
		Body:        "persisted winner",
		SentAt:      1710100000000,
	}
	expectLoadMessageByClientMsgID(mock, existing)
	mock.ExpectQuery("SELECT status").
		WithArgs(existing.From, existing.To).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("blocked"))

	h := &LogicHandler{Rdb: rdb, DB: db, Delivery: &delivery.RedisDelivery{Rdb: rdb}}
	_, err = h.PushMessage(ctx, pushMessageRequest(t, frame))
	if err == nil || !strings.Contains(err.Error(), "normal friend") {
		t.Fatalf("PushMessage error = %v, want post-race permission rejection", err)
	}
	assertClientMessageReservationReleased(t, ctx, rdb, frame.From, frame.ClientMsgId)
	assertNoPendingDelivery(t, ctx, rdb, existing.To)
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

func TestResolveGroupRecipientsFailsClosedWithoutDatabase(t *testing.T) {
	h := &LogicHandler{}
	frame := &api.WireMessage{From: "1001", To: "G1", ToType: "group"}
	if _, err := h.resolveRecipients(context.Background(), frame); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("resolveRecipients error = %v, want unavailable group membership store", err)
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

type recordingGroupDispatcher struct {
	calls int
}

func (d *recordingGroupDispatcher) PublishGroupDispatch(context.Context, *api.WireMessage, []string) error {
	d.calls++
	return nil
}

func pushMessageRequest(t *testing.T, frame *api.WireMessage) *api.PushMsgReq {
	t.Helper()
	payload, err := proto.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal push frame: %v", err)
	}
	return &api.PushMsgReq{UserId: frame.From, Content: payload}
}

func expectLoadMessageByClientMsgID(mock sqlmock.Sqlmock, frame *api.WireMessage) {
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
		))
}

func assertClientMessageReservationReleased(t *testing.T, ctx context.Context, rdb *redis.Client, uid, clientMsgID string) {
	t.Helper()
	if _, err := rdb.Get(ctx, clientMessageKey(uid, clientMsgID)).Result(); err != redis.Nil {
		t.Fatalf("client message reservation error = %v, want redis.Nil", err)
	}
}

func assertNoPendingDelivery(t *testing.T, ctx context.Context, rdb *redis.Client, uid string) {
	t.Helper()
	count, err := rdb.ZCard(ctx, "pending_ack:"+uid).Result()
	if err != nil {
		t.Fatalf("read pending delivery: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending deliveries = %d, want 0", count)
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
