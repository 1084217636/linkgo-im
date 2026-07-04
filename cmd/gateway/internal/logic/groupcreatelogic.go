package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GroupCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGroupCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GroupCreateLogic {
	return &GroupCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GroupCreateLogic) Create(req *types.GroupCreateReq) (*types.GroupCreateResp, error) {
	if req.GroupID == "" || len(req.Members) == 0 {
		return nil, errors.New("group_id and members are required")
	}
	if l.svcCtx.DB == nil {
		return l.createRedisOnly(req)
	}

	creatorID := gwmiddleware.UserIDFromContext(l.ctx)
	memberSet := map[string]struct{}{creatorID: {}}
	for _, member := range req.Members {
		member = strings.TrimSpace(member)
		if member != "" {
			memberSet[member] = struct{}{}
		}
	}
	now := time.Now().UnixMilli()
	tx, err := l.svcCtx.DB.BeginTx(l.ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	groupName := strings.TrimSpace(req.Name)
	if groupName == "" {
		groupName = req.GroupID
	}
	if _, err := tx.ExecContext(l.ctx, `
INSERT INTO im_groups (group_id, name, owner_id, status, created_at, updated_at)
VALUES (?, ?, ?, 'active', ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  status = 'active',
  updated_at = VALUES(updated_at)
`, req.GroupID, groupName, creatorID, now, now); err != nil {
		return nil, err
	}

	for member := range memberSet {
		role := "member"
		if member == creatorID {
			role = "owner"
		}
		if _, err := tx.ExecContext(l.ctx, `
INSERT INTO group_members (group_id, user_id, role, mute_until, status, joined_at)
VALUES (?, ?, ?, 0, 'active', ?)
ON DUPLICATE KEY UPDATE
  role = IF(user_id = ?, 'owner', role),
  status = 'active'
`, req.GroupID, member, role, now, creatorID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for member := range memberSet {
		if err := l.svcCtx.Rdb.SAdd(l.ctx, "group_members:"+req.GroupID, member).Err(); err != nil {
			return nil, err
		}
		if err := l.svcCtx.Rdb.SAdd(l.ctx, "user_groups:"+member, req.GroupID).Err(); err != nil {
			return nil, err
		}
	}

	return &types.GroupCreateResp{
		GroupID: req.GroupID,
		Members: len(memberSet),
		Msg:     "group created",
	}, nil
}

func (l *GroupCreateLogic) createRedisOnly(req *types.GroupCreateReq) (*types.GroupCreateResp, error) {
	creatorID := gwmiddleware.UserIDFromContext(l.ctx)
	memberSet := map[string]struct{}{creatorID: {}}
	for _, member := range req.Members {
		member = strings.TrimSpace(member)
		if member != "" {
			memberSet[member] = struct{}{}
		}
	}

	for member := range memberSet {
		if err := l.svcCtx.Rdb.SAdd(l.ctx, "group_members:"+req.GroupID, member).Err(); err != nil {
			return nil, err
		}
		if err := l.svcCtx.Rdb.SAdd(l.ctx, "user_groups:"+member, req.GroupID).Err(); err != nil {
			return nil, err
		}
	}
	return &types.GroupCreateResp{
		GroupID: req.GroupID,
		Members: len(memberSet),
		Msg:     "group created",
	}, nil
}
