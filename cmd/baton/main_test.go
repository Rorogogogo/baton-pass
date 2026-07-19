package main

import (
	"strings"
	"testing"
)

func TestBuildHookOutputUsesCodexStopSchema(t *testing.T) {
	got := buildHookOutput("codex", "threshold reached", "present the options")

	if got["decision"] != "block" {
		t.Fatalf("decision = %v, want block", got["decision"])
	}
	if got["reason"] != "threshold reached\n\npresent the options" {
		t.Fatalf("reason = %q, want Codex continuation text", got["reason"])
	}
	if _, exists := got["hookSpecificOutput"]; exists {
		t.Fatal("Codex Stop output must not include hookSpecificOutput")
	}
}

func TestBuildInstructionsDoesNotRequireUnavailableCodexPicker(t *testing.T) {
	got := buildInstructions("session", "/tmp/handoffs", "baton", "batonresume", 10000, "codex")

	if strings.Contains(got, "AskUserQuestion") {
		t.Fatal("Codex instructions must not require AskUserQuestion, which is unavailable in Default mode")
	}
	if !strings.Contains(got, "Present these four options") {
		t.Fatalf("Codex instructions must tell the agent to present the choices, got %q", got)
	}
}
