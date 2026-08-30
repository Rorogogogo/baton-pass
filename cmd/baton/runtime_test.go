package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := legacyConfig()
	cfg.Context.Enabled = false
	cfg.Quota.Enabled = true
	return cfg
}

func writeTestUsage(t *testing.T, now time.Time, used float64, reset, updated int64) {
	t.Helper()
	usage := QuotaUsage{Claude: ClaudeQuotaUsage{FiveHour: UsageWindow{
		UsedPercentage: used, ResetsAt: reset, UpdatedAt: updated,
	}}}
	if err := writeJSONAtomic(filepath.Join(dataDir(), "usage.json"), usage); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaLevelBoundaries(t *testing.T) {
	cfg := testConfig().Quota
	cases := []struct {
		used  float64
		level string
	}{
		{74, "normal"}, {75, "aware"}, {84, "aware"}, {85, "caution"},
		{91, "caution"}, {92, "handoff"}, {95, "handoff"},
		{96, "emergency"}, {100, "emergency"},
	}
	for _, tc := range cases {
		if got := quotaLevel(tc.used, cfg); got != tc.level {
			t.Errorf("quotaLevel(%v) = %q, want %q", tc.used, got, tc.level)
		}
	}
}

func TestParseQuotaUsageValidatesRequiredFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	valid := `{"rate_limits":{"five_hour":{"used_percentage":87.4,"resets_at":1700003600},"seven_day":{"used_percentage":31,"resets_at":1700600000}}}`
	usage, ok := parseQuotaUsage([]byte(valid), now)
	if !ok || usage.Claude.FiveHour.UsedPercentage != 87.4 || usage.Claude.FiveHour.UpdatedAt != now.Unix() {
		t.Fatalf("valid usage rejected or changed: %#v, %v", usage, ok)
	}
	for _, input := range []string{
		`{}`, `{"rate_limits":{"five_hour":{"used_percentage":101,"resets_at":1}}}`,
		`{"rate_limits":{"five_hour":{"used_percentage":50,"resets_at":0}}}`, `{bad`,
	} {
		if _, ok := parseQuotaUsage([]byte(input), now); ok {
			t.Errorf("invalid usage accepted: %s", input)
		}
	}
}

