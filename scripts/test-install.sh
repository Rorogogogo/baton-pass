#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_HOME="$(mktemp -d)"
ORIGINAL_PATH="$PATH"
trap 'rm -rf "$TEST_HOME"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

make_tools() {
  local tools="$1"
  mkdir -p "$tools"
  ln -s "$(command -v go)" "$tools/go"
}

make_agents() {
  local tools="$1"
  cat > "$tools/claude" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat > "$tools/codex" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CODEX_LOG"
EOF
  chmod +x "$tools/claude" "$tools/codex"
}

prepare_home() {
  local home="$1"
  mkdir -p "$home/.claude" "$home/.codex"
  printf '{}\n' > "$home/.claude/settings.json"
  printf '{}\n' > "$home/.codex/hooks.json"
}

run_installer() {
  local home="$1"
  local tools="$2"
  shift 2
  HOME="$home" \
    CODEX_LOG="$home/codex.log" \
    PATH="$tools:/usr/bin:/bin:/usr/sbin:/sbin" \
    "$REPO/install.sh" "$@"
}

run_uninstaller() {
  local home="$1"
  local tools="$2"
  shift 2
  HOME="$home" \
    CODEX_LOG="$home/codex.log" \
    PATH="$tools:/usr/bin:/bin:/usr/sbin:/sbin" \
    "$REPO/uninstall.sh" "$@"
}

assert_claude_installed() {
  local home="$1"
  test -L "$home/.claude/skills/baton-pass/SKILL.md" || fail "Claude skill link missing"
  test "$(readlink "$home/.claude/skills/baton-pass/SKILL.md")" = "$REPO/SKILL.md" || fail "Claude skill link target is wrong"
  test -x "$home/.local/bin/baton" || fail "baton command missing"
  cp "$home/.claude/settings.json" "$home/claude-hook-before.json"
  "$REPO/bin/baton" install-hook "$home/.claude/settings.json" >/dev/null
  cmp -s "$home/claude-hook-before.json" "$home/.claude/settings.json" || fail "Claude Stop hook missing"
}

assert_codex_installed() {
  local home="$1"
  test -L "$home/.agents/skills/baton-pass" || fail "Codex skill directory link missing"
  test "$(readlink "$home/.agents/skills/baton-pass")" = "$REPO" || fail "Codex skill link target is wrong"
  cp "$home/.codex/hooks.json" "$home/codex-hook-before.json"
  "$REPO/bin/baton" install-hook "$home/.codex/hooks.json" >/dev/null
  cmp -s "$home/codex-hook-before.json" "$home/.codex/hooks.json" || fail "Codex Stop hook missing"
  test "$(cat "$home/codex.log")" = "features enable hooks" || fail "Codex hooks feature invocation is wrong"
}

test_both_commands_detected_without_config_dirs() {
  local home="$TEST_HOME/both-commands"
  local tools="$home/tools"
  mkdir -p "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" >/dev/null
  assert_claude_installed "$home"
  assert_codex_installed "$home"
  echo "PASS: command-only detection configures both agents and creates hook parents"
}

test_claude_only() {
  local home="$TEST_HOME/claude-only"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" --claude-only >/dev/null
  assert_claude_installed "$home"
  test ! -e "$home/.agents/skills/baton-pass" || fail "Codex skill installed in Claude-only mode"
  test ! -e "$home/codex.log" || fail "Codex CLI called in Claude-only mode"
  test "$(cat "$home/.codex/hooks.json")" = '{}' || fail "Codex hooks changed in Claude-only mode"
  echo "PASS: --claude-only configures only Claude"
}

test_codex_only() {
  local home="$TEST_HOME/codex-only"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" --codex-only >/dev/null
  assert_codex_installed "$home"
  test ! -e "$home/.claude/skills/baton-pass/SKILL.md" || fail "Claude skill installed in Codex-only mode"
  test "$(cat "$home/.claude/settings.json")" = '{}' || fail "Claude settings changed in Codex-only mode"
  echo "PASS: --codex-only configures only Codex"
}

test_neither_detected() {
  local home="$TEST_HOME/neither"
  local tools="$home/tools"
  local output="$home/output"
  mkdir -p "$home"
  make_tools "$tools"

  if run_installer "$home" "$tools" >"$output" 2>&1; then
    fail "installer succeeded with no detected agents"
  fi
  grep -q -- '--claude-only' "$output" || fail "missing actionable --claude-only hint"
  grep -q -- '--codex-only' "$output" || fail "missing actionable --codex-only hint"
  echo "PASS: no-agent failure includes explicit flag hints"
}

test_second_install_is_idempotent() {
  local home="$TEST_HOME/idempotent"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" >/dev/null
  cp "$home/.claude/settings.json" "$home/claude-first.json"
  cp "$home/.codex/hooks.json" "$home/codex-first.json"
  run_installer "$home" "$tools" >/dev/null
  cmp -s "$home/claude-first.json" "$home/.claude/settings.json" || fail "second install duplicated Claude Stop hook"
  cmp -s "$home/codex-first.json" "$home/.codex/hooks.json" || fail "second install duplicated Codex Stop hook"
  echo "PASS: second install does not duplicate Stop hooks"
}

