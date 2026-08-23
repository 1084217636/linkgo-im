package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/internal/ids"
)

var (
	ErrWalletInvalid       = errors.New("invalid wallet operation")
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
)

type WalletService struct {
	DB    *sql.DB
	Now   func() time.Time
	NewID func(prefix string) string
}

type WalletInfo struct {
	UserID           string `json:"user_id"`
	AvailableBalance int64  `json:"available_balance"`
	UpdatedAt        int64  `json:"updated_at"`
}

type WalletLedgerEntry struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	Direction    string `json:"direction"`
	Amount       int64  `json:"amount"`
	BusinessType string `json:"business_type"`
	BusinessID   string `json:"business_id"`
	BalanceAfter int64  `json:"balance_after"`
	CreatedAt    int64  `json:"created_at"`
}

type MockRechargeParams struct {
	UserID    string
	RequestID string
	Channel   string
	Amount    int64
}

type MockRechargeResult struct {
	OrderID         string     `json:"order_id"`
	RequestID       string     `json:"request_id"`
	Channel         string     `json:"channel"`
	Amount          int64      `json:"amount"`
	ProviderTradeNo string     `json:"provider_trade_no"`
	AlreadyPaid     bool       `json:"already_paid"`
	Wallet          WalletInfo `json:"wallet"`
}

func NewWalletService(db *sql.DB) *WalletService {
	return &WalletService{DB: db}
}

