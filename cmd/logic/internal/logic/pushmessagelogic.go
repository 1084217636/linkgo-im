package logic

import (
	"context"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

type PushMessageLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPushMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PushMessageLogic {
	return &PushMessageLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *PushMessageLogic) PushMessage(in *api.PushMsgReq) (*api.PushMsgReply, error) {
	return l.svcCtx.Core.PushMessage(l.ctx, in)
}
