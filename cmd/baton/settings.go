package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var claudeHookEvents = []struct {
	Name  string
	Event string
}{
	{"UserPromptSubmit", "prompt"},
	{"PreToolUse", "pre-tool"},
	{"PostToolUse", "post-tool"},
	{"Stop", "stop"},
}

func hookSettings(action string, args []string) {
	if len(args) < 1 || args[0] == "" {
		fatalUsage(fmt.Sprintf("usage: baton %s <settings.json> [claude|codex]", action))
	}
	settingsPath := args[0]
	agent := ""
	if len(args) > 1 {
		agent = strings.ToLower(args[1])
	}
	if agent == "" {
		if filepath.Base(settingsPath) == "hooks.json" {
			agent = "codex"
		} else {
			agent = "claude"
		}
	}
	data := readSettings(settingsPath)
	events := claudeHookEvents
	if agent == "codex" {
		events = claudeHookEvents[len(claudeHookEvents)-1:]
	}
	self := executablePath()

	if action == "install-hook" {
		allPresent := true
		for _, event := range events {
			command := fmt.Sprintf("%q check --event %s --agent %s", self, event.Event, agent)
			if !exactHookPresent(eventHooks(data, event.Name), command) {
				allPresent = false
				break
			}
		}
		if allPresent {
			fmt.Println("  ✓ Baton hooks already registered")
			return
		}
		for _, event := range events {
			groups, _ := filterBatonHooks(eventHooks(data, event.Name))
			command := fmt.Sprintf("%q check --event %s --agent %s", self, event.Event, agent)
			groups = append(groups, map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command}},
			})
			setEventHooks(data, event.Name, groups)
		}
		backup(settingsPath)
		writeSettings(settingsPath, data)
		fmt.Printf("  ✓ Baton lifecycle hooks registered (backup: %s.bak)\n", filepath.Base(settingsPath))
		return
	}

	removed := false
	for _, event := range claudeHookEvents {
		kept, didRemove := filterBatonHooks(eventHooks(data, event.Name))
		if didRemove {
			removed = true
			setEventHooks(data, event.Name, kept)
		}
	}
	if !removed {
		fmt.Println("  ✓ no Baton hook entry found")
		return
	}
	backup(settingsPath)
	writeSettings(settingsPath, data)
	fmt.Printf("  ✓ removed Baton hooks (backup: %s.bak)\n", filepath.Base(settingsPath))
}

func statuslineSettings(action string, args []string) {
	if len(args) != 1 || args[0] == "" {
		fatalUsage(fmt.Sprintf("usage: baton %s <settings.json>", action))
	}
	path := args[0]
	data := readSettings(path)
	status, _ := data["statusLine"].(map[string]any)
	command, _ := status["command"].(string)
	if action == "install-statusline" {
		if isBatonStatusline(command) {
			fmt.Println("  ✓ Claude quota telemetry already registered")
			return
		}
		wrapped := fmt.Sprintf("%q statusline", executablePath())
		if command != "" {
			wrapped += " --passthrough " + base64.RawURLEncoding.EncodeToString([]byte(command))
		}
		updated := map[string]any{"type": "command", "command": wrapped}
		for key, value := range status {
			if key != "type" && key != "command" {
				updated[key] = value
			}
		}
		data["statusLine"] = updated
		backup(path)
		writeSettings(path, data)
		if command == "" {
			fmt.Println("  ✓ Claude quota telemetry registered")
		} else {
			fmt.Println("  ✓ Claude quota telemetry registered; existing status line preserved")
		}
		return
	}
	if !isBatonStatusline(command) {
		fmt.Println("  ✓ no Baton status line entry found")
		return
	}
	original := statuslinePassthrough(command)
	if original == "" {
		delete(data, "statusLine")
	} else {
		status["type"] = "command"
		status["command"] = original
		data["statusLine"] = status
	}
	backup(path)
	writeSettings(path, data)
	fmt.Println("  ✓ removed Claude quota telemetry; previous status line restored")
}

