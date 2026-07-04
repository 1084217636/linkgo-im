package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/internal/ids"
	"github.com/go-sql-driver/mysql"
)

const defaultRedPacketTTL = 24 * time.Hour

var (
	ErrRedPacketInvalid        = errors.New("invalid red packet")
	ErrRedPacketNotFound       = errors.New("red packet not found")
	ErrRedPacketAlreadyClaimed = errors.New("red packet already claimed")
	ErrRedPacketFinished       = errors.New("red packet finished")
	ErrRedPacketExpired        = errors.New("red packet expired")
)

type RedPacketService struct {
	DB    *sql.DB
	Now   func() time.Time
	NewID func() string
}

type RedPacketCreateParams struct {
	SenderID       string
	ConversationID string
	ToType         string
	TotalAmount    int64
	TotalCount     int
	Greeting       string
	ExpiresAt      int64
}

type RedPacketInfo struct {
	ID              string `json:"red_packet_id"`
	SenderID        string `json:"sender_id"`
	ConversationID  string `json:"conversation_id"`
	ToType          string `json:"to_type"`
	TotalAmount     int64  `json:"total_amount"`
	TotalCount      int    `json:"total_count"`
	RemainingAmount int64  `json:"remaining_amount"`
	RemainingCount  int    `json:"remaining_count"`
	Greeting        string `json:"greeting"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	ExpiresAt       int64  `json:"expires_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type RedPacketClaimInfo struct {
	RedPacketID string `json:"red_packet_id"`
	UserID      string `json:"user_id"`
	Amount      int64  `json:"amount"`
	CreatedAt   int64  `json:"created_at"`
}

type RedPacketDetail struct {
	Packet  RedPacketInfo        `json:"packet"`
	Claims  []RedPacketClaimInfo `json:"claims"`
	Claimed bool                 `json:"claimed"`
}

func NewRedPacketService(db *sql.DB) *RedPacketService {
	return &RedPacketService{DB: db}
}

