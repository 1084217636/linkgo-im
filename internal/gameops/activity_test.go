package gameops

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func validActivityConfig() ActivityConfig {
	return ActivityConfig{
		Title:          "Summer Login",
		StartAt:        1_720_000_000_000,
		EndAt:          1_720_086_400_000,
		RewardItemID:   "gem",
		RewardQuantity: 100,
	}
}

func TestValidateActivityDraftRejectsInvalidWindowAndRollout(t *testing.T) {
	config := validActivityConfig()
	config.EndAt = config.StartAt
	if err := ValidateActivityDraft("summer", config, 10); !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf("window validation error = %v", err)
	}
	config = validActivityConfig()
	if err := ValidateActivityDraft("summer", config, 101); !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf("rollout validation error = %v", err)
	}
}

func TestCreateDraftAllocatesVersionUnderActivityRowLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO game_activities").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT current_version FROM game_activities").WithArgs("summer").WillReturnRows(sqlmock.NewRows([]string{"current_version"}).AddRow(3))
	mock.ExpectExec("UPDATE game_activities").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO game_activity_versions").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO operation_audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	service := NewActivityService(db, nil)
	version, err := service.CreateDraft(context.Background(), Actor{UserID: "1001", Role: "operator"}, "summer", validActivityConfig(), 20, "req-1", "trace-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if version.Version != 4 {
		t.Fatalf("version = %d, want 4", version.Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishActivityWritesStateAuditOutboxAndCache(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, created_by, approved_by, config_json, rollout_percent").
		WithArgs("summer", 1).
		WillReturnRows(sqlmock.NewRows([]string{"status", "created_by", "approved_by", "config_json", "rollout_percent"}).AddRow("approved", "1001", "1002", `{"title":"Summer Login","start_at":1720000000000,"end_at":1720086400000,"reward_item_id":"gem","reward_quantity":100}`, 20))
	mock.ExpectExec("UPDATE game_activity_versions").WithArgs(sqlmock.AnyArg(), "summer", 1).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE game_activity_versions").WithArgs(sqlmock.AnyArg(), "summer", 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE game_activities").WithArgs(1, 20, sqlmock.AnyArg(), "summer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO operation_audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO gameops_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec("UPDATE gameops_outbox").WillReturnResult(sqlmock.NewResult(0, 1))

	service := NewActivityService(db, rdb)
	version, err := service.Publish(context.Background(), Actor{UserID: "1003", Role: "admin"}, "summer", 1, "req-1", "trace-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if version.Status != ActivityPublished || version.ApprovedBy != "1002" {
		t.Fatalf("published version = %#v", version)
	}
	if !redisServer.Exists(ActivityCacheKey("summer")) {
		t.Fatal("published activity cache missing")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApproveActivityRejectsSelfApproval(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT created_by FROM game_activity_versions").
		WithArgs("summer", 1).
		WillReturnRows(sqlmock.NewRows([]string{"created_by"}).AddRow("1001"))
	mock.ExpectRollback()

	service := NewActivityService(db, nil)
	err = service.Approve(context.Background(), Actor{UserID: "1001", Role: "reviewer"}, "summer", 1, "req-1", "trace-1", "127.0.0.1")
	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("Approve() error = %v, want ErrSelfApproval", err)
	}
}

func TestApproveActivityRecordsReviewer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT created_by FROM game_activity_versions").WithArgs("summer", 1).
		WillReturnRows(sqlmock.NewRows([]string{"created_by"}).AddRow("1001"))
	mock.ExpectExec("UPDATE game_activity_versions").WithArgs("1002", sqlmock.AnyArg(), "summer", 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE game_activities").WithArgs(sqlmock.AnyArg(), "summer", 1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO operation_audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	service := NewActivityService(db, nil)
	if err := service.Approve(context.Background(), Actor{UserID: "1002", Role: "reviewer"}, "summer", 1, "req-1", "trace-1", "127.0.0.1"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
