package main

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("HOUSED_TEST_SET", "from-env")
	t.Setenv("HOUSED_TEST_EMPTY", "")
	if got := envOr("HOUSED_TEST_SET", "default"); got != "from-env" {
		t.Errorf("set: %q", got)
	}
	if got := envOr("HOUSED_TEST_EMPTY", "default"); got != "default" {
		t.Errorf("empty: %q, want the default", got)
	}
	if got := envOr("HOUSED_TEST_NEVER_SET", "default"); got != "default" {
		t.Errorf("unset: %q, want the default", got)
	}
}
