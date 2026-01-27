package service

import (
	"context"
	"fmt"
	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/cmd/logic/repo" // 引用 repo 层
	"google.golang.org/protobuf/proto"
	"github.com/redis/go-redis/v9"
)

type ChatService struct {
    // 依赖 Gateway 客户端 (这也是一种 repo，但为了简单先放这)
	GatewayCli api.GatewayClient 
}

// HandlePush 处理消息发送逻辑
func (s *ChatService) HandlePush(ctx context.Context, msg *api.ChatMessage) error {
	// 1. 生成 Seq (调用 Repo)
	var seqKey string
	if msg.GroupId != "" {
		seqKey = fmt.Sprintf("seq:group:%s", msg.GroupId)
	} else {
		seqKey = fmt.Sprintf("seq:user:%s", msg.ToUserId)
	}
	
	newSeq, _ := repo.GetNextSeq(ctx, seqKey) // 不处理 error 简化演示
	msg.Seq = newSeq
	
	fmt.Printf("🧠 [Service] 业务处理: Seq=%d\n", msg.Seq)

	// 2. 分发逻辑
	if msg.GroupId != "" {
		// 群聊 -> 扔给 Kafka (调用 Repo)
		bytes, _ := proto.Marshal(msg)
		return repo.SendToKafka("group_msg", msg.GroupId, bytes)
	} else {
		// 私聊 -> 直接推送
		return s.sendDirect(ctx, msg)
	}
}

func (s *ChatService) sendDirect(ctx context.Context, msg *api.ChatMessage) error {
	// 1. 查路由 (调用 Repo)
	_, err := repo.GetUserRoute(ctx, msg.ToUserId)
	if err == redis.Nil {
		// 离线，不推了，Transfer 会存库
		return nil 
	}
	
	// 2. 在线推送
	bytes, _ := proto.Marshal(msg)
	_, err = s.GatewayCli.PushToUser(context.Background(), &api.SendToUserReq{
		TargetUserId: msg.ToUserId,
		Content:      bytes,
	})
	return err
}

// HandleLogin 处理登录拉取逻辑
func (s *ChatService) HandleLogin(ctx context.Context, userId string) {
	// 1. 查历史 (调用 Repo)
	msgs := repo.GetHistoryMsgs(userId, 20)
	
	// 2. 推送
	for i := len(msgs) - 1; i >= 0; i-- {
		s.GatewayCli.PushToUser(ctx, &api.SendToUserReq{
			TargetUserId: userId,
			Content:      msgs[i].Content,
		})
	}
}