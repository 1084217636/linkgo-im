package logic

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWalletMockRechargeCreditsBalanceAndPersistsLedger(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.UnixMilli(1710100000000)
	svc := &WalletService{
		DB:  db,
		Now: func() time.Time { return now },
		NewID: func(prefix string) string {
			return prefix + "-test"
		},
	}
	mock.ExpectQuery("SELECT id, request_id, channel, amount, provider_trade_no").
		WithArgs("1001", "recharge-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "request_id", "channel", "amount", "provider_trade_no"}))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO mock_payment_orders").
		WithArgs("pay-test", "recharge-1", "1001", "mock_wechat", int64(500), "mock-trade-test", now.UnixMilli(), now.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO wallet_accounts").
		WithArgs("1001", now.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT available_balance").
		WithArgs("1001").
		WillReturnRows(sqlmock.NewRows([]string{"available_balance"}).AddRow(int64(100)))
	mock.ExpectExec("UPDATE wallet_accounts").
		WithArgs(int64(600), now.UnixMilli(), "1001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO wallet_ledger").
		WithArgs("wallet-test", "1001", "credit", int64(500), "mock_recharge", "recharge-1", int64(600), now.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := svc.MockRecharge(ctx, MockRechargeParams{
		UserID:    "1001",
		RequestID: "recharge-1",
		Channel:   "wechat",
		Amount:    500,
	})
	if err != nil {
		t.Fatalf("MockRecharge error = %v", err)
	}
	if result.Wallet.AvailableBalance != 600 || result.Channel != "mock_wechat" || result.AlreadyPaid {
		t.Fatalf("result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRedPacketCreateRejectsInsufficientWalletBalance(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()
	now := time.UnixMilli(1710100000000)
	svc := &RedPacketService{DB: db, Now: func() time.Time { return now }, NewID: func() string { return "rp-test" }}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO wallet_accounts").
		WithArgs("1001", now.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT available_balance").
		WithArgs("1001").
		WillReturnRows(sqlmock.NewRows([]string{"available_balance"}).AddRow(int64(99)))
	mock.ExpectRollback()

	_, err = svc.Create(ctx, RedPacketCreateParams{
		SenderID:       "1001",
		ConversationID: "c2c:1001:1002",
		ToType:         "user",
		TotalAmount:    100,
		TotalCount:     1,
	})
	if err != ErrInsufficientBalance {
		t.Fatalf("Create error = %v, want ErrInsufficientBalance", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
