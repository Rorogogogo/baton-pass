package main

import (
	"strings"
	"testing"
)

func TestFilterBatonHooksPreservesOtherCommandsInMixedGroup(t *testing.T) {
	stop := []any{
		map[string]any{
			"matcher": "mixed-group",
			"timeout": float64(30),
			"hooks": []any{
				map[string]any{"type": "command", "command": `"/tmp/baton" check`},
				map[string]any{"type": "command", "command": "keep-me --flag", "timeout": float64(5)},
			},
		},
		map[string]any{
			"matcher": "baton-only",
			"hooks":   []any{map[string]any{"type": "command", "command": "/tmp/baton check"}},
		},
	}

	got, removed := filterBatonHooks(stop)
	if !removed {
		t.Fatal("filterBatonHooks reported no removal")
	}
	if len(got) != 1 {
		t.Fatalf("len(filtered Stop groups) = %d, want 1", len(got))
	}
	group, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("filtered group type = %T, want map[string]any", got[0])
	}
	if group["matcher"] != "mixed-group" || group["timeout"] != float64(30) {
		t.Fatalf("group metadata changed: %#v", group)
	}
	hooks, ok := group["hooks"].([]any)
	if !ok || len(hooks) != 1 {
		t.Fatalf("remaining hooks = %#v, want one unrelated hook", group["hooks"])
	}
	hook := hooks[0].(map[string]any)
	if hook["command"] != "keep-me --flag" || hook["timeout"] != float64(5) {
		t.Fatalf("unrelated hook changed: %#v", hook)
	}
}

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
