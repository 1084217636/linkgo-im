package svc

import (
	"context"
	"testing"

	"github.com/1084217636/linkgo-im/api"
)

func TestAIBotResponderOnlyRepliesToBotPrivateMessage(t *testing.T) {
	responder := &aiBotResponder{botID: "9001"}

	ignored, err := responder.BuildReply(context.Background(), &api.WireMessage{
		From:   "1001",
		To:     "1002",
		ToType: "user",
		Body:   "hello",
	})
	if err != nil {
		t.Fatalf("BuildReply non-bot error = %v", err)
	}
	if ignored != nil {
		t.Fatal("BuildReply replied to a non-bot private message")
	}

	reply, err := responder.BuildReply(context.Background(), &api.WireMessage{
		MessageId: "c2c:1001:9001-1",
		From:      "1001",
		To:        "9001",
		ToType:    "user",
		Body:      "项目里 Redis 用来做什么？",
	})
	if err != nil {
		t.Fatalf("BuildReply bot message error = %v", err)
	}
	if reply == nil {
		t.Fatal("BuildReply did not reply to bot private message")
	}
	if reply.From != "9001" || reply.To != "1001" || reply.ToType != "user" {
		t.Fatalf("unexpected reply route: %#v", reply)
	}
	if reply.Body == "" || reply.ClientMsgId == "" || reply.TraceId == "" {
		t.Fatalf("reply missing required fields: %#v", reply)
	}
}