func (s *WalletService) Get(ctx context.Context, userID string) (*WalletInfo, []WalletLedgerEntry, error) {
	if s == nil || s.DB == nil {
		return nil, nil, errors.New("database is required")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil, fmt.Errorf("%w: user_id is required", ErrWalletInvalid)
	}

	wallet := WalletInfo{UserID: userID}
	err := s.DB.QueryRowContext(ctx, `
SELECT user_id, available_balance, updated_at
FROM wallet_accounts
WHERE user_id = ?
`, userID).Scan(&wallet.UserID, &wallet.AvailableBalance, &wallet.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	rows, err := s.DB.QueryContext(ctx, `
SELECT id, user_id, direction, amount, business_type, business_id, balance_after, created_at
FROM wallet_ledger
WHERE user_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 50
`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ledger := make([]WalletLedgerEntry, 0)
	for rows.Next() {
		var entry WalletLedgerEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.Direction,
			&entry.Amount,
			&entry.BusinessType,
			&entry.BusinessID,
			&entry.BalanceAfter,
			&entry.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		ledger = append(ledger, entry)
	}
	return &wallet, ledger, rows.Err()
}

func (s *WalletService) MockRecharge(ctx context.Context, params MockRechargeParams) (*MockRechargeResult, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	params.UserID = strings.TrimSpace(params.UserID)
	params.RequestID = strings.TrimSpace(params.RequestID)
	params.Channel = strings.ToLower(strings.TrimSpace(params.Channel))
	if params.Channel == "wechat" {
		params.Channel = "mock_wechat"
	}
	if params.Channel == "alipay" {
		params.Channel = "mock_alipay"
	}
	if params.UserID == "" || params.RequestID == "" {
		return nil, fmt.Errorf("%w: user_id and request_id are required", ErrWalletInvalid)
	}
	if len(params.RequestID) > 128 {
		return nil, fmt.Errorf("%w: request_id is too long", ErrWalletInvalid)
	}
	if params.Channel != "mock_wechat" && params.Channel != "mock_alipay" {
		return nil, fmt.Errorf("%w: channel must be mock_wechat or mock_alipay", ErrWalletInvalid)
	}
	if params.Amount <= 0 || params.Amount > 1_000_000 {
		return nil, fmt.Errorf("%w: amount must be between 1 and 1000000 fen", ErrWalletInvalid)
	}

	if existing, ok, err := s.loadRecharge(ctx, params.UserID, params.RequestID); err != nil {
		return nil, err
	} else if ok {
		if existing.Amount != params.Amount || existing.Channel != params.Channel {
			return nil, fmt.Errorf("%w: request_id was already used with different recharge parameters", ErrWalletInvalid)
		}
		existing.AlreadyPaid = true
		wallet, _, err := s.Get(ctx, params.UserID)
		if err != nil {
			return nil, err
		}
		existing.Wallet = *wallet
		return existing, nil
	}

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := s.now().UnixMilli()
	orderID := s.newID("pay")
	providerTradeNo := s.newID("mock-trade")
	_, err = tx.ExecContext(ctx, `
INSERT INTO mock_payment_orders
  (id, request_id, user_id, channel, amount, status, provider_trade_no, created_at, paid_at)
VALUES (?, ?, ?, ?, ?, 'paid', ?, ?, ?)
`, orderID, params.RequestID, params.UserID, params.Channel, params.Amount, providerTradeNo, now, now)
	if err != nil {
		if isDuplicateKey(err) {
			_ = tx.Rollback()
			return s.MockRecharge(ctx, params)
		}
		return nil, err
	}

	balance, err := lockWallet(ctx, tx, params.UserID, now)
	if err != nil {
		return nil, err
	}
	balance += params.Amount
	if err := updateWalletAndLedger(
		ctx,
		tx,
		s.newID("wallet"),
		params.UserID,
		"credit",
		params.Amount,
		"mock_recharge",
		params.RequestID,
		balance,
		now,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MockRechargeResult{
		OrderID:         orderID,
		RequestID:       params.RequestID,
		Channel:         params.Channel,
		Amount:          params.Amount,
		ProviderTradeNo: providerTradeNo,
		Wallet:          WalletInfo{UserID: params.UserID, AvailableBalance: balance, UpdatedAt: now},
	}, nil
}

func (s *WalletService) loadRecharge(ctx context.Context, userID, requestID string) (*MockRechargeResult, bool, error) {
	var result MockRechargeResult
	err := s.DB.QueryRowContext(ctx, `
SELECT id, request_id, channel, amount, provider_trade_no
FROM mock_payment_orders
WHERE user_id = ? AND request_id = ? AND status = 'paid'
`, userID, requestID).Scan(
		&result.OrderID,
		&result.RequestID,
		&result.Channel,
		&result.Amount,
		&result.ProviderTradeNo,
	)
	if err == nil {
		return &result, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return nil, false, err
}

func lockWallet(ctx context.Context, tx *sql.Tx, userID string, now int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO wallet_accounts (user_id, available_balance, updated_at)
VALUES (?, 0, ?)
ON DUPLICATE KEY UPDATE user_id = VALUES(user_id)
`, userID, now); err != nil {
		return 0, err
	}
	var balance int64
	if err := tx.QueryRowContext(ctx, `
SELECT available_balance
FROM wallet_accounts
WHERE user_id = ?
FOR UPDATE
`, userID).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}

func updateWalletAndLedger(
	ctx context.Context,
	tx *sql.Tx,
	ledgerID string,
	userID string,
	direction string,
	amount int64,
	businessType string,
	businessID string,
	balanceAfter int64,
	now int64,
) error {
	if balanceAfter < 0 || amount <= 0 {
		return ErrInsufficientBalance
	}
	result, err := tx.ExecContext(ctx, `
UPDATE wallet_accounts
SET available_balance = ?, updated_at = ?
WHERE user_id = ?
`, balanceAfter, now, userID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected != 1 {
		return errors.New("wallet account update affected no rows")
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO wallet_ledger
  (id, user_id, direction, amount, business_type, business_id, balance_after, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, ledgerID, userID, direction, amount, businessType, businessID, balanceAfter, now)
	return err
}

func (s *WalletService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *WalletService) newID(prefix string) string {
	if s != nil && s.NewID != nil {
		return s.NewID(prefix)
	}
	return strings.Replace(ids.NewTraceID(), "tr-", prefix+"-", 1)
}
