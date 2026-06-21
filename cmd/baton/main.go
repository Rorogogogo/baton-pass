// Command baton is the baton-pass runtime: a single dependency-free binary that
// replaces the former python3 hook and shell+python state helper.
//
// Subcommands:
//
//	baton check                      Stop hook — reads the hook payload on stdin.
//	baton extend  <session> <value>  Raise a session's threshold.
//	baton disable <session>          Silence baton-pass for a session.
//	baton reset   <session>          Clear a session's state.
//	baton install-hook   <settings>  Register the Stop hook in a settings.json.
//	baton uninstall-hook <settings>  Remove the Stop hook from a settings.json.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func baseThreshold() int { return envInt("BATON_THRESHOLD", 190000) }
func extendStep() int    { return envInt("BATON_EXTEND_STEP", 10000) }

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "check":
		check()
	case "extend", "disable", "reset":
		state(cmd, args[1:])
	case "install-hook", "uninstall-hook":
		hookSettings(cmd, args[1:])
	case "", "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "baton: unknown command %q\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `baton-pass runtime

usage:
  baton check                       run the Stop hook (reads JSON payload on stdin)
  baton extend  <session> <value>   raise a session's threshold
  baton disable <session>           silence baton-pass for a session
  baton reset   <session>           clear a session's state
  baton install-hook   <settings>   register the Stop hook in settings.json
  baton uninstall-hook <settings>   remove the Stop hook from settings.json
`)
}

// ---------------------------------------------------------------------------
// check: the Stop hook
// ---------------------------------------------------------------------------

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

type sessionState struct {
	ThresholdOverride int  `json:"threshold_override,omitempty"`
	Disabled          bool `json:"disabled,omitempty"`
}

func check() {
	var in hookInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		os.Exit(0)
	}
	// Avoid an infinite block loop: if we're already inside a stop-hook
	// continuation, don't block again.
	if in.StopHookActive {
		os.Exit(0)
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

	repoRoot := repoRoot()
	dataDir := repoRoot
	if d := os.Getenv("BATON_DATA"); d != "" {
		dataDir = d
	}

	st := readState(filepath.Join(dataDir, "state", sessionID+".json"))
	if st.Disabled {
		os.Exit(0)
	}

	threshold := baseThreshold()
	if st.ThresholdOverride > 0 {
		threshold = st.ThresholdOverride
	}

	tokens, ok := readContextTokens(in.TranscriptPath)
	if !ok || tokens < threshold {
		os.Exit(0)
	}

	instructions := buildInstructions(
		sessionID,
		filepath.Join(dataDir, "handoffs", project),
		resolveCmd("baton", repoRoot),
		resolveCmd("batonresume", repoRoot),
		tokens+extendStep(),
		detectTool(),
	)
	notice := fmt.Sprintf("[baton-pass] Context %s ≥ %s — pick how to proceed.",
		commas(tokens), commas(threshold))

	out := map[string]any{
		"decision": "block",
		"reason":   notice,
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "Stop",
			"additionalContext": instructions,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(out)
}

// readContextTokens returns the context size used on the last assistant turn.
// It understands two transcript formats — Claude Code's JSONL and OpenAI Codex's
// rollout JSONL — so the same hook fires under either agent.
func readContextTokens(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	// record captures both transcript shapes; only the fields present on a given
	// line unmarshal, so we sniff the format per line by which branch is populated.
	type record struct {
		// Claude Code / Cursor: one object per message, usage on assistant turns.
		// input_tokens is the *non-cached* portion, so we add the cache fields.
		Message struct {
			Role  string `json:"role"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`

		// Codex rollout: token_count events. info.last_token_usage.input_tokens is
		// the most recent request's full prompt (it *includes* cached_input_tokens),
		// i.e. the live context-window occupancy — so we use it as-is.
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
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
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

func buildInstructions(sessionID, handoffDir, batonCmd, resumeCmd string, extendValue int, tool string) string {
	return fmt.Sprintf(
		"The baton-pass context threshold was reached (current agent: %s). "+
			"Use the AskUserQuestion tool (the native option picker) to ask how to proceed "+
			"— do NOT ask in plain text. Offer these four options and act on the choice:\n\n"+
			"• Handoff now — run the `baton-pass` skill to write the handoff doc under "+
			"%s/, then tell the user to exit and resume with `%s %s`.\n"+
			"• Extend +10K — run `%s extend %s %d`, then continue.\n"+
			"• Disable here — run `%s disable %s`, then continue.\n"+
			"• Skip — continue; you'll be asked again next turn.",
		tool, handoffDir, resumeCmd, tool, batonCmd, sessionID, extendValue, batonCmd, sessionID,
	)
}

func detectTool() string {
	if forced := strings.ToLower(strings.TrimSpace(os.Getenv("BATON_TOOL"))); forced != "" {
		return forced
	}
	agent := strings.ToLower(os.Getenv("AI_AGENT"))
	if strings.Contains(agent, "codex") || anyEnvHasPrefix("CODEX") {
		return "codex"
	}
	if strings.Contains(agent, "claude") || os.Getenv("CLAUDECODE") != "" {
		return "claude"
	}
	return "claude"
}

func anyEnvHasPrefix(prefix string) bool {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

// resolveCmd prefers the bare command name when it's installed on PATH (or in
// ~/.local/bin); otherwise falls back to the absolute path in the repo's bin/.
func resolveCmd(name, repoRoot string) string {
	if _, err := exec.LookPath(name); err == nil {
		return name
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); err == nil {
			return name
		}
	}
	return filepath.Join(repoRoot, "bin", name)
}

// ---------------------------------------------------------------------------
// state: extend / disable / reset
// ---------------------------------------------------------------------------

func state(action string, args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: baton {extend <session> <value>|disable <session>|reset <session>}")
		os.Exit(1)
	}
	session := args[0]

	dataDir := repoRoot()
	if d := os.Getenv("BATON_DATA"); d != "" {
		dataDir = d
	}
	stateDir := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "baton: %v\n", err)
		os.Exit(1)
	}
	file := filepath.Join(stateDir, session+".json")
	st := readState(file)

	switch action {
	case "extend":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: baton extend <session> <value>")
			os.Exit(1)
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "baton: invalid value %q\n", args[1])
			os.Exit(1)
		}
		st.ThresholdOverride = v
	case "disable":
		st.Disabled = true
	case "reset":
		st = sessionState{}
	}

	if err := writeState(file, st); err != nil {
		fmt.Fprintf(os.Stderr, "baton: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("baton-pass: %s ok (%s)\n", action, file)
}

func readState(file string) sessionState {
	var st sessionState
	b, err := os.ReadFile(file)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

func writeState(file string, st sessionState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644)
}

// ---------------------------------------------------------------------------
// install-hook / uninstall-hook: edit a Claude settings.json in place
// ---------------------------------------------------------------------------

func hookSettings(action string, args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintf(os.Stderr, "usage: baton %s <settings.json>\n", action)
		os.Exit(1)
	}
	settingsPath := args[0]

	self, err := os.Executable()
	if err == nil {
		if resolved, e := filepath.EvalSymlinks(self); e == nil {
			self = resolved
		}
	}
	command := fmt.Sprintf("%s check", self)

	var data map[string]any
	if b, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(b, &data); err != nil {
			fmt.Fprintf(os.Stderr, "baton: %s is not valid JSON: %v\n", settingsPath, err)
			os.Exit(1)
		}
	}
	if data == nil {
		data = map[string]any{}
	}

	stop := stopHooks(data)
	switch action {
	case "install-hook":
		if hookPresent(stop) {
			fmt.Println("  ✓ Stop hook already registered")
			return
		}
		entry := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command}},
		}
		stop = append(stop, entry)
		setStopHooks(data, stop)
		backup(settingsPath)
		writeSettings(settingsPath, data)
		fmt.Printf("  ✓ Stop hook registered (backup: %s.bak)\n", filepath.Base(settingsPath))
	case "uninstall-hook":
		kept := make([]any, 0, len(stop))
		for _, grp := range stop {
			if !groupHasHook(grp) {
				kept = append(kept, grp)
			}
		}
		if len(kept) == len(stop) {
			fmt.Println("  ✓ no Stop hook entry found")
			return
		}
		setStopHooks(data, kept)
		backup(settingsPath)
		writeSettings(settingsPath, data)
		fmt.Printf("  ✓ removed Stop hook (backup: %s.bak)\n", filepath.Base(settingsPath))
	}
}

