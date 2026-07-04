package main

import "testing"

func TestGetEnv(t *testing.T) {
	t.Setenv("TRANSFER_TEST_KEY", "value")

	if got := getEnv("TRANSFER_TEST_KEY", "fallback"); got != "value" {
		t.Fatalf("getEnv existing = %q", got)
	}
	if got := getEnv("TRANSFER_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("getEnv missing = %q", got)
	}
}

func TestStageLabel(t *testing.T) {
	if got := stageLabel(false); got != "consume" {
		t.Fatalf("stageLabel(false) = %q", got)
	}
	if got := stageLabel(true); got != "retry_consume" {
		t.Fatalf("stageLabel(true) = %q", got)
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Setenv("TRANSFER_INT", "5")
	t.Setenv("TRANSFER_BAD_INT", "bad")

	if got := getEnvInt("TRANSFER_INT", 3); got != 5 {
		t.Fatalf("getEnvInt existing = %d, want 5", got)
	}
	if got := getEnvInt("TRANSFER_BAD_INT", 3); got != 3 {
		t.Fatalf("getEnvInt bad = %d, want fallback", got)
	}
	if got := getEnvInt("TRANSFER_MISSING_INT", 3); got != 3 {
		t.Fatalf("getEnvInt missing = %d, want fallback", got)
	}
}

func TestGroupRecipientDedupKey(t *testing.T) {
	if got := groupRecipientDedupKey("msg-1", "1002"); got != "group_delivery:msg-1:1002" {
		t.Fatalf("groupRecipientDedupKey = %q", got)
	}
}
