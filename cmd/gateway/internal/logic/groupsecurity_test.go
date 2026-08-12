package logic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	authutil "github.com/1084217636/linkgo-im/internal/middleware"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func TestGroupCreateRejectsExistingGroupInsteadOfTakingItOver(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO im_groups").
		WithArgs("group-1", "existing group", "1002", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "duplicate entry"})
	mock.ExpectRollback()

	logic := NewGroupCreateLogic(authenticatedContext(t, "1002"), &svc.ServiceContext{DB: db})
	_, err = logic.Create(&types.GroupCreateReq{GroupID: "group-1", Name: "existing group", Members: []string{"1003"}})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Create error = %v, want existing-group rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGroupMembersRejectsNonMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT status
FROM group_members
WHERE group_id = ? AND user_id = ?
LIMIT 1
`)).WithArgs("group-1", "outsider").
		WillReturnRows(sqlmock.NewRows([]string{"status"}))

	logic := NewGroupMembersLogic(authenticatedContext(t, "outsider"), &svc.ServiceContext{DB: db})
	_, err = logic.List(&types.GroupMembersReq{GroupID: "group-1"})
	if err == nil || !strings.Contains(err.Error(), "not an active group member") {
		t.Fatalf("List error = %v, want membership rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGroupMembersAllowsActiveMember(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT status").WithArgs("group-1", "1001").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	mock.ExpectQuery("SELECT user_id, role, status, mute_until, joined_at").WithArgs("group-1").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "role", "status", "mute_until", "joined_at"}).
			AddRow("1001", "owner", "active", int64(0), int64(1)).
			AddRow("1002", "member", "active", int64(0), int64(2)))

	logic := NewGroupMembersLogic(authenticatedContext(t, "1001"), &svc.ServiceContext{DB: db})
	resp, err := logic.List(&types.GroupMembersReq{GroupID: "group-1"})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(resp.Members) != 2 || resp.Members[1].UserID != "1002" {
		t.Fatalf("List response = %#v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestGroupCreateReturnsSuccessWhenCacheFailsAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()
	redisServer.SetError("ERR cache unavailable")

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO im_groups").
		WithArgs("group-1", "group one", "1001", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO group_members").
		WithArgs("group-1", "1001", "owner", sqlmock.AnyArg(), "1001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO conversation_members").
		WithArgs("group:group-1", "1001", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	logic := NewGroupCreateLogic(authenticatedContext(t, "1001"), &svc.ServiceContext{DB: db, Rdb: rdb})
	resp, err := logic.Create(&types.GroupCreateReq{
		GroupID: "group-1",
		Name:    "group one",
		Members: []string{"1001"},
	})
	if err != nil {
		t.Fatalf("Create error = %v, want success after committed DB transaction", err)
	}
	if resp == nil || resp.GroupID != "group-1" || resp.Members != 1 {
		t.Fatalf("Create response = %#v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func authenticatedContext(t *testing.T, userID string) context.Context {
	t.Helper()
	token, err := authutil.GenerateToken(userID)
	if err != nil {
		t.Fatalf("GenerateToken error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	var result context.Context
	gwmiddleware.NewAuthMiddleware().Handle(func(_ http.ResponseWriter, request *http.Request) {
		result = request.Context()
	})(recorder, req)
	if result == nil {
		t.Fatalf("authentication failed with status %d", recorder.Code)
	}
	return result
}