func TestReadQuotaUsageFailsOpenForMissingMalformedStaleAndExpired(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	now := time.Unix(1_700_000_000, 0)
	if _, ok := readQuotaUsage(now, 1800); ok {
		t.Fatal("missing usage must be unavailable")
	}
	if err := os.WriteFile(filepath.Join(dataDir(), "usage.json"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readQuotaUsage(now, 1800); ok {
		t.Fatal("malformed usage must be unavailable")
	}
	writeTestUsage(t, now, 93, now.Add(time.Hour).Unix(), now.Add(-time.Hour).Unix())
	if _, ok := readQuotaUsage(now, 1800); ok {
		t.Fatal("stale usage must be unavailable")
	}
	writeTestUsage(t, now, 93, now.Add(-time.Second).Unix(), now.Unix())
	if _, ok := readQuotaUsage(now, 1800); ok {
		t.Fatal("expired usage must be unavailable")
	}
}

func TestQuotaGuardDisabledMissingAndCodexAreSilent(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	now := time.Unix(1_700_000_000, 0)
	cfg := testConfig()
	st := sessionState{}
	cfg.Quota.Enabled = false
	if got, _ := evaluateGuards(cfg, &st, hookInput{}, "post-tool", "claude", now); got.Triggered {
		t.Fatal("disabled quota guard triggered")
	}
	cfg.Quota.Enabled = true
	if got, _ := evaluateGuards(cfg, &st, hookInput{}, "post-tool", "claude", now); got.Triggered {
		t.Fatal("missing quota usage triggered")
	}
	writeTestUsage(t, now, 99, now.Add(time.Hour).Unix(), now.Unix())
	if got, _ := evaluateGuards(cfg, &st, hookInput{}, "post-tool", "codex", now); got.Triggered {
		t.Fatal("Claude quota blocked Codex")
	}
}

func TestQuotaHandoffDeduplicatesAndRearmsForNewWindow(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	now := time.Unix(1_700_000_000, 0)
	cfg := testConfig()
	st := sessionState{}
	windowA := now.Add(time.Hour).Unix()
	writeTestUsage(t, now, 92, windowA, now.Unix())
	first, _ := evaluateGuards(cfg, &st, hookInput{}, "post-tool", "claude", now)
	if !first.Triggered || first.Reason != HandoffReasonQuota || !st.QuotaHandoffTriggered {
		t.Fatalf("first handoff did not trigger: %#v %#v", first, st)
	}
	second, _ := evaluateGuards(cfg, &st, hookInput{}, "stop", "claude", now)
	if second.Triggered {
		t.Fatal("same quota window emitted a duplicate handoff")
	}
	windowB := now.Add(6 * time.Hour).Unix()
	writeTestUsage(t, now, 96, windowB, now.Unix())
	rearmed, _ := evaluateGuards(cfg, &st, hookInput{}, "pre-tool", "claude", now)
	if !rearmed.Triggered || rearmed.Level != "emergency" || st.QuotaWindowReset != windowB {
		t.Fatalf("new window did not rearm: %#v %#v", rearmed, st)
	}
}

func TestPromptAdvisoryIsOneTimePerLevel(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	now := time.Unix(1_700_000_000, 0)
	cfg := testConfig()
	st := sessionState{}
	writeTestUsage(t, now, 75, now.Add(time.Hour).Unix(), now.Unix())
	first, _ := evaluateGuards(cfg, &st, hookInput{}, "prompt", "claude", now)
	if !first.Triggered || !strings.Contains(first.Message, "quota-aware") {
		t.Fatalf("aware advisory missing: %#v", first)
	}
	second, _ := evaluateGuards(cfg, &st, hookInput{}, "prompt", "claude", now)
	if second.Triggered {
		t.Fatal("aware advisory repeated")
	}
	writeTestUsage(t, now, 85, now.Add(time.Hour).Unix(), now.Unix())
	caution, _ := evaluateGuards(cfg, &st, hookInput{}, "prompt", "claude", now)
	if !caution.Triggered || !strings.Contains(caution.Message, "caution") {
		t.Fatalf("caution escalation missing: %#v", caution)
	}
}

func TestContextGuardDisabledBelowAtAboveAndOverride(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeTranscript := func(tokens int) {
		line := map[string]any{"message": map[string]any{
			"role": "assistant", "usage": map[string]any{"input_tokens": tokens},
		}}
		b, _ := json.Marshal(line)
		if err := os.WriteFile(transcript, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := legacyConfig()
	cfg.Quota.Enabled = false
	in := hookInput{TranscriptPath: transcript}
	st := sessionState{}
	cfg.Context.Enabled = false
	writeTranscript(defaultContextThreshold + 1)
	if got, _ := evaluateGuards(cfg, &st, in, "stop", "claude", time.Now()); got.Triggered {
		t.Fatal("disabled context guard triggered")
	}
	cfg.Context.Enabled = true
	for _, tc := range []struct {
		tokens  int
		trigger bool
	}{{defaultContextThreshold - 1, false}, {defaultContextThreshold, true}, {defaultContextThreshold + 1, true}} {
		writeTranscript(tc.tokens)
		got, _ := evaluateGuards(cfg, &st, in, "stop", "claude", time.Now())
		if got.Triggered != tc.trigger {
			t.Errorf("tokens %d triggered=%v, want %v", tc.tokens, got.Triggered, tc.trigger)
		}
	}
	st.ThresholdOverride = defaultContextThreshold + 10_000
	writeTranscript(defaultContextThreshold + 1)
	if got, _ := evaluateGuards(cfg, &st, in, "stop", "claude", time.Now()); got.Triggered {
		t.Fatal("threshold override was ignored")
	}
}

func TestCombinedTriggersProduceOneEvent(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	now := time.Unix(1_700_000_000, 0)
	writeTestUsage(t, now, 93, now.Add(time.Hour).Unix(), now.Unix())
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	line := `{"message":{"role":"assistant","usage":{"input_tokens":190000}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Context.Enabled = true
	st := sessionState{}
	got, _ := evaluateGuards(cfg, &st, hookInput{TranscriptPath: transcript}, "stop", "claude", now)
	if !got.Triggered || got.Reason != HandoffReasonBoth {
		t.Fatalf("combined result = %#v", got)
	}
}

func TestEventOutputsUseClaudeSchemas(t *testing.T) {
	result := GuardResult{Message: "notice", Instructions: "handoff"}
	pre := buildEventOutput("pre-tool", "claude", result)
	preSpecific := pre["hookSpecificOutput"].(map[string]any)
	if preSpecific["permissionDecision"] != "deny" || preSpecific["hookEventName"] != "PreToolUse" {
		t.Fatalf("bad PreToolUse output: %#v", pre)
	}
	post := buildEventOutput("post-tool", "claude", result)
	if post["decision"] != "block" {
		t.Fatalf("bad PostToolUse output: %#v", post)
	}
	prompt := buildEventOutput("prompt", "claude", result)
	if _, blocked := prompt["decision"]; blocked {
		t.Fatalf("prompt handoff must be injected, not erase the user's prompt: %#v", prompt)
	}
}

func TestLegacyConfigMigrationDefaults(t *testing.T) {
	t.Setenv("BATON_DATA", t.TempDir())
	cfg := loadConfig()
	if !cfg.Context.Enabled || cfg.Quota.Enabled {
		t.Fatalf("migration defaults changed: %#v", cfg)
	}
}
