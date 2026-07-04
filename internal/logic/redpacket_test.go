package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNextEqualClaimAmountDistributesRemainder(t *testing.T) {
	packet := RedPacketInfo{TotalAmount: 100, TotalCount: 3, RemainingAmount: 100, RemainingCount: 3}
	if got := nextEqualClaimAmount(packet); got != 34 {
		t.Fatalf("first amount = %d, want 34", got)
	}
	packet.RemainingAmount = 66
	packet.RemainingCount = 2
	if got := nextEqualClaimAmount(packet); got != 33 {
		t.Fatalf("second amount = %d, want 33", got)
	}
	packet.RemainingAmount = 33
	packet.RemainingCount = 1
	if got := nextEqualClaimAmount(packet); got != 33 {
		t.Fatalf("last amount = %d, want 33", got)
	}
}

func TestRedPacketCreatePersistsInitialState(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.UnixMilli(1710100000000)
	svc := &RedPacketService{
		DB:    db,
		Now:   func() time.Time { return now },
		NewID: func() string { return "rp-test" },
	}

	mock.ExpectExec("INSERT INTO red_packets").
		WithArgs(
			"rp-test",
			"1001",
			"c2c:1001:1002",
			"user",
			int64(100),
			2,
			int64(100),
			2,
			"恭喜发财",
			now.UnixMilli(),
			now.Add(defaultRedPacketTTL).UnixMilli(),
			now.UnixMilli(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	packet, err := svc.Create(ctx, RedPacketCreateParams{
		SenderID:       "1001",
		ConversationID: "c2c:1001:1002",
		ToType:         "user",
		TotalAmount:    100,
		TotalCount:     2,
		Greeting:       "恭喜发财",
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if packet.ID != "rp-test" || packet.RemainingAmount != 100 || packet.RemainingCount != 2 || packet.Status != "active" {
		t.Fatalf("packet = %#v", packet)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRedPacketClaimLocksAndUpdates(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	now := time.UnixMilli(1710100000000)
	svc := &RedPacketService{
		DB:  db,
		Now: func() time.Time { return now },
	}

	mock.ExpectQuery("SELECT red_packet_id, user_id, amount, created_at").
		WithArgs("rp-1", "1002").
		WillReturnRows(sqlmock.NewRows([]string{"red_packet_id", "user_id", "amount", "created_at"}))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, sender_id, conversation_id").
		WithArgs("rp-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "sender_id", "conversation_id", "to_type", "total_amount", "total_count",
			"remaining_amount", "remaining_count", "greeting", "status", "created_at", "expires_at", "updated_at",
		}).AddRow("rp-1", "1001", "c2c:1001:1002", "user", int64(100), 2, int64(100), 2, "hi", "active", int64(1), now.Add(time.Hour).UnixMilli(), int64(1)))
	mock.ExpectExec("INSERT INTO red_packet_claims").
		WithArgs("rp-1", "1002", int64(50), now.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE red_packets").
		WithArgs(int64(50), "active", now.UnixMilli(), "rp-1", int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claim, err := svc.Claim(ctx, "rp-1", "1002")
	if err != nil {
		t.Fatalf("Claim error = %v", err)
	}
	if claim.Amount != 50 || claim.UserID != "1002" {
		t.Fatalf("claim = %#v", claim)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRedPacketClaimAlreadyClaimedReturnsExistingClaim(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error = %v", err)
	}
	defer db.Close()

	svc := &RedPacketService{DB: db}
	mock.ExpectQuery("SELECT red_packet_id, user_id, amount, created_at").
		WithArgs("rp-1", "1002").
		WillReturnRows(sqlmock.NewRows([]string{"red_packet_id", "user_id", "amount", "created_at"}).
			AddRow("rp-1", "1002", int64(88), int64(1710100000000)))

	claim, err := svc.Claim(ctx, "rp-1", "1002")
	if !errors.Is(err, ErrRedPacketAlreadyClaimed) {
		t.Fatalf("Claim error = %v, want ErrRedPacketAlreadyClaimed", err)
	}
	if claim == nil || claim.Amount != 88 {
		t.Fatalf("claim = %#v", claim)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
