package logic

import (
	"testing"

	"github.com/1084217636/linkgo-im/api"
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
