package handler

import (
	"context"
	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/service"
	"google.golang.org/protobuf/proto"
)

type LogicServer struct {
	api.UnimplementedLogicServer
	ChatSvc *service.ChatService // 持有业务对象
}

func (s *LogicServer) PushMessage(ctx context.Context, req *api.PushMsgReq) (*api.PushMsgReply, error) {
	// 1. 解析参数
	msg := &api.ChatMessage{}
	proto.Unmarshal(req.Content, msg)
	
	// 兼容代码
	if msg.ToUserId == "Group1" {
		msg.GroupId = "Group1"
		msg.ToUserId = ""
	}

	// 2. 【关键】直接丢给 Service 层去干活
	err := s.ChatSvc.HandlePush(ctx, msg)
	
	return &api.PushMsgReply{}, err
}

func (s *LogicServer) UserLogin(ctx context.Context, req *api.UserLoginReq) (*api.UserLoginReply, error) {
	// 直接丢给 Service
	go s.ChatSvc.HandleLogin(ctx, req.UserId)
	return &api.UserLoginReply{}, nil
}