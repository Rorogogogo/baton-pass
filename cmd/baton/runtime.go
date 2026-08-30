package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type HandoffReason string

const (
	HandoffReasonContext HandoffReason = "context"
	HandoffReasonQuota   HandoffReason = "quota"
	HandoffReasonBoth    HandoffReason = "context+quota"
)

type hookInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	StopHookActive bool           `json:"stop_hook_active"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
}

type sessionState struct {
	ThresholdOverride     int    `json:"threshold_override,omitempty"`
	Disabled              bool   `json:"disabled,omitempty"`
	QuotaDisabled         bool   `json:"quota_disabled,omitempty"`
	QuotaContinueTurn     bool   `json:"quota_continue_turn,omitempty"`
	QuotaHandoffTriggered bool   `json:"quota_handoff_triggered,omitempty"`
	QuotaWindowReset      int64  `json:"quota_window_reset,omitempty"`
	QuotaLevelNotified    string `json:"quota_level_notified,omitempty"`
}

type GuardResult struct {
	Triggered    bool
	Reason       HandoffReason
	Level        string
	Message      string
	Instructions string
	Tokens       int
	Threshold    int
	UsagePercent float64
	ResetsAt     int64
}

func check(args []string) {
	event, agent := "", ""
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return
		}
		switch args[i] {
		case "--event":
			event = normalizeEvent(args[i+1])
		case "--agent":
			agent = strings.ToLower(args[i+1])
		default:
			return
		}
		i++
	}

	var in hookInput
	if json.NewDecoder(os.Stdin).Decode(&in) != nil {
		return
	}
	if event == "" {
		event = normalizeEvent(in.HookEventName)
	}
	if event == "" {
		event = "stop"
	}
	if agent == "" {
		agent = detectTool()
	}
	if event == "stop" && in.StopHookActive {
		return
	}

	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = "unknown"
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	project := filepath.Base(strings.TrimRight(cwd, "/"))
	if project == "" || project == "." || project == "/" {
		project = "default"
	}

	stateFile := filepath.Join(dataDir(), "state", sessionID+".json")
	st := readState(stateFile)
	if st.Disabled {
		return
	}
	stateChanged := false
	if event == "prompt" {
		if st.QuotaContinueTurn {
			st.QuotaHandoffTriggered = false
			stateChanged = true
		}
		st.QuotaContinueTurn = false
	}
	cfg := loadConfig()
	result, changed := evaluateGuards(cfg, &st, in, event, agent, time.Now())
	if changed || stateChanged {
		_ = writeState(stateFile, st)
	}
	if !result.Triggered {
		return
	}

	if result.Level != "aware" && result.Level != "caution" {
		root := repoRoot()
		result.Instructions = buildGuardInstructions(
			result, sessionID, filepath.Join(dataDir(), "handoffs", project),
			resolveCmd("baton", root), resolveCmd("batonresume", root), agent,
		)
	}
	out := buildEventOutput(event, agent, result)
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func normalizeEvent(event string) string {
	event = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(event), "_", "-"))
	switch event {
	case "userpromptsubmit", "user-prompt-submit", "prompt":
		return "prompt"
	case "pretooluse", "pre-tool-use", "pre-tool":
		return "pre-tool"
	case "posttooluse", "post-tool-use", "post-tool":
		return "post-tool"
	case "stop":
		return "stop"
	default:
		return ""
	}
}

func evaluateGuards(cfg Config, st *sessionState, in hookInput, event, agent string, now time.Time) (GuardResult, bool) {
	var result GuardResult
	changed := false

	contextTriggered := false
	if event == "stop" && cfg.Context.Enabled {
		threshold := effectiveContextThreshold(cfg)
		if st.ThresholdOverride > 0 {
			threshold = st.ThresholdOverride
		}
		if tokens, ok := readContextTokens(in.TranscriptPath); ok && tokens >= threshold {
			contextTriggered = true
			result.Tokens = tokens
			result.Threshold = threshold
		}
	}

	quotaAtHandoff := false
	quotaNewTrigger := false
	if agent == "claude" && cfg.Quota.Enabled && !st.QuotaDisabled {
		if usage, ok := readQuotaUsage(now, cfg.Quota.StaleAfterSeconds); ok {
			window := usage.Claude.FiveHour
			result.UsagePercent = window.UsedPercentage
			result.ResetsAt = window.ResetsAt
			if st.QuotaWindowReset != window.ResetsAt {
				st.QuotaWindowReset = window.ResetsAt
				st.QuotaHandoffTriggered = false
				st.QuotaLevelNotified = ""
				st.QuotaContinueTurn = false
				changed = true
			}
			level := quotaLevel(window.UsedPercentage, cfg.Quota)
			result.Level = level
			quotaAtHandoff = level == "handoff" || level == "emergency"
			if quotaAtHandoff && !st.QuotaContinueTurn && !st.QuotaHandoffTriggered {
				st.QuotaHandoffTriggered = true
				quotaNewTrigger = true
				changed = true
			} else if quotaAtHandoff && !st.QuotaContinueTurn && st.QuotaHandoffTriggered &&
				event == "pre-tool" && !isQuotaControlTool(in) {
				// Notices are deduplicated, but PreToolUse remains an enforcement
				// boundary if the model ignores the handoff prompt.
				quotaNewTrigger = true
			} else if event == "prompt" && (level == "aware" || level == "caution") &&
				quotaLevelRank(level) > quotaLevelRank(st.QuotaLevelNotified) {
				st.QuotaLevelNotified = level
				changed = true
				result.Triggered = true
				result.Reason = HandoffReasonQuota
				result.Message = quotaAdvisory(level)
				return result, changed
			}
		}
	}

	switch {
	case contextTriggered && quotaAtHandoff:
		result.Triggered = true
		result.Reason = HandoffReasonBoth
	case contextTriggered:
		result.Triggered = true
		result.Reason = HandoffReasonContext
	case quotaNewTrigger:
		result.Triggered = true
		result.Reason = HandoffReasonQuota
	default:
		return GuardResult{}, changed
	}
	result.Message = buildNotice(result)
	return result, changed
}

func isQuotaControlTool(in hookInput) bool {
	if in.ToolName == "AskUserQuestion" {
		return true
	}
	command, _ := in.ToolInput["command"].(string)
	return strings.Contains(command, "baton continue-quota") ||
		strings.Contains(command, "baton disable-session-quota") ||
		strings.Contains(command, "/baton continue-quota") ||
		strings.Contains(command, "/baton disable-session-quota")
}

func quotaLevel(used float64, cfg QuotaConfig) string {
	switch {
	case used >= cfg.EmergencyPercent:
		return "emergency"
	case used >= cfg.HandoffPercent:
		return "handoff"
	case used >= cfg.CautionPercent:
		return "caution"
	case used >= cfg.AwarePercent:
		return "aware"
	default:
		return "normal"
	}
}

func quotaLevelRank(level string) int {
	return map[string]int{"": 0, "normal": 0, "aware": 1, "caution": 2, "handoff": 3, "emergency": 4}[level]
}

func quotaAdvisory(level string) string {
	if level == "caution" {
		return "Baton quota caution: finish the current checkpoint before starting another large phase."
	}
	return "Baton quota-aware mode: keep this turn checkpoint-friendly and avoid combining large phases."
}

func buildNotice(result GuardResult) string {
	switch result.Reason {
	case HandoffReasonContext:
		return fmt.Sprintf("[baton-pass] Context %s ≥ %s — pick how to proceed.", commas(result.Tokens), commas(result.Threshold))
	case HandoffReasonBoth:
		return fmt.Sprintf("[baton-pass] Handoff recommended: context %s / %s; 5h quota %.1f%%.", commas(result.Tokens), commas(result.Threshold), result.UsagePercent)
	default:
		return fmt.Sprintf("[baton-pass] 5-hour quota reached %.1f%%. Reset: %s.", result.UsagePercent, time.Unix(result.ResetsAt, 0).Local().Format("3:04 PM"))
	}
}

func buildEventOutput(event, agent string, result GuardResult) map[string]any {
	if result.Message != "" && result.Instructions == "" {
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "UserPromptSubmit", "additionalContext": result.Message,
		}}
	}
	full := result.Message + "\n\n" + result.Instructions
	if agent == "codex" {
		return map[string]any{"decision": "block", "reason": full}
	}
	switch event {
	case "prompt":
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "UserPromptSubmit", "additionalContext": full,
		}}
	case "pre-tool":
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny",
			"permissionDecisionReason": full,
		}}
	case "post-tool":
		return map[string]any{
			"decision": "block", "reason": result.Message,
			"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": result.Instructions},
		}
	default:
		return buildHookOutput(agent, result.Message, result.Instructions)
	}
}

func buildHookOutput(tool, notice, instructions string) map[string]any {
	if tool == "codex" {
		return map[string]any{"decision": "block", "reason": notice + "\n\n" + instructions}
	}
	return map[string]any{
		"decision": "block", "reason": notice,
		"hookSpecificOutput": map[string]any{"hookEventName": "Stop", "additionalContext": instructions},
	}
}

func buildGuardInstructions(result GuardResult, sessionID, handoffDir, batonCmd, resumeCmd, tool string) string {
	choicePrompt := "Use the AskUserQuestion tool (the native option picker) to ask how to proceed. Never choose for the user."
	if tool == "codex" {
		choicePrompt = "Present these four options, ask the user how to proceed, and never choose for them."
	}
	if result.Reason == HandoffReasonContext {
		return fmt.Sprintf(
			"Trigger reason: context. Current agent: %s. %s\n\n"+
				"• Handoff now — run the `baton-pass` skill and write under %s/, then tell the user to resume with `%s %s`.\n"+
				"• Extend +10K — run `%s extend %s %d`, then continue.\n"+
				"• Disable here — run `%s disable %s`, then continue.\n"+
				"• Skip — continue; Baton may ask again next turn.",
			tool, choicePrompt, handoffDir, resumeCmd, tool, batonCmd, sessionID,
			result.Tokens+envInt("BATON_EXTEND_STEP", 10000), batonCmd, sessionID,
		)
	}

	reason := "quota"
	if result.Reason == HandoffReasonBoth {
		reason = "context+quota"
	}
	emergency := ""
	options := fmt.Sprintf(
		"• Handoff now — run `%s continue-quota %s`, then run the `baton-pass` skill and write under %s/; tell the user to resume with `%s %s`.\n"+
			"• Continue this turn — run `%s continue-quota %s`, then finish only the current checkpoint.\n"+
			"• Disable quota guard for this session — run `%s disable-session-quota %s`, then continue.\n"+
			"• Skip — do not start another large phase.",
		batonCmd, sessionID, handoffDir, resumeCmd, tool, batonCmd, sessionID, batonCmd, sessionID,
	)
	if result.Level == "emergency" {
		emergency = "Emergency quota handoff. Do not start tests, builds, sweeps, investigations, refactors, or new work. Only preserve current work, inspect cheap state needed for the handoff, write it, and stop.\n\n"
		options = fmt.Sprintf(
			"• Handoff now — run `%s continue-quota %s`, then run the `baton-pass` skill and write under %s/.\n"+
				"• Disable quota guard for this session — run `%s disable-session-quota %s`.",
			batonCmd, sessionID, handoffDir, batonCmd, sessionID,
		)
	}
	return fmt.Sprintf(
		"Trigger reason: %s. 5-hour usage: %.1f%%. Reset epoch: %d. Current agent: %s.\n%s%s\n\n%s",
		reason, result.UsagePercent, result.ResetsAt, tool, emergency, choicePrompt, options,
	)
}

// buildInstructions retains the original helper signature for compatibility
// with existing tests and callers.
func buildInstructions(sessionID, handoffDir, batonCmd, resumeCmd string, extendValue int, tool string) string {
	result := GuardResult{Reason: HandoffReasonContext, Tokens: extendValue - envInt("BATON_EXTEND_STEP", 10000)}
	return buildGuardInstructions(result, sessionID, handoffDir, batonCmd, resumeCmd, tool)
}

func state(action string, args []string) {
	if len(args) < 1 || args[0] == "" {
		fatalUsage("usage: baton {extend <session> <value>|disable <session>|reset <session>}")
	}
	file := filepath.Join(dataDir(), "state", args[0]+".json")
	st := readState(file)
	switch action {
	case "extend":
		if len(args) < 2 {
			fatalUsage("usage: baton extend <session> <value>")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			fatal("baton: invalid value %q", args[1])
		}
		st.ThresholdOverride = v
	case "disable":
		st.Disabled = true
	case "reset":
		st = sessionState{}
	}
	if err := writeState(file, st); err != nil {
		fatal("baton: %v", err)
	}
	fmt.Printf("baton-pass: %s ok (%s)\n", action, file)
}

func quotaSessionAction(action string, args []string) {
	if len(args) != 1 || args[0] == "" {
		fatalUsage("usage: baton {continue-quota|disable-session-quota} <session>")
	}
	file := filepath.Join(dataDir(), "state", args[0]+".json")
	st := readState(file)
	if action == "continue-quota" {
		st.QuotaContinueTurn = true
	} else {
		st.QuotaDisabled = true
	}
	if err := writeState(file, st); err != nil {
		fatal("baton: %v", err)
	}
	fmt.Printf("baton-pass: %s ok (%s)\n", action, file)
}

func readState(file string) sessionState {
	var st sessionState
	b, err := os.ReadFile(file)
	if err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func writeState(file string, st sessionState) error {
	return writeJSONAtomic(file, st)
}

// readContextTokens understands Claude Code JSONL and OpenAI Codex rollout JSONL.
func readContextTokens(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	type record struct {
		Message struct {
			Role  string `json:"role"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Info struct {
				LastTokenUsage *struct {
					InputTokens int `json:"input_tokens"`
				} `json:"last_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	last, found := 0, false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var rec record
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch {
		case rec.Message.Role == "assistant" && rec.Message.Usage != nil:
			u := rec.Message.Usage
			last = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			found = true
		case rec.Type == "event_msg" && rec.Payload.Type == "token_count" && rec.Payload.Info.LastTokenUsage != nil:
			last = rec.Payload.Info.LastTokenUsage.InputTokens
			found = true
		}
	}
	return last, found
}
