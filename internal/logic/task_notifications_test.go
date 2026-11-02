package logic

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/redis/go-redis/v9"
)

func TestTaskNoticeVerbDoesNotExposeReviewReason(t *testing.T) {
	for _, tc := range []struct {
		result string
		want   string
	}{
		{`{"status":"approved","review_note":"private note"}`, "审核通过"},
		{`{"status":"returned","review_note":"private note"}`, "审核退回"},
	} {
		got, err := taskNoticeVerb("review", []byte(tc.result))
		if err != nil || got != tc.want {
			t.Fatalf("result=%s got=%s err=%v", tc.result, got, err)
		}
	}
	if _, err := taskNoticeVerb("review", []byte(`{"status":"unknown"}`)); err == nil {
		t.Fatal("unknown review state accepted")
	}
}

func TestTaskNotificationRejectsOccupiedSystemUID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()
	mock.ExpectQuery("SELECT username,status FROM users").WithArgs(taskSystemUserID).
		WillReturnRows(sqlmock.NewRows([]string{"username", "status"}).AddRow("real_user", 1))
	h := &LogicHandler{DB: db, Rdb: rdb, ConversationOutbox: true, DeliveryOutbox: true}
	_, err = h.publishTaskEventMessage(context.Background(), taskNotice{ID: 1})
	if err == nil || !strings.Contains(err.Error(), "not reserved and disabled") {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
