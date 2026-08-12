package logic

import (
	"context"
	"errors"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/internal/svc"
	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	reply, err := l.svcCtx.Core.PushMessage(l.ctx, in)
	if errors.Is(err, corelogic.ErrClientMessageInFlight) {
		return nil, status.Error(codes.Aborted, "client message is still processing")
	}
	return reply, err
}
