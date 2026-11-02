package logic

import (
	"context"
	"github.com/1084217636/linkgo-im/api"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestCommunityHistoryTargetNames(t *testing.T) {
	for _, target := range []string{"G100", "group:G100", "c-9d248a43-c295-4534-80b7-1ad6dfa03ce0", "group:c-9d248a43-c295-4534-80b7-1ad6dfa03ce0"} {
		if targetType(target) != "group" {
			t.Fatalf("community history target %q was treated as a user", target)
		}
	}
	if targetType("9d248a43-c295-4534-80b7-1ad6dfa03ce0") != "user" {
		t.Fatal("registered user UUID must remain a user target")
	}
}

func TestDepartedMemberCannotReadStaleConversationSummary(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	h := &LogicHandler{DB: db}
	m.ExpectQuery("SELECT m.group_id FROM group_members").WithArgs("u").WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow("active"))
	got, err := h.filterCommunityConversations(context.Background(), "u", []*api.Conversation{{ConversationId: "group:left", LastMsg: "must not leak"}, {ConversationId: "group:active"}, {ConversationId: "c2c:u:v"}})
	if err != nil || len(got) != 2 || got[0].ConversationId != "group:active" {
		t.Fatalf("unauthorized cached group summary: %+v %v", got, err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
