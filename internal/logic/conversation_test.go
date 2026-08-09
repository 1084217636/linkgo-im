package logic

import (
	"context"
	"testing"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestConversationTitleForUser(t *testing.T) {
	if got := conversationTitleForUser("1001", "c2c:1001:1002", "user", ""); got != "1002" {
		t.Fatalf("title for 1001 = %q, want 1002", got)
	}
	if got := conversationTitleForUser("1002", "c2c:1001:1002", "user", ""); got != "1001" {
		t.Fatalf("title for 1002 = %q, want 1001", got)
	}
	if got := conversationTitleForUser("1001", "group:G1", "group", "Project"); got != "Project" {
		t.Fatalf("group title = %q, want Project", got)
	}
}

func TestConversationMembers(t *testing.T) {
	frame := &api.WireMessage{
		From:   "1001",
		To:     "G1",
		ToType: "group",
	}
	members := conversationMembers(frame, []string{"1002", "1003", "1002"})
	if len(members) != 3 {
		t.Fatalf("members len = %d, want 3: %#v", len(members), members)
	}
	seen := make(map[string]bool, len(members))
	for _, member := range members {
		seen[member] = true
	}
	for _, want := range []string{"1001", "1002", "1003"} {
		if !seen[want] {
			t.Fatalf("missing member %s in %#v", want, members)
		}
	}
}

func TestUnreadCount(t *testing.T) {
	if got := unreadCount(10, 3); got != 7 {
		t.Fatalf("unreadCount = %d, want 7", got)
	}
	if got := unreadCount(3, 10); got != 0 {
		t.Fatalf("unreadCount over-read = %d, want 0", got)
	}
}

func TestCacheConversationStateDoesNotRegressLastSequence(t *testing.T) {
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	h := &LogicHandler{Rdb: rdb}
	newer := &api.WireMessage{
		SessionId: "c2c:1001:1002",
		Seq:       2,
		From:      "1002",
		To:        "1001",
		ToType:    "user",
		Body:      "newer",
		SentAt:    200,
	}
	older := &api.WireMessage{
		SessionId: newer.SessionId,
		Seq:       1,
		From:      "1001",
		To:        "1002",
		ToType:    "user",
		Body:      "older",
		SentAt:    100,
	}
	if err := h.cacheConversationState(ctx, newer, []string{"1001", "1002"}); err != nil {
		t.Fatalf("cache newer conversation state: %v", err)
	}
	if err := h.cacheConversationState(ctx, older, []string{"1001", "1002"}); err != nil {
		t.Fatalf("cache older conversation state: %v", err)
	}

	fields, err := rdb.HGetAll(ctx, server.ConversationLastKey(newer.SessionId)).Result()
	if err != nil {
		t.Fatalf("HGetAll conversation state: %v", err)
	}
	if fields["last_seq"] != "2" || fields["last_msg"] != "newer" || fields["sender_id"] != "1002" {
		t.Fatalf("conversation state regressed: %#v", fields)
	}
	for _, uid := range []string{"1001", "1002"} {
		score, err := rdb.ZScore(ctx, server.UserConversationsKey(uid), newer.SessionId).Result()
		if err != nil {
			t.Fatalf("ZScore(%s) error = %v", uid, err)
		}
		if score != 200 {
			t.Fatalf("conversation index score for %s = %v, want 200", uid, score)
		}
	}
}
