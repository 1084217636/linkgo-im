package logic

import (
	"context"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(req *types.LoginReq) (*types.LoginResp, error) {
	cli, err := l.svcCtx.LogicRouter.GetClient(l.ctx, req.Username)
	if err != nil {
		return nil, err
	}

	ctx := zrpc.SetHashKey(l.ctx, req.Username)
	reply, err := cli.Login(ctx, &api.LoginReq{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		return nil, err
	}

	return &types.LoginResp{
		Token:         reply.Token,
		UserID:        reply.UserId,
		Conversations: reply.Conversations,
	}, nil
}
