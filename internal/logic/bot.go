package logic

import (
	"context"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultAIBotUserID = "9001"
	botResponseTimeout = 20 * time.Second
)

type BotResponder interface {
	BuildReply(ctx context.Context, incoming *api.WireMessage) (*api.WireMessage, error)
}

func (h *LogicHandler) triggerBotResponse(incoming *api.WireMessage) {
	if h == nil || h.BotResponder == nil || incoming == nil {
		return
	}
	incomingCopy := *incoming
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), botResponseTimeout)
		defer cancel()

		reply, err := h.BotResponder.BuildReply(ctx, &incomingCopy)
		if err != nil {
			logx.Errorw("build ai bot reply failed",
				logx.Field("trace_id", incomingCopy.TraceId),
				logx.Field("message_id", incomingCopy.MessageId),
				logx.Field("target_id", incomingCopy.To),
				logx.Field("error", err.Error()),
			)
			return
		}
		if reply == nil {
			return
		}
		payload, err := proto.Marshal(reply)
		if err != nil {
			logx.Errorw("marshal ai bot reply failed",
				logx.Field("trace_id", incomingCopy.TraceId),
				logx.Field("message_id", incomingCopy.MessageId),
				logx.Field("error", err.Error()),
			)
			return
		}
		if _, err := h.PushMessage(ctx, &api.PushMsgReq{UserId: reply.From, Content: payload}); err != nil {
			logx.Errorw("send ai bot reply failed",
				logx.Field("trace_id", reply.TraceId),
				logx.Field("client_msg_id", reply.ClientMsgId),
				logx.Field("target_id", reply.To),
				logx.Field("error", err.Error()),
			)
		}
	}()
}
