package main

import "testing"

func TestEnvOrDefault(t *testing.T) {
	const name = "STRIEM_TEST_SETTING"
	t.Setenv(name, "")
	if got := envOrDefault(name, "fallback"); got != "fallback" {
		t.Fatalf("empty environment value = %q, want fallback", got)
	}
	t.Setenv(name, "configured")
	if got := envOrDefault(name, "fallback"); got != "configured" {
		t.Fatalf("configured environment value = %q, want configured", got)
	}
}
