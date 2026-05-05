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
