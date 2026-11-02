package community

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/segmentio/kafka-go"
)

func TestMutationRetryRejectsChangedPayload(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := &Service{DB: db}
	m.ExpectBegin()
	m.ExpectQuery("SELECT user_id FROM users").WithArgs("u").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("u"))
	m.ExpectQuery("SELECT request_hash,result_json").WithArgs("u", "same-key").WillReturnRows(sqlmock.NewRows([]string{"hash", "result"}).AddRow("hash-of-different-payload", `{"post_id":"old"}`))
	m.ExpectRollback()
	called := false
	_, err := s.mutate(context.Background(), "u", "same-key", "create", Input{Name: "changed"}, func(_ *sql.Tx, _ int64) (any, error) { called = true; return nil, nil })
	if Status(err) != 409 || called {
		t.Fatalf("expected rejected replay without mutation: %v", err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestRoleRejectsFormerMember(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	m.ExpectQuery("SELECT m.role FROM group_members").WithArgs("g", "u").WillReturnRows(sqlmock.NewRows([]string{"role"}))
	_, err := role(context.Background(), db, "g", "u", false)
	if Status(err) != 404 {
		t.Fatal(err)
	}
}
func TestFeedRejectsMalformedCursor(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	m.ExpectQuery("SELECT m.role FROM group_members").WithArgs("g", "u").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("member"))
	_, err := (&Service{DB: db}).Posts(context.Background(), "u", "g", "invalid@@", "", "")
	if Status(err) != 400 {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAnnouncementRequiresManagerAndRollsBack(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	m.ExpectBegin()
	m.ExpectQuery("SELECT user_id FROM users").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("u"))
	m.ExpectQuery("SELECT request_hash,result_json").WillReturnRows(sqlmock.NewRows([]string{"hash", "json"}))
	m.ExpectQuery("SELECT group_id FROM im_groups").WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow("g"))
	m.ExpectQuery("SELECT m.role FROM group_members").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("member"))
	m.ExpectRollback()
	_, err := (&Service{DB: db}).CreatePost(context.Background(), "u", "g", Input{RequestID: "valid-key", Kind: "announcement", Title: "标题", Body: "正文"})
	if Status(err) != 404 {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type failingWriter struct{}

func (failingWriter) WriteMessages(context.Context, ...kafka.Message) error {
	return errors.New("broker unavailable")
}
func TestOutboxFailureRetainsRetryWithLeaseToken(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	w := &Worker{DB: db, Writer: failingWriter{}}
	m.ExpectExec("INSERT IGNORE INTO community_event_outbox").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	m.ExpectBegin()
	m.ExpectQuery("FOR UPDATE SKIP LOCKED").WillReturnRows(sqlmock.NewRows([]string{"event", "group", "actor", "kind", "object", "target", "time", "attempts"}).AddRow("e", "g", "u", "post_created", "p", "", time.Now().UnixMilli(), 0))
	m.ExpectExec("UPDATE community_event_outbox SET status='processing'").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "e").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectExec(regexp.QuoteMeta("UPDATE community_event_outbox SET status=?,available_at=?,last_error=? WHERE event_id=? AND lease_token=? AND status='processing'")).WithArgs("pending", sqlmock.AnyArg(), "broker unavailable", "e", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := w.PublishBatch(context.Background()); err == nil {
		t.Fatal("must report unavailable broker")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestNotificationProjectionRollbackKeepsKafkaUncommitted(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	w := &Worker{DB: db}
	m.ExpectQuery("SELECT event_id,group_id").WillReturnRows(sqlmock.NewRows([]string{"event", "group", "actor", "kind", "object", "target", "at"}).AddRow("e", "g", "u", "post_created", "p", "", time.Now().UnixMilli()))
	m.ExpectBegin()
	m.ExpectExec("INSERT IGNORE INTO community_notifications").WillReturnError(errors.New("db unavailable"))
	m.ExpectRollback()
	if err := w.Project(context.Background(), Event{ID: "e"}); err == nil {
		t.Fatal("projection failure must propagate to consumer before offset commit")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
