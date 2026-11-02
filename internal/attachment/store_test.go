package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAllowedAttachmentTypes(t *testing.T) {
	tests := []struct {
		name, detected string
		want           bool
	}{
		{"photo.png", "image/png", true},
		{"movie.mp4", "video/mp4", true},
		{"notes.txt", "text/plain; charset=utf-8", true},
		{"notes.exe", "application/octet-stream", false},
		{"notes.txt", "application/octet-stream", false},
		{"report.docx", "application/octet-stream", false},
		{"photo.png", "text/html", false},
		{"report.pdf", "application/pdf", true},
		{"report.pdf", "text/plain", false},
	}
	for _, tc := range tests {
		t.Run(tc.name+"/"+tc.detected, func(t *testing.T) {
			if got := allowed(tc.name, tc.detected); got != tc.want {
				t.Fatalf("allowed(%q, %q)=%v; want %v", tc.name, tc.detected, got, tc.want)
			}
		})
	}
}

func TestContentDispositionEscapesFilename(t *testing.T) {
	got := ContentDisposition("研究 报告.pdf")
	if got == "" || got == "attachment; filename=研究 报告.pdf" {
		t.Fatalf("filename should be safely encoded, got %q", got)
	}
}

func TestCanonicalizeMessageUsesStoredMetadataAndChecksMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT attachment_id,group_id,uploader_id,file_name,mime_type,file_size,sha256 FROM attachments WHERE attachment_id=? AND status='ready'")).WithArgs("a1").WillReturnRows(
		sqlmock.NewRows([]string{"attachment_id", "group_id", "uploader_id", "file_name", "mime_type", "file_size", "sha256"}).AddRow("a1", "g1", "u1", "actual.png", "image/png", int64(5), "hash"),
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM group_members WHERE group_id=? AND user_id=? AND status='active'")).WithArgs("g1", "u1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	got, err := store.CanonicalizeMessage(context.Background(), "u1", "g1", `{"kind":"attachment","attachment_id":"a1","file_name":"spoof.exe","size":999}`)
	if err != nil {
		t.Fatal(err)
	}
	var body MessageBody
	if err := json.Unmarshal([]byte(got), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "actual.png" || body.MIME != "image/png" || body.Size != 5 {
		t.Fatalf("client metadata was not replaced by stored values: %+v", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalizeMessageRejectsNonMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{DB: db}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT attachment_id,group_id,uploader_id,file_name,mime_type,file_size,sha256 FROM attachments WHERE attachment_id=? AND status='ready'")).WithArgs("a1").WillReturnRows(
		sqlmock.NewRows([]string{"attachment_id", "group_id", "uploader_id", "file_name", "mime_type", "file_size", "sha256"}).AddRow("a1", "g1", "u1", "actual.png", "image/png", int64(5), "hash"),
	)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM group_members WHERE group_id=? AND user_id=? AND status='active'")).WithArgs("g1", "u2").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	_, err = store.CanonicalizeMessage(context.Background(), "u2", "g1", `{"kind":"attachment","attachment_id":"a1"}`)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected membership denial, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
