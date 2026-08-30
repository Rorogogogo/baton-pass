// Command baton is the dependency-free baton-pass runtime.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "check":
		check(args[1:])
	case "extend", "reset":
		state(cmd, args[1:])
	case "disable":
		if len(args) > 1 && (args[1] == "context" || args[1] == "quota") {
			setGuard(args[1], false)
		} else {
			state(cmd, args[1:])
		}
	case "enable":
		if len(args) != 2 {
			fatalUsage("usage: baton enable {context|quota}")
		}
		setGuard(args[1], true)
	case "disable-session-quota", "continue-quota":
		quotaSessionAction(cmd, args[1:])
	case "status":
		showStatus()
	case "is-enabled":
		if len(args) != 2 {
			os.Exit(1)
		}
		cfg := loadConfig()
		if (args[1] == "quota" && cfg.Quota.Enabled) || (args[1] == "context" && cfg.Context.Enabled) {
			return
		}
		os.Exit(1)
	case "config":
		configure(args[1:])
	case "init-config":
		initConfig(args[1:])
	case "statusline":
		statusline(args[1:])
	case "install-hook", "uninstall-hook":
		hookSettings(cmd, args[1:])
	case "install-statusline", "uninstall-statusline":
		statuslineSettings(cmd, args[1:])
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
  baton check [--event EVENT] [--agent AGENT]  run a lifecycle hook (JSON on stdin)
  baton status                                show guards and Claude quota
  baton enable  {context|quota}               enable an automatic guard
  baton disable {context|quota}               disable an automatic guard
  baton config [context TOKENS|quota PERCENT] configure guards
  baton extend  <session> <value>             raise a session's context threshold
  baton disable <session>                     silence all guards for a session
  baton reset   <session>                     clear a session's state
  baton install-hook <settings> [agent]       register lifecycle hooks
  baton uninstall-hook <settings> [agent]     remove lifecycle hooks
`)
}

func fatalUsage(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func dataDir() string {
	if d := os.Getenv("BATON_DATA"); d != "" {
		return d
	}
	return repoRoot()
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

func resolveCmd(name, root string) string {
	if _, err := exec.LookPath(name); err == nil {
		return name
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); err == nil {
			return name
		}
	}
	return filepath.Join(root, "bin", name)
}

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
	return filepath.Dir(filepath.Dir(self))
}

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