test_invalid_flags() {
  local home="$TEST_HOME/invalid-flags"
  local tools="$home/tools"
  mkdir -p "$home"
  make_tools "$tools"

  if run_installer "$home" "$tools" --unknown >/dev/null 2>&1; then
    fail "installer accepted an unknown flag"
  fi
  if run_installer "$home" "$tools" --claude-only --codex-only >/dev/null 2>&1; then
    fail "installer accepted mutually exclusive flags"
  fi
  echo "PASS: invalid flag combinations are rejected"
}

test_help_has_no_side_effects() {
  local home="$TEST_HOME/help"
  local tools="$home/tools"
  local output="$home/output"
  mkdir -p "$home"
  make_tools "$tools"

  run_installer "$home" "$tools" --help >"$output"
  grep -q '^Usage:' "$output" || fail "--help did not print usage"
  test ! -e "$home/.local/bin" || fail "--help installed command links"
  test ! -e "$home/.claude" || fail "--help modified Claude configuration"
  test ! -e "$home/.codex" || fail "--help modified Codex configuration"
  test ! -e "$home/.agents" || fail "--help installed an agent skill"
  echo "PASS: --help prints usage without modifying files"
}

test_codex_directory_without_executable() {
  local home="$TEST_HOME/codex-directory"
  local tools="$home/tools"
  local output="$home/output"
  mkdir -p "$home/.codex"
  printf '{}\n' > "$home/.codex/hooks.json"
  make_tools "$tools"

  run_installer "$home" "$tools" >"$output"
  test -L "$home/.agents/skills/baton-pass" || fail "directory-detected Codex skill link missing"
  test "$(readlink "$home/.agents/skills/baton-pass")" = "$REPO" || fail "directory-detected Codex skill target is wrong"
  cp "$home/.codex/hooks.json" "$home/codex-directory-hook-before.json"
  "$REPO/bin/baton" install-hook "$home/.codex/hooks.json" >/dev/null
  cmp -s "$home/codex-directory-hook-before.json" "$home/.codex/hooks.json" || fail "directory-detected Codex Stop hook missing"
  grep -Fq "$home/.codex/config.toml" "$output" || fail "manual Codex config path missing"
  awk '/^\[features\]$/{getline; if ($0 == "hooks = true") found=1} END{exit !found}' "$output" || fail "manual Codex hooks TOML is not pasteable"
  test ! -e "$home/.codex/config.toml" || fail "installer wrote Codex feature or trust configuration"
  if grep -qi 'trust' "$home/.codex/hooks.json"; then
    fail "installer wrote a Codex trust hash"
  fi
  echo "PASS: Codex directory detection prints manual hooks guidance without trust writes"
}

test_codex_skill_conflict_has_no_side_effects() {
  local home="$TEST_HOME/codex-conflict"
  local tools="$home/tools"
  local output="$home/output"
  prepare_home "$home"
  mkdir -p "$home/.agents/skills/baton-pass"
  make_tools "$tools"
  make_agents "$tools"

  if run_installer "$home" "$tools" >"$output" 2>&1; then
    fail "installer succeeded despite a conflicting Codex skill directory"
  fi
  grep -Fq "$home/.agents/skills/baton-pass" "$output" || fail "Codex conflict error does not identify the destination"
  test ! -e "$home/.local/bin" || fail "Codex conflict created shared command links"
  test ! -e "$home/.claude/skills/baton-pass" || fail "Codex conflict installed the Claude skill"
  test "$(cat "$home/.claude/settings.json")" = '{}' || fail "Codex conflict changed Claude hooks"
  test "$(cat "$home/.codex/hooks.json")" = '{}' || fail "Codex conflict changed Codex hooks"
  test ! -e "$home/.claude/settings.json.bak" || fail "Codex conflict backed up Claude hooks"
  test ! -e "$home/.codex/hooks.json.bak" || fail "Codex conflict backed up Codex hooks"
  test ! -e "$home/codex.log" || fail "Codex conflict invoked the Codex CLI"
  echo "PASS: Codex skill conflict fails before all installation side effects"
}