func (s *RedPacketService) Create(ctx context.Context, params RedPacketCreateParams) (*RedPacketInfo, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	params.SenderID = strings.TrimSpace(params.SenderID)
	params.ConversationID = strings.TrimSpace(params.ConversationID)
	params.ToType = normalizeRedPacketToType(params.ToType)
	params.Greeting = strings.TrimSpace(params.Greeting)

	if params.SenderID == "" || params.ConversationID == "" {
		return nil, fmt.Errorf("%w: sender_id and conversation_id are required", ErrRedPacketInvalid)
	}
	if params.ToType != "user" && params.ToType != "group" {
		return nil, fmt.Errorf("%w: to_type must be user or group", ErrRedPacketInvalid)
	}
	if params.TotalAmount <= 0 || params.TotalCount <= 0 || params.TotalAmount < int64(params.TotalCount) {
		return nil, fmt.Errorf("%w: total_amount must be >= total_count and both must be positive", ErrRedPacketInvalid)
	}
	if params.TotalCount > 1000 {
		return nil, fmt.Errorf("%w: total_count too large", ErrRedPacketInvalid)
	}
	if len(params.Greeting) > 255 {
		params.Greeting = params.Greeting[:255]
	}

	now := s.now().UnixMilli()
	if params.ExpiresAt <= 0 {
		params.ExpiresAt = now + defaultRedPacketTTL.Milliseconds()
	}
	if params.ExpiresAt <= now {
		return nil, fmt.Errorf("%w: expires_at must be in the future", ErrRedPacketInvalid)
	}

	packetID := s.newID()
	packet := &RedPacketInfo{
		ID:              packetID,
		SenderID:        params.SenderID,
		ConversationID:  params.ConversationID,
		ToType:          params.ToType,
		TotalAmount:     params.TotalAmount,
		TotalCount:      params.TotalCount,
		RemainingAmount: params.TotalAmount,
		RemainingCount:  params.TotalCount,
		Greeting:        params.Greeting,
		Status:          "active",
		CreatedAt:       now,
		ExpiresAt:       params.ExpiresAt,
		UpdatedAt:       now,
	}

	_, err := s.DB.ExecContext(ctx, `
INSERT INTO red_packets
  (id, sender_id, conversation_id, to_type, total_amount, total_count, remaining_amount, remaining_count, greeting, status, created_at, expires_at, updated_at)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)
`, packet.ID, packet.SenderID, packet.ConversationID, packet.ToType, packet.TotalAmount, packet.TotalCount, packet.RemainingAmount, packet.RemainingCount, packet.Greeting, packet.CreatedAt, packet.ExpiresAt, packet.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return packet, nil
}

func (s *RedPacketService) Claim(ctx context.Context, redPacketID, userID string) (*RedPacketClaimInfo, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	redPacketID = strings.TrimSpace(redPacketID)
	userID = strings.TrimSpace(userID)
	if redPacketID == "" || userID == "" {
		return nil, fmt.Errorf("%w: red_packet_id and user_id are required", ErrRedPacketInvalid)
	}

	if claim, ok, err := s.loadClaim(ctx, redPacketID, userID); err != nil {
		return nil, err
	} else if ok {
		return claim, ErrRedPacketAlreadyClaimed
	}

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	packet, err := loadRedPacketForUpdate(ctx, tx, redPacketID)
	if err != nil {
		return nil, err
	}

	now := s.now().UnixMilli()
	if packet.Status != "active" {
		return nil, ErrRedPacketFinished
	}
	if packet.ExpiresAt > 0 && packet.ExpiresAt <= now {
		_ = markRedPacketExpired(ctx, tx, packet.ID, now)
		return nil, ErrRedPacketExpired
	}
	if packet.RemainingCount <= 0 || packet.RemainingAmount <= 0 {
		_ = markRedPacketFinished(ctx, tx, packet.ID, now)
		return nil, ErrRedPacketFinished
	}

	amount := nextEqualClaimAmount(packet)
	if amount <= 0 || amount > packet.RemainingAmount {
		return nil, fmt.Errorf("%w: invalid claim amount", ErrRedPacketInvalid)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO red_packet_claims (red_packet_id, user_id, amount, created_at)
VALUES (?, ?, ?, ?)
`, packet.ID, userID, amount, now)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, ErrRedPacketAlreadyClaimed
		}
		return nil, err
	}

	nextStatus := "active"
	if packet.RemainingCount == 1 || packet.RemainingAmount == amount {
		nextStatus = "finished"
	}
	res, err := tx.ExecContext(ctx, `
UPDATE red_packets
SET remaining_amount = remaining_amount - ?,
    remaining_count = remaining_count - 1,
    status = ?,
    updated_at = ?
WHERE id = ? AND remaining_amount >= ? AND remaining_count > 0
`, amount, nextStatus, now, packet.ID, amount)
	if err != nil {
		return nil, err
	}
	if affected, err := res.RowsAffected(); err == nil && affected != 1 {
		return nil, ErrRedPacketFinished
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RedPacketClaimInfo{
		RedPacketID: packet.ID,
		UserID:      userID,
		Amount:      amount,
		CreatedAt:   now,
	}, nil
}

func (s *RedPacketService) Detail(ctx context.Context, redPacketID, userID string) (*RedPacketDetail, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	redPacketID = strings.TrimSpace(redPacketID)
	if redPacketID == "" {
		return nil, fmt.Errorf("%w: red_packet_id is required", ErrRedPacketInvalid)
	}

	packet, err := s.loadPacket(ctx, redPacketID)
	if err != nil {
		return nil, err
	}
	claims, err := s.listClaims(ctx, redPacketID)
	if err != nil {
		return nil, err
	}
	detail := &RedPacketDetail{
		Packet: packet,
		Claims: claims,
	}
	for _, claim := range claims {
		if claim.UserID == userID {
			detail.Claimed = true
			break
		}
	}
	return detail, nil
}

func (s *RedPacketService) loadPacket(ctx context.Context, redPacketID string) (RedPacketInfo, error) {
	var packet RedPacketInfo
	err := s.DB.QueryRowContext(ctx, `
SELECT id, sender_id, conversation_id, to_type, total_amount, total_count, remaining_amount, remaining_count, greeting, status, created_at, expires_at, updated_at
FROM red_packets
WHERE id = ?
`, redPacketID).Scan(&packet.ID, &packet.SenderID, &packet.ConversationID, &packet.ToType, &packet.TotalAmount, &packet.TotalCount, &packet.RemainingAmount, &packet.RemainingCount, &packet.Greeting, &packet.Status, &packet.CreatedAt, &packet.ExpiresAt, &packet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return packet, ErrRedPacketNotFound
	}
	return packet, err
}

func (s *RedPacketService) listClaims(ctx context.Context, redPacketID string) ([]RedPacketClaimInfo, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT red_packet_id, user_id, amount, created_at
FROM red_packet_claims
WHERE red_packet_id = ?
ORDER BY created_at ASC
`, redPacketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claims := make([]RedPacketClaimInfo, 0)
	for rows.Next() {
		var claim RedPacketClaimInfo
		if err := rows.Scan(&claim.RedPacketID, &claim.UserID, &claim.Amount, &claim.CreatedAt); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (s *RedPacketService) loadClaim(ctx context.Context, redPacketID, userID string) (*RedPacketClaimInfo, bool, error) {
	var claim RedPacketClaimInfo
	err := s.DB.QueryRowContext(ctx, `
SELECT red_packet_id, user_id, amount, created_at
FROM red_packet_claims
WHERE red_packet_id = ? AND user_id = ?
`, redPacketID, userID).Scan(&claim.RedPacketID, &claim.UserID, &claim.Amount, &claim.CreatedAt)
	if err == nil {
		return &claim, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return nil, false, err
}

func loadRedPacketForUpdate(ctx context.Context, tx *sql.Tx, redPacketID string) (RedPacketInfo, error) {
	var packet RedPacketInfo
	err := tx.QueryRowContext(ctx, `
SELECT id, sender_id, conversation_id, to_type, total_amount, total_count, remaining_amount, remaining_count, greeting, status, created_at, expires_at, updated_at
FROM red_packets
WHERE id = ?
FOR UPDATE
`, redPacketID).Scan(&packet.ID, &packet.SenderID, &packet.ConversationID, &packet.ToType, &packet.TotalAmount, &packet.TotalCount, &packet.RemainingAmount, &packet.RemainingCount, &packet.Greeting, &packet.Status, &packet.CreatedAt, &packet.ExpiresAt, &packet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return packet, ErrRedPacketNotFound
	}
	return packet, err
}

func markRedPacketFinished(ctx context.Context, tx *sql.Tx, redPacketID string, now int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE red_packets SET status = 'finished', updated_at = ? WHERE id = ?", now, redPacketID)
	return err
}

func markRedPacketExpired(ctx context.Context, tx *sql.Tx, redPacketID string, now int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE red_packets SET status = 'expired', updated_at = ? WHERE id = ?", now, redPacketID)
	return err
}

func nextEqualClaimAmount(packet RedPacketInfo) int64 {
	if packet.TotalCount <= 0 || packet.RemainingCount <= 0 {
		return 0
	}
	base := packet.TotalAmount / int64(packet.TotalCount)
	remainder := packet.TotalAmount % int64(packet.TotalCount)
	claimedCount := packet.TotalCount - packet.RemainingCount
	amount := base
	if int64(claimedCount) < remainder {
		amount++
	}
	if packet.RemainingCount == 1 {
		amount = packet.RemainingAmount
	}
	return amount
}

func (s *RedPacketService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *RedPacketService) newID() string {
	if s != nil && s.NewID != nil {
		return s.NewID()
	}
	return strings.Replace(ids.NewTraceID(), "tr-", "rp-", 1)
}

func normalizeRedPacketToType(toType string) string {
	toType = strings.ToLower(strings.TrimSpace(toType))
	if toType == "" {
		return "user"
	}
	return toType
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	return false
}
