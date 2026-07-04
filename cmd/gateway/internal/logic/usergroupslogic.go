package logic

import (
	"context"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type UserGroupsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupsLogic {
	return &UserGroupsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupsLogic) List() (*types.UserGroupsResp, error) {
	uid := gwmiddleware.UserIDFromContext(l.ctx)
	if l.svcCtx.DB != nil {
		rows, err := l.svcCtx.DB.QueryContext(l.ctx, `
SELECT group_id
FROM group_members
WHERE user_id = ? AND status = 'active'
ORDER BY joined_at DESC
`, uid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		groups := make([]string, 0)
		for rows.Next() {
			var groupID string
			if err := rows.Scan(&groupID); err != nil {
				return nil, err
			}
			groups = append(groups, groupID)
		}
		return &types.UserGroupsResp{Groups: groups}, rows.Err()
	}
	groups, err := l.svcCtx.Rdb.SMembers(l.ctx, "user_groups:"+uid).Result()
	if err != nil {
		return nil, err
	}
	return &types.UserGroupsResp{Groups: groups}, nil
}
