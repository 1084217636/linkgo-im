package logic

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	"github.com/DATA-DOG/go-sqlmock"
)

const groupMembershipQuery = `
SELECT status, mute_until
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`

func TestValidateConversationAccessEnforcesGroupSendPermission(t *testing.T) {
	tests := []struct {
		name      string
		rows      *sqlmock.Rows
		wantError string
	}{
		{
			name: "active member without mute can create",
			rows: sqlmock.NewRows([]string{"status", "mute_until"}).
				AddRow("active", int64(0)),
		},
		{
			name: "active member with expired mute can create",
			rows: sqlmock.NewRows([]string{"status", "mute_until"}).
				AddRow("active", time.Now().Add(-time.Hour).UnixMilli()),
		},
		{
			name: "active member still muted cannot create",
			rows: sqlmock.NewRows([]string{"status", "mute_until"}).
				AddRow("active", time.Now().Add(time.Hour).UnixMilli()),
			wantError: "muted",
		},
		{
			name: "left member cannot create",
			rows: sqlmock.NewRows([]string{"status", "mute_until"}).
				AddRow("left", int64(0)),
			wantError: "not an active group member",
		},
		{
			name:      "missing member cannot create",
			rows:      sqlmock.NewRows([]string{"status", "mute_until"}),
			wantError: "not an active group member",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectQuery(regexp.QuoteMeta(groupMembershipQuery)).
				WithArgs("group-1", "user-1").
				WillReturnRows(tt.rows)

			logic := NewRedPacketLogic(context.Background(), &svc.ServiceContext{DB: db})
			err = logic.validateConversationAccess("user-1", "group-1", "group", "group:group-1")
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validateConversationAccess returned error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validateConversationAccess error = %v, want containing %q", err, tt.wantError)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestValidatePacketParticipantAllowsMutedActiveGroupMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(groupMembershipQuery)).
		WithArgs("group-1", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"status", "mute_until"}).
			AddRow("active", time.Now().Add(time.Hour).UnixMilli()))

	logic := NewRedPacketLogic(context.Background(), &svc.ServiceContext{DB: db})
	packet := corelogic.RedPacketInfo{
		ConversationID: "group:group-1",
		ToType:         "group",
	}
	if err := logic.validatePacketParticipant("user-1", packet); err != nil {
		t.Fatalf("muted active member should remain a packet participant: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestValidateActiveGroupMemberPropagatesDatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	wantErr := errors.New("database unavailable")
	mock.ExpectQuery(regexp.QuoteMeta(groupMembershipQuery)).
		WithArgs("group-1", "user-1").
		WillReturnError(wantErr)

	logic := NewRedPacketLogic(context.Background(), &svc.ServiceContext{DB: db})
	err = logic.validateActiveGroupMember("user-1", "group-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("validateActiveGroupMember error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
