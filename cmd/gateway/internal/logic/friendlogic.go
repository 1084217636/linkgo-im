package logic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type FriendLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFriendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FriendLogic {
	return &FriendLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FriendLogic) Apply(req *types.FriendApplyReq) (*types.FriendApplyResp, error) {
	if l.svcCtx.DB == nil {
		return nil, errors.New("database is required")
	}
	fromUID := gwmiddleware.UserIDFromContext(l.ctx)
	toUID := strings.TrimSpace(req.TargetUserID)
	if fromUID == "" || toUID == "" {
		return nil, errors.New("target_user_id is required")
	}
	if fromUID == toUID {
		return nil, errors.New("cannot apply to yourself")
	}
	if exists, err := l.userExists(toUID); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("target user %s not found", toUID)
	}
	if normal, err := l.friendRelationNormal(fromUID, toUID); err != nil {
		return nil, err
	} else if normal {
		now := time.Now().UnixMilli()
		return &types.FriendApplyResp{FromUserID: fromUID, ToUserID: toUID, Status: "accepted", UpdatedAt: now}, nil
	}

	now := time.Now().UnixMilli()
	if _, err := l.svcCtx.DB.ExecContext(l.ctx, `
INSERT INTO friend_requests (from_user_id, to_user_id, message, status, created_at, updated_at)
VALUES (?, ?, ?, 'pending', ?, ?)
ON DUPLICATE KEY UPDATE
  message = VALUES(message),
  status = 'pending',
  updated_at = VALUES(updated_at)
`, fromUID, toUID, req.Message, now, now); err != nil {
		return nil, err
	}
	return &types.FriendApplyResp{FromUserID: fromUID, ToUserID: toUID, Status: "pending", UpdatedAt: now}, nil
}

func (l *FriendLogic) Respond(req *types.FriendRespondReq) (*types.FriendRespondResp, error) {
	if l.svcCtx.DB == nil {
		return nil, errors.New("database is required")
	}
	toUID := gwmiddleware.UserIDFromContext(l.ctx)
	fromUID := strings.TrimSpace(req.FromUserID)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if fromUID == "" {
		return nil, errors.New("from_user_id is required")
	}
	if action != "accept" && action != "reject" {
		return nil, errors.New("action must be accept or reject")
	}

	status := "rejected"
	if action == "accept" {
		status = "accepted"
	}
	now := time.Now().UnixMilli()
	tx, err := l.svcCtx.DB.BeginTx(l.ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(l.ctx, `
UPDATE friend_requests
SET status = ?, updated_at = ?
WHERE from_user_id = ? AND to_user_id = ? AND status = 'pending'
`, status, now, fromUID, toUID)
	if err != nil {
		return nil, err
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return nil, errors.New("pending friend request not found")
	}

	if status == "accepted" {
		if err := upsertFriendRelation(l.ctx, tx, fromUID, toUID, now); err != nil {
			return nil, err
		}
		if err := upsertFriendRelation(l.ctx, tx, toUID, fromUID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &types.FriendRespondResp{FromUserID: fromUID, ToUserID: toUID, Status: status, UpdatedAt: now}, nil
}

func (l *FriendLogic) ListFriends() (*types.FriendListResp, error) {
	if l.svcCtx.DB == nil {
		return &types.FriendListResp{}, nil
	}
	uid := gwmiddleware.UserIDFromContext(l.ctx)
	rows, err := l.svcCtx.DB.QueryContext(l.ctx, `
SELECT friend_id, status, updated_at
FROM friend_relations
WHERE user_id = ? AND status = 'normal'
ORDER BY updated_at DESC
`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	friends := make([]types.FriendInfo, 0)
	for rows.Next() {
		var item types.FriendInfo
		if err := rows.Scan(&item.UserID, &item.Status, &item.UpdatedAt); err != nil {
			return nil, err
		}
		friends = append(friends, item)
	}
	return &types.FriendListResp{Friends: friends}, rows.Err()
}

func (l *FriendLogic) ListRequests() (*types.FriendRequestsResp, error) {
	if l.svcCtx.DB == nil {
		return &types.FriendRequestsResp{}, nil
	}
	uid := gwmiddleware.UserIDFromContext(l.ctx)
	rows, err := l.svcCtx.DB.QueryContext(l.ctx, `
SELECT from_user_id, to_user_id, message, status, created_at, updated_at
FROM friend_requests
WHERE to_user_id = ? AND status = 'pending'
ORDER BY updated_at DESC
`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := make([]types.FriendRequestInfo, 0)
	for rows.Next() {
		var item types.FriendRequestInfo
		if err := rows.Scan(&item.FromUserID, &item.ToUserID, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, item)
	}
	return &types.FriendRequestsResp{Requests: requests}, rows.Err()
}

func (l *FriendLogic) userExists(uid string) (bool, error) {
	var found string
	err := l.svcCtx.DB.QueryRowContext(l.ctx, "SELECT user_id FROM users WHERE user_id = ? LIMIT 1", uid).Scan(&found)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (l *FriendLogic) friendRelationNormal(uid, friendID string) (bool, error) {
	var status string
	err := l.svcCtx.DB.QueryRowContext(l.ctx, `
SELECT status FROM friend_relations WHERE user_id = ? AND friend_id = ? LIMIT 1
`, uid, friendID).Scan(&status)
	if err == nil {
		return status == "normal", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

type friendRelationTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertFriendRelation(ctx context.Context, tx friendRelationTx, uid, friendID string, now int64) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO friend_relations (user_id, friend_id, status, created_at, updated_at)
VALUES (?, ?, 'normal', ?, ?)
ON DUPLICATE KEY UPDATE
  status = 'normal',
  updated_at = VALUES(updated_at)
`, uid, friendID, now, now)
	return err
}
