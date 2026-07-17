package gameops

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGrantItemsUpdatesInventoryOnceAndAudits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status FROM game_item_grant_requests").WithArgs("grant-1").WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()
	mock.ExpectExec("INSERT INTO game_item_grant_failures").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO operation_audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewGrantService(db)
	_, err = service.GrantItems(context.Background(), Actor{UserID: "1001", Role: "operator"}, GrantRequest{
		GrantRequestID: "grant-1",
		Items:          []GrantItem{{UserID: "player-1", ItemID: "gem", Quantity: 100}},
	}, "trace-1", "127.0.0.1")
	if err == nil {
		t.Fatal("expected non-ErrNoRows query failure")
	}
}

func TestGrantItemsSuccessfulTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status FROM game_item_grant_requests").WithArgs("grant-1").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO game_item_grant_requests").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO game_item_grants").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO player_items").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE game_item_grant_requests").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO operation_audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	service := NewGrantService(db)
	result, err := service.GrantItems(context.Background(), Actor{UserID: "1001", Role: "operator"}, GrantRequest{
		GrantRequestID: "grant-1",
		Items:          []GrantItem{{UserID: "player-1", ItemID: "gem", Quantity: 100}},
	}, "trace-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("GrantItems() error = %v", err)
	}
	if result.AlreadyProcessed || len(result.Items) != 1 || result.Items[0].Quantity != 100 {
		t.Fatalf("grant result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGrantRequestRejectsDuplicateTuple(t *testing.T) {
	err := ValidateGrantRequest(GrantRequest{
		GrantRequestID: "grant-1",
		Items: []GrantItem{
			{UserID: "player-1", ItemID: "gem", Quantity: 100},
			{UserID: "player-1", ItemID: "gem", Quantity: 100},
		},
	})
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("ValidateGrantRequest() error = %v", err)
	}
}

func TestGrantItemsReturnsExistingResultWithoutIncrementingInventory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status FROM game_item_grant_requests").WithArgs("grant-1").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("success"))
	mock.ExpectQuery("SELECT user_id, item_id, quantity FROM game_item_grants").WithArgs("grant-1").WillReturnRows(sqlmock.NewRows([]string{"user_id", "item_id", "quantity"}).AddRow("player-1", "gem", 100))
	mock.ExpectCommit()

	service := NewGrantService(db)
	result, err := service.GrantItems(context.Background(), Actor{UserID: "1001", Role: "operator"}, GrantRequest{
		GrantRequestID: "grant-1",
		Items:          []GrantItem{{UserID: "player-1", ItemID: "gem", Quantity: 100}},
	}, "trace-2", "127.0.0.1")
	if err != nil {
		t.Fatalf("GrantItems() error = %v", err)
	}
	if !result.AlreadyProcessed || result.Status != "success" || len(result.Items) != 1 {
		t.Fatalf("idempotent result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
