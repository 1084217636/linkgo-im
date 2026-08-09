package server

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type failingReplayWriter struct {
	calls int
	err   error
}

func (w *failingReplayWriter) WriteBinary([]byte) error {
	w.calls++
	return w.err
}

func TestPendingReplayStopsAfterFirstWriteFailure(t *testing.T) {
	rdb, cleanup := replayTestRedis(t)
	defer cleanup()
	ctx := context.Background()
	encoded := base64.StdEncoding.EncodeToString([]byte("payload"))
	if err := rdb.ZAdd(ctx, PendingAckKey("1001"), redis.Z{Score: 1, Member: "m1"}, redis.Z{Score: 2, Member: "m2"}).Err(); err != nil {
		t.Fatalf("ZAdd error = %v", err)
	}
	if err := rdb.HSet(ctx, AckIndexKey("1001"), "m1", encoded, "m2", encoded).Err(); err != nil {
		t.Fatalf("HSet error = %v", err)
	}

	want := errors.New("slow websocket")
	writer := &failingReplayWriter{err: want}
	err := SyncOfflineMessages(ctx, rdb, "1001", writer, "", -1)
	if !errors.Is(err, want) {
		t.Fatalf("SyncOfflineMessages error = %v, want %v", err, want)
	}
	if writer.calls != 1 {
		t.Fatalf("write calls = %d, want 1", writer.calls)
	}
}

func TestTimelineReplayStopsAfterFirstWriteFailure(t *testing.T) {
	rdb, cleanup := replayTestRedis(t)
	defer cleanup()
	ctx := context.Background()
	encoded := base64.StdEncoding.EncodeToString([]byte("payload"))
	if err := rdb.ZAdd(ctx, SessionTimelineKey("c2c:1001:1002"), redis.Z{Score: 1, Member: "m1"}, redis.Z{Score: 2, Member: "m2"}).Err(); err != nil {
		t.Fatalf("ZAdd error = %v", err)
	}
	if err := rdb.MSet(ctx, MessagePayloadKey("m1"), encoded, MessagePayloadKey("m2"), encoded).Err(); err != nil {
		t.Fatalf("MSet error = %v", err)
	}

	want := errors.New("closed websocket")
	writer := &failingReplayWriter{err: want}
	err := SyncSessionMessagesAfterSeq(ctx, rdb, "1001", writer, "c2c:1001:1002", 0, nil)
	if !errors.Is(err, want) {
		t.Fatalf("SyncSessionMessagesAfterSeq error = %v, want %v", err, want)
	}
	if writer.calls != 1 {
		t.Fatalf("write calls = %d, want 1", writer.calls)
	}
}

type collectingReplayWriter struct{ payloads [][]byte }

func (w *collectingReplayWriter) WriteBinary(payload []byte) error {
	w.payloads = append(w.payloads, append([]byte(nil), payload...))
	return nil
}

func TestReconnectFallsBackToMySQLWhenRedisTimelineIsEmpty(t *testing.T) {
	rdb, cleanup := replayTestRedis(t)
	defer cleanup()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT message_id, client_msg_id, session_id, seq").
		WithArgs("c2c:1001:1002", int64(10), 200).
		WillReturnRows(sqlmock.NewRows([]string{
			"message_id", "client_msg_id", "session_id", "seq", "from_uid", "to_id", "to_type", "content", "create_time",
		}).AddRow("m-11", "c-11", "c2c:1001:1002", int64(11), "1002", "1001", "user", "one", int64(1)).
			AddRow("m-13", "c-13", "c2c:1001:1002", int64(13), "1001", "1002", "user", "two", int64(2)))

	writer := &collectingReplayWriter{}
	if err := SyncOfflineMessagesWithDB(context.Background(), rdb, db, "1001", writer, "c2c:1001:1002", 10); err != nil {
		t.Fatalf("SyncOfflineMessagesWithDB error = %v", err)
	}
	if len(writer.payloads) != 2 {
		t.Fatalf("fallback payload count = %d, want 2", len(writer.payloads))
	}
	for i, payload := range writer.payloads {
		msg := DecodeWireMessage(payload)
		if msg == nil || msg.Seq != []int64{11, 13}[i] {
			t.Fatalf("fallback message %d = %#v", i, msg)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func replayTestRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run error = %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	return client, func() {
		_ = client.Close()
		server.Close()
	}
}
