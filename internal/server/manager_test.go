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
