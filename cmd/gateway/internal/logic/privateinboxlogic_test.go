package logic

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPrivateUnreadCountsIncomingMessages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT m.from_uid, COUNT").WithArgs("u1").
		WillReturnRows(sqlmock.NewRows([]string{"from_uid", "count"}).AddRow("u2", int64(2)).AddRow("u3", int64(1)))
	counts, err := (&PrivateInboxLogic{DB: db}).Unread(context.Background(), "u1")
	if err != nil || counts["u2"] != 2 || counts["u3"] != 1 {
		t.Fatalf("Unread = %v, %v", counts, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateReadRequiresFriendship(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT status FROM friend_relations").WithArgs("u1", "u2").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("blocked"))
	err = (&PrivateInboxLogic{DB: db}).MarkRead(context.Background(), "u1", "u2", 7)
	if !errors.Is(err, ErrPrivateConversationUnavailable) {
		t.Fatalf("MarkRead error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateReadCapsRequestedSequenceAtPersistedMessage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT status FROM friend_relations").WithArgs("u1", "u2").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("normal"))
	mock.ExpectQuery("SELECT seq FROM messages").WithArgs("c2c:u1:u2").
		WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(int64(7)))
	mock.ExpectExec("INSERT INTO conversation_members.*").WithArgs("c2c:u1:u2", "u1", int64(7), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	err = (&PrivateInboxLogic{DB: db}).MarkRead(context.Background(), "u1", "u2", 999)
	if err != nil {
		t.Fatalf("MarkRead error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