test_uninstall_both_is_symmetric_and_idempotent() {
  local home="$TEST_HOME/uninstall-both"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"
  printf '%s\n' '{"hooks":{"Stop":[{"matcher":"keep","hooks":[{"type":"command","command":"keep-me --flag"}]}]}}' > "$home/.claude/settings.json"
  printf '%s\n' '{"hooks":{"Stop":[{"matcher":"keep","hooks":[{"type":"command","command":"keep-me --flag"}]}]}}' > "$home/.codex/hooks.json"
  find "$REPO/handoffs" "$REPO/state" -print | sort > "$home/runtime-paths-before"
  find "$REPO/handoffs" "$REPO/state" -type f -exec shasum {} \; | sort > "$home/runtime-content-before"

  run_installer "$home" "$tools" >/dev/null
  run_uninstaller "$home" "$tools" >/dev/null

  test ! -e "$home/.local/bin/baton" || fail "baton command remained after full uninstall"
  test ! -e "$home/.local/bin/batonresume" || fail "batonresume command remained after full uninstall"
  test ! -e "$home/.claude/skills/baton-pass/SKILL.md" || fail "Claude skill remained after full uninstall"
  test ! -e "$home/.agents/skills/baton-pass" || fail "Codex skill remained after full uninstall"
  ! grep -Fq "$REPO/bin/baton" "$home/.claude/settings.json" || fail "Claude baton Stop hook remained"
  ! grep -Fq "$REPO/bin/baton" "$home/.codex/hooks.json" || fail "Codex baton Stop hook remained"
  grep -Fq '"command": "keep-me --flag"' "$home/.claude/settings.json" || fail "unrelated Claude Stop hook changed"
  grep -Fq '"command": "keep-me --flag"' "$home/.codex/hooks.json" || fail "unrelated Codex Stop hook changed"
  find "$REPO/handoffs" "$REPO/state" -print | sort > "$home/runtime-paths-after"
  find "$REPO/handoffs" "$REPO/state" -type f -exec shasum {} \; | sort > "$home/runtime-content-after"
  cmp -s "$home/runtime-paths-before" "$home/runtime-paths-after" || fail "handoffs/ or state/ paths changed"
  cmp -s "$home/runtime-content-before" "$home/runtime-content-after" || fail "handoffs/ or state/ contents changed"

  run_uninstaller "$home" "$tools" >/dev/null
  test ! -e "$home/.local/bin/baton" || fail "second uninstall recreated baton"
  test ! -e "$home/.agents/skills/baton-pass" || fail "second uninstall recreated Codex skill"
  echo "PASS: full uninstall is symmetric, preserves unrelated data, and is idempotent"
}

test_single_agent_uninstall_preserves_shared_commands() {
  local home="$TEST_HOME/uninstall-single"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" >/dev/null
  run_uninstaller "$home" "$tools" --claude-only >/dev/null
  test ! -e "$home/.claude/skills/baton-pass/SKILL.md" || fail "Claude skill remained after Claude-only uninstall"
  test -L "$home/.agents/skills/baton-pass" || fail "Claude-only uninstall removed Codex skill"
  ! grep -Fq "$REPO/bin/baton" "$home/.claude/settings.json" || fail "Claude hook remained after Claude-only uninstall"
  grep -Fq "$REPO/bin/baton" "$home/.codex/hooks.json" || fail "Claude-only uninstall removed Codex hook"
  test -L "$home/.local/bin/baton" || fail "Claude-only uninstall removed shared baton command"
  test -L "$home/.local/bin/batonresume" || fail "Claude-only uninstall removed shared batonresume command"

  run_uninstaller "$home" "$tools" --codex-only >/dev/null
  test ! -e "$home/.agents/skills/baton-pass" || fail "Codex skill remained after Codex-only uninstall"
  ! grep -Fq "$REPO/bin/baton" "$home/.codex/hooks.json" || fail "Codex hook remained after Codex-only uninstall"
  test ! -e "$home/.local/bin/baton" || fail "shared baton command remained after final agent uninstall"
  test ! -e "$home/.local/bin/batonresume" || fail "shared batonresume command remained after final agent uninstall"
  echo "PASS: single-agent uninstall preserves shared commands until the final agent is removed"
}

test_uninstall_help_and_invalid_flags() {
  local home="$TEST_HOME/uninstall-options"
  local tools="$home/tools"
  local output="$home/output"
  mkdir -p "$home"
  make_tools "$tools"

  run_uninstaller "$home" "$tools" --help > "$output"
  grep -q '^Usage:' "$output" || fail "uninstall --help did not print usage"
  test ! -e "$home/.local/bin" || fail "uninstall --help modified command links"
  test ! -e "$home/.claude" || fail "uninstall --help modified Claude configuration"
  test ! -e "$home/.codex" || fail "uninstall --help modified Codex configuration"
  if run_uninstaller "$home" "$tools" --unknown >/dev/null 2>&1; then
    fail "uninstaller accepted an unknown flag"
  fi
  if run_uninstaller "$home" "$tools" --claude-only --codex-only >/dev/null 2>&1; then
    fail "uninstaller accepted mutually exclusive flags"
  fi
  echo "PASS: uninstall help has no side effects and invalid flags are rejected"
}

test_both_commands_detected_without_config_dirs
test_claude_only
test_codex_only
test_neither_detected
test_second_install_is_idempotent
test_invalid_flags
test_help_has_no_side_effects
test_codex_skill_conflict_has_no_side_effects
test_codex_directory_without_executable
test_uninstall_both_is_symmetric_and_idempotent
test_single_agent_uninstall_preserves_shared_commands
test_uninstall_help_and_invalid_flags

PATH="$ORIGINAL_PATH"
echo "All installer integration tests passed."