func stopHooks(data map[string]any) []any {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	stop, _ := hooks["Stop"].([]any)
	return stop
}

func setStopHooks(data map[string]any, stop []any) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	hooks["Stop"] = stop
}

// hookPresent reports whether any registered command references our binary.
func hookPresent(stop []any) bool {
	for _, grp := range stop {
		if groupHasHook(grp) {
			return true
		}
	}
	return false
}

func groupHasHook(grp any) bool {
	g, _ := grp.(map[string]any)
	if g == nil {
		return false
	}
	hooks, _ := g["hooks"].([]any)
	for _, h := range hooks {
		hm, _ := h.(map[string]any)
		if hm == nil {
			continue
		}
		cmd, _ := hm["command"].(string)
		// Match the new Go binary (`baton check` / …/baton) plus legacy installs
		// (the old `hb` binary and the original python hook) so uninstall migrates.
		if strings.Contains(cmd, "baton check") ||
			strings.HasSuffix(cmd, "/baton") ||
			strings.Contains(cmd, "handoff_baton_check.py") ||
			strings.Contains(cmd, "hb check") ||
			strings.HasSuffix(cmd, "/hb") {
			return true
		}
	}
	return false
}

func backup(path string) {
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", b, 0o644)
	}
}

func writeSettings(path string, data map[string]any) {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "baton: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "baton: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// repoRoot resolves the repository root from the running binary's real path,
// following symlinks (e.g. ~/.local/bin/baton → <repo>/bin/baton).
func repoRoot() string {
	self, err := os.Executable()
	if err != nil {
		if wd, e := os.Getwd(); e == nil {
			return wd
		}
		return "."
	}
	if resolved, e := filepath.EvalSymlinks(self); e == nil {
		self = resolved
	}
	return filepath.Dir(filepath.Dir(self)) // <repo>/bin/baton → <repo>
}

// commas formats n with thousands separators (e.g. 190000 → "190,000").
func commas(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
