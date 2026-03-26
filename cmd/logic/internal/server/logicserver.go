package server

import (
	"context"

	"github.com/1084217636/linkgo-im/api"
	logiclogic "github.com/1084217636/linkgo-im/cmd/logic/internal/logic"
	"github.com/1084217636/linkgo-im/cmd/logic/internal/svc"
)

type LogicServer struct {
	svcCtx *svc.ServiceContext
	api.UnimplementedLogicServer
}

func NewLogicServer(svcCtx *svc.ServiceContext) *LogicServer {
	return &LogicServer{svcCtx: svcCtx}
}

func (s *LogicServer) Login(ctx context.Context, in *api.LoginReq) (*api.LoginReply, error) {
	impl := logiclogic.NewLoginLogic(ctx, s.svcCtx)
	return impl.Login(in)
}

func (s *LogicServer) PushMessage(ctx context.Context, in *api.PushMsgReq) (*api.PushMsgReply, error) {
	impl := logiclogic.NewPushMessageLogic(ctx, s.svcCtx)
	return impl.PushMessage(in)
}

func (s *LogicServer) UserLogin(ctx context.Context, in *api.UserLoginReq) (*api.UserLoginReply, error) {
	impl := logiclogic.NewUserLoginLogic(ctx, s.svcCtx)
	return impl.UserLogin(in)
}

func (s *LogicServer) GetHistory(ctx context.Context, in *api.GetHistoryReq) (*api.GetHistoryReply, error) {
	impl := logiclogic.NewGetHistoryLogic(ctx, s.svcCtx)
	return impl.GetHistory(in)
}
