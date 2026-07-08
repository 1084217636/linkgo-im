package svc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/ai"
	"github.com/1084217636/linkgo-im/internal/ids"
)

type aiBotResponder struct {
	botID string
	ask   *ai.AskService
}

func (r *aiBotResponder) BuildReply(ctx context.Context, incoming *api.WireMessage) (*api.WireMessage, error) {
	if r == nil || incoming == nil {
		return nil, nil
	}
	if incoming.ToType != "user" || incoming.To != r.botID || incoming.From == r.botID {
		return nil, nil
	}
	question := strings.TrimSpace(incoming.Body)
	if question == "" {
		return nil, nil
	}

	answer := "我已经收到你的消息。当前版本会优先回答项目知识、IM 链路、红包并发和 AI 接入相关问题。"
	if r.ask != nil {
		result, err := r.ask.Ask(ctx, ai.AskParams{
			OperatorID: incoming.From,
			Question:   question,
		})
		if err != nil {
			return nil, err
		}
		if result != nil && strings.TrimSpace(result.Answer) != "" {
			answer = result.Answer
		}
	}

	return &api.WireMessage{
		From:        r.botID,
		To:          incoming.From,
		ToType:      "user",
		MsgType:     api.MsgType_NORMAL,
		Body:        answer,
		ClientMsgId: fmt.Sprintf("bot:%s:%d", incoming.MessageId, time.Now().UnixMilli()),
		TraceId:     ids.NewTraceID(),
		SentAt:      time.Now().UnixMilli(),
	}, nil
}
