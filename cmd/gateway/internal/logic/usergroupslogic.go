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
	groups, err := l.svcCtx.Rdb.SMembers(l.ctx, "user_groups:"+uid).Result()
	if err != nil {
		return nil, err
	}
	return &types.UserGroupsResp{Groups: groups}, nil
}
