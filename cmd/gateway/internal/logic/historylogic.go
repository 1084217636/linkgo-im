package logic

import (
	"context"
	"errors"

	"github.com/1084217636/linkgo-im/api"
	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type HistoryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HistoryLogic {
	return &HistoryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HistoryLogic) GetHistory(req *types.HistoryReq) (*types.HistoryResp, error) {
	if req.TargetID == "" {
		return nil, errors.New("target_id is required")
	}

	userID := gwmiddleware.UserIDFromContext(l.ctx)
	cli, err := l.svcCtx.LogicRouter.GetClient(l.ctx, userID)
	if err != nil {
		return nil, err
	}

	ctx := zrpc.SetHashKey(l.ctx, userID)
	reply, err := cli.GetHistory(ctx, &api.GetHistoryReq{
		UserId:   userID,
		TargetId: req.TargetID,
	})
	if err != nil {
		return nil, err
	}

	return &types.HistoryResp{Data: reply.Messages}, nil
}
