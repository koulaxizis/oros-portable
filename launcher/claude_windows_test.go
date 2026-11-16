package main

import "testing"

func TestWingetVersion(t *testing.T) {
	out := "Name   Id               Version  Available Source\r\n" +
		"-----------------------------------------------\r\n" +
		"Claude Anthropic.Claude 0.14.10  0.14.11   winget\r\n"
	if v := wingetVersion(out, "Anthropic.Claude"); v != "0.14.10" {
		t.Fatalf("version %q", v)
	}
	if v := wingetVersion("No installed package found matching input criteria.", "Anthropic.Claude"); v != "" {
		t.Fatalf("none: %q", v)
	}
	for _, id := range []string{"AnthropicPBC.Claude_abc123!Claude", "Claude.Desktop"} {
		if !claudeAppID.MatchString(id) {
			t.Errorf("app id %q refused", id)
		}
	}
	for _, id := range []string{"", "a;calc", "x' & calc", "a b"} {
		if claudeAppID.MatchString(id) {
			t.Errorf("app id %q accepted", id)
		}
	}
}
