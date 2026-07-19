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

test_both_detected() {
  local home="$TEST_HOME/both"
  local tools="$home/tools"
  prepare_home "$home"
  make_tools "$tools"
  make_agents "$tools"

  run_installer "$home" "$tools" >/dev/null
  assert_claude_installed "$home"
  assert_codex_installed "$home"
  echo "PASS: both detected agents are configured"
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
  grep -Fq '[features] hooks = true' "$output" || fail "manual Codex hooks instruction missing"
  test ! -e "$home/.codex/config.toml" || fail "installer wrote Codex feature or trust configuration"
  if grep -qi 'trust' "$home/.codex/hooks.json"; then
    fail "installer wrote a Codex trust hash"
  fi
  echo "PASS: Codex directory detection prints manual hooks guidance without trust writes"
}

test_both_detected
test_claude_only
test_codex_only
test_neither_detected
test_second_install_is_idempotent
test_invalid_flags
test_help_has_no_side_effects
test_codex_directory_without_executable

PATH="$ORIGINAL_PATH"
echo "All installer integration tests passed."
