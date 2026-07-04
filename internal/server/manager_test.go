package server

import "testing"

func TestClientManagerRemoveOnlyMatchingSession(t *testing.T) {
	manager := &ClientManager{}
	first := &ClientConn{SessionID: "old"}
	second := &ClientConn{SessionID: "new"}

	manager.Add("1001", first)
	manager.Add("1001", second)
	manager.Remove("1001", first)

	got, ok := manager.GetConn("1001")
	if !ok {
		t.Fatal("expected active client connection")
	}
	if got != second {
		t.Fatalf("expected newest session to stay active, got %#v", got)
	}
}

func TestParseGatewayID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "session aware", value: "gateway-a|abc123", want: "gateway-a"},
		{name: "legacy", value: "gateway-b", want: "gateway-b"},
		{name: "empty", value: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseGatewayID(tt.value); got != tt.want {
				t.Fatalf("ParseGatewayID(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseRoute(t *testing.T) {
	gatewayID, sessionID := ParseRoute("gateway-a|conn-1")
	if gatewayID != "gateway-a" || sessionID != "conn-1" {
		t.Fatalf("ParseRoute = (%q, %q), want (gateway-a, conn-1)", gatewayID, sessionID)
	}

	gatewayID, sessionID = ParseRoute("gateway-b")
	if gatewayID != "gateway-b" || sessionID != "" {
		t.Fatalf("legacy ParseRoute = (%q, %q), want (gateway-b, empty)", gatewayID, sessionID)
	}
}

func TestSessionTimelineKey(t *testing.T) {
	if got := SessionTimelineKey("group:C"); got != "session_timeline:group:C" {
		t.Fatalf("SessionTimelineKey = %q", got)
	}
}

func TestConversationKeys(t *testing.T) {
	if got := UserConversationsKey("1001"); got != "user:conversations:1001" {
		t.Fatalf("UserConversationsKey = %q", got)
	}
	if got := ConversationMembersKey("c2c:1001:1002"); got != "conversation:members:c2c:1001:1002" {
		t.Fatalf("ConversationMembersKey = %q", got)
	}
	if got := ConversationLastKey("group:G1"); got != "conversation:last:group:G1" {
		t.Fatalf("ConversationLastKey = %q", got)
	}
	if got := UserConversationReadKey("1002"); got != "user:conversation:read:1002" {
		t.Fatalf("UserConversationReadKey = %q", got)
	}
}
