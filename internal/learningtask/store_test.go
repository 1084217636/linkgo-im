package learningtask

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestInputValidation(t *testing.T) {
	if validateTaskFields("", "", 0, time.Now().UnixMilli()) == nil {
		t.Fatal("empty title accepted")
	}
	if validateTaskFields("作业", "说明", time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()) != nil {
		t.Fatal("valid task rejected")
	}
	for _, input := range []struct{ text, link string }{{"", ""}, {"ok", "javascript:alert(1)"}, {"ok", "https://name:pass@example.com"}} {
		if validateSubmission(input.text, input.link, nil) == nil {
			t.Fatalf("bad submission accepted: %+v", input)
		}
	}
	if validateSubmission("学习笔记", "https://example.org/note", nil) != nil {
		t.Fatal("valid submission rejected")
	}
	if validateSubmission("", "", []string{"00112233445566778899aabbccddeeff"}) != nil {
		t.Fatal("valid attachment-only submission rejected")
	}
	if validateSubmission("", "", []string{"not-a-valid-attachment-id"}) == nil {
		t.Fatal("invalid attachment ID accepted")
	}
}

func TestCreateRejectsNonMemberBeforeWriting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT gm.role FROM group_members").WithArgs("G1", "outsider").WillReturnRows(sqlmock.NewRows([]string{"role"}))
	mock.ExpectRollback()
	_, err = NewService(db).Create(context.Background(), "outsider", "G1", "request-123", "作业", "说明", 0)
	if StatusCode(err) != http.StatusNotFound {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRejectsStaleVersionAndRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT task_id,group_id,title").WithArgs("lt_1").WillReturnRows(taskRows("lt_1", "G1", "draft", 2))
	mock.ExpectQuery("SELECT gm.role FROM group_members").WithArgs("G1", "owner").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))
	mock.ExpectQuery("SELECT request_hash,result_json FROM task_events").WithArgs("owner", "request-123").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, err = NewService(db).Publish(context.Background(), "owner", "lt_1", "request-123", 1)
	if StatusCode(err) != http.StatusConflict {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitRejectsManagerEvenWithValidBody(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT task_id,group_id,title").WithArgs("lt_1").WillReturnRows(taskRows("lt_1", "G1", "published", 2))
	mock.ExpectQuery("SELECT gm.role FROM group_members").WithArgs("G1", "owner").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))
	mock.ExpectQuery("SELECT request_hash,result_json FROM task_events").WithArgs("owner", "request-123").WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, err = NewService(db).Submit(context.Background(), "owner", "lt_1", "request-123", "完成", "", 0)
	if StatusCode(err) != http.StatusNotFound {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReviewRejectsAlreadyReviewed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT task_id FROM task_submissions").WithArgs("ts_1").WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow("lt_1"))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT task_id,group_id,title").WithArgs("lt_1").WillReturnRows(taskRows("lt_1", "G1", "published", 2))
	mock.ExpectQuery("SELECT gm.role FROM group_members").WithArgs("G1", "owner").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))
	mock.ExpectQuery("SELECT request_hash,result_json FROM task_events").WithArgs("owner", "request-123").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT submission_id,task_id,user_id").WithArgs("ts_1", "lt_1").WillReturnRows(sqlmock.NewRows([]string{"submission_id", "task_id", "user_id", "content_text", "content_link", "attachment_ids", "status", "attempt_no", "version", "reviewer_id", "review_note", "submitted_at", "reviewed_at"}).AddRow("ts_1", "lt_1", "member", "text", "", []byte("[]"), "approved", 1, 2, "owner", "", int64(1), int64(2)))
	mock.ExpectRollback()
	_, err = NewService(db).Review(context.Background(), "owner", "ts_1", "request-123", "approve", "", 1)
	if StatusCode(err) != http.StatusConflict {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEventUniqueRequestConflictIsNotStoreOutage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO task_events").WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = saveEvent(context.Background(), tx, "te_1", "lt_1", "", "owner", "create", "same-request", "hash", struct{}{}, struct{}{}, nil, 1)
	if StatusCode(err) != http.StatusConflict {
		t.Fatalf("err=%v", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSubmissionCursorPagesWithoutOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for page := 0; page < 2; page++ {
		mock.ExpectQuery("SELECT task_id,group_id,title").WithArgs("lt_1").WillReturnRows(taskRows("lt_1", "G1", "published", 2))
		mock.ExpectQuery("SELECT gm.role FROM group_members").WithArgs("G1", "owner").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))
		if page == 0 {
			mock.ExpectQuery("SELECT submission_id,task_id,user_id").WithArgs("lt_1", 2).WillReturnRows(submissionRows().AddRow("ts_2", "lt_1", "member2", "new", "", []byte("[]"), "submitted", 1, 1, "", "", int64(200), int64(0)).AddRow("ts_1", "lt_1", "member1", "old", "", []byte("[]"), "submitted", 1, 1, "", "", int64(100), int64(0)))
		} else {
			mock.ExpectQuery("SELECT submission_id,task_id,user_id").WithArgs("lt_1", int64(200), int64(200), "ts_2", 2).WillReturnRows(submissionRows().AddRow("ts_1", "lt_1", "member1", "old", "", []byte("[]"), "submitted", 1, 1, "", "", int64(100), int64(0)))
		}
	}
	first, err := NewService(db).ListSubmissions(context.Background(), "owner", "lt_1", "", 1)
	if err != nil || len(first.Submissions) != 1 || first.Submissions[0].ID != "ts_2" || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil || string(decoded) != "200:ts_2" {
		t.Fatalf("cursor=%q err=%v", decoded, err)
	}
	second, err := NewService(db).ListSubmissions(context.Background(), "owner", "lt_1", first.NextCursor, 1)
	if err != nil || len(second.Submissions) != 1 || second.Submissions[0].ID != "ts_1" || second.NextCursor != "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func submissionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"submission_id", "task_id", "user_id", "content_text", "content_link", "attachment_ids", "status", "attempt_no", "version", "reviewer_id", "review_note", "submitted_at", "reviewed_at"})
}

func taskRows(id, group, status string, version int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"task_id", "group_id", "title", "description", "due_at", "status", "version", "created_by", "published_at", "closed_at", "created_at", "updated_at"}).AddRow(id, group, "作业", "说明", int64(0), status, version, "owner", int64(0), int64(0), int64(1), int64(1))
}
