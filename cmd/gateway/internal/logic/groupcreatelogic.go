package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/go-sql-driver/mysql"
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
		return nil, errors.New("group store is unavailable")
	}

	creatorID := gwmiddleware.UserIDFromContext(l.ctx)
	if creatorID == "" {
		return nil, errors.New("authenticated creator is required")
	}
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
`, req.GroupID, groupName, creatorID, now, now); err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, errors.New("group already exists")
		}
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
	for member := range memberSet {
		if _, err := tx.ExecContext(l.ctx, `
INSERT INTO conversation_members (conversation_id, user_id, read_seq, acked_seq, joined_at)
VALUES (?, ?, 0, 0, ?)
ON DUPLICATE KEY UPDATE joined_at = LEAST(joined_at, VALUES(joined_at))
`, "group:"+req.GroupID, member, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if l.svcCtx.Rdb != nil {
		pipe := l.svcCtx.Rdb.TxPipeline()
		for member := range memberSet {
			pipe.SAdd(l.ctx, "group_members:"+req.GroupID, member)
			pipe.SAdd(l.ctx, "user_groups:"+member, req.GroupID)
		}
		if _, err := pipe.Exec(l.ctx); err != nil {
			logx.Errorw("cache committed group membership failed",
				logx.Field("group_id", req.GroupID),
				logx.Field("creator_id", creatorID),
				logx.Field("error", err.Error()),
			)
		}
	}

	return &types.GroupCreateResp{
		GroupID: req.GroupID,
		Members: len(memberSet),
		Msg:     "group created",
	}, nil
}