func executablePath() string {
	self, err := os.Executable()
	if err != nil {
		return filepath.Join(repoRoot(), "bin", "baton")
	}
	if resolved, e := filepath.EvalSymlinks(self); e == nil {
		self = resolved
	}
	return self
}

func readSettings(path string) map[string]any {
	var data map[string]any
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &data); err != nil {
			fatal("baton: %s is not valid JSON: %v", path, err)
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data
}

func eventHooks(data map[string]any, event string) []any {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	groups, _ := hooks[event].([]any)
	return groups
}

func setEventHooks(data map[string]any, event string, groups []any) {
	hooks, _ := data["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	if len(groups) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = groups
	}
	if len(hooks) == 0 {
		delete(data, "hooks")
	}
}

func stopHooks(data map[string]any) []any          { return eventHooks(data, "Stop") }
func setStopHooks(data map[string]any, stop []any) { setEventHooks(data, "Stop", stop) }

func exactHookPresent(groups []any, command string) bool {
	for _, groupValue := range groups {
		group, _ := groupValue.(map[string]any)
		hooks, _ := group["hooks"].([]any)
		for _, hookValue := range hooks {
			hook, _ := hookValue.(map[string]any)
			if hook["command"] == command {
				return true
			}
		}
	}
	return false
}

func hookPresent(groups []any) bool {
	for _, group := range groups {
		if groupHasHook(group) {
			return true
		}
	}
	return false
}

func groupHasHook(groupValue any) bool {
	group, _ := groupValue.(map[string]any)
	hooks, _ := group["hooks"].([]any)
	for _, hook := range hooks {
		if isBatonHook(hook) {
			return true
		}
	}
	return false
}

func filterBatonHooks(groups []any) ([]any, bool) {
	keptGroups := make([]any, 0, len(groups))
	removed := false
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			keptGroups = append(keptGroups, groupValue)
			continue
		}
		hooks, ok := group["hooks"].([]any)
		if !ok {
			keptGroups = append(keptGroups, groupValue)
			continue
		}
		keptHooks := make([]any, 0, len(hooks))
		changed := false
		for _, hook := range hooks {
			if isBatonHook(hook) {
				removed, changed = true, true
				continue
			}
			keptHooks = append(keptHooks, hook)
		}
		if !changed {
			keptGroups = append(keptGroups, groupValue)
			continue
		}
		if len(keptHooks) > 0 {
			copyGroup := make(map[string]any, len(group))
			for key, value := range group {
				copyGroup[key] = value
			}
			copyGroup["hooks"] = keptHooks
			keptGroups = append(keptGroups, copyGroup)
		}
	}
	return keptGroups, removed
}

func isBatonHook(hookValue any) bool {
	hook, _ := hookValue.(map[string]any)
	cmd, _ := hook["command"].(string)
	cmd = strings.ReplaceAll(cmd, `"`, "")
	return strings.Contains(cmd, "baton check") || strings.Contains(cmd, "/baton ") ||
		strings.Contains(cmd, "handoff_baton_check.py") || strings.Contains(cmd, "hb check") || strings.HasSuffix(cmd, "/hb")
}

func isBatonStatusline(command string) bool {
	clean := strings.ReplaceAll(command, `"`, "")
	return strings.Contains(clean, "baton statusline") || strings.Contains(clean, "/baton statusline")
}

func statuslinePassthrough(command string) string {
	fields := strings.Fields(command)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "--passthrough" {
			b, err := base64.RawURLEncoding.DecodeString(fields[i+1])
			if err == nil {
				return string(b)
			}
		}
	}
	return ""
}

func backup(path string) {
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", b, 0o644)
	}
}

func writeSettings(path string, data map[string]any) {
	if err := writeJSONAtomic(path, data); err != nil {
		fatal("baton: %v", err)
	}
}
