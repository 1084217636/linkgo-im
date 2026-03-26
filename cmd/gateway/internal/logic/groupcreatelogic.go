package logic

import (
	"context"
	"errors"

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

	creatorID := gwmiddleware.UserIDFromContext(l.ctx)
	memberSet := map[string]struct{}{creatorID: {}}
	for _, member := range req.Members {
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
