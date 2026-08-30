#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DATA="$(mktemp -d)"
trap 'rm -rf "$TEST_DATA"' EXIT
BATON="$REPO/bin/baton"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

GOCACHE="${GOCACHE:-$TEST_DATA/go-cache}" go build -o "$BATON" "$REPO/cmd/baton"
export BATON_DATA="$TEST_DATA/data"

"$BATON" init-config --quota true --context false --quota-threshold 92
"$BATON" disable quota >/dev/null
grep -Fq 'quota         disabled' <<<"$("$BATON" status)" || fail "baton disable quota failed"
"$BATON" enable quota >/dev/null
"$BATON" enable context >/dev/null
grep -Fq 'context       enabled' <<<"$("$BATON" status)" || fail "baton enable context failed"
"$BATON" disable context >/dev/null
passthrough="$(printf '%s' "printf 'kept\\n'" | base64 | tr '+/' '-_' | tr -d '=')"
status_output="$("$BATON" statusline --passthrough "$passthrough" < "$REPO/fixtures/usage/normal.json")"
[[ "$status_output" == "kept" ]] || fail "existing status line output was not preserved"
normal="$("$BATON" check --event post-tool --agent claude < "$REPO/fixtures/claude-post-tool.json")"
[[ -z "$normal" ]] || fail "healthy quota emitted hook output"

"$BATON" statusline < "$REPO/fixtures/usage/handoff.json"
post="$("$BATON" check --event post-tool --agent claude < "$REPO/fixtures/claude-post-tool.json")"
grep -Fq '"decision":"block"' <<<"$post" || fail "PostToolUse did not trigger"
grep -Fq 'PostToolUse' <<<"$post" || fail "PostToolUse schema missing"

duplicate="$("$BATON" check --event stop --agent claude < "$REPO/fixtures/claude-stop.json")"
[[ -z "$duplicate" ]] || fail "same quota window emitted duplicate output"

"$BATON" reset fixture-session >/dev/null
pre="$("$BATON" check --event pre-tool --agent claude < "$REPO/fixtures/claude-pre-tool.json")"
grep -Fq '"permissionDecision":"deny"' <<<"$pre" || fail "PreToolUse did not deny new work"

"$BATON" reset fixture-session >/dev/null
prompt="$("$BATON" check --event prompt --agent claude < "$REPO/fixtures/claude-prompt.json")"
grep -Fq 'UserPromptSubmit' <<<"$prompt" || fail "UserPromptSubmit did not inject handoff context"
enforced="$("$BATON" check --event pre-tool --agent claude < "$REPO/fixtures/claude-pre-tool.json")"
grep -Fq '"permissionDecision":"deny"' <<<"$enforced" || fail "PreToolUse did not enforce an earlier prompt handoff"

"$BATON" reset fixture-session >/dev/null
codex="$("$BATON" check --event stop --agent codex < "$REPO/fixtures/claude-stop.json")"
[[ -z "$codex" ]] || fail "Claude quota blocked Codex"

"$BATON" reset fixture-session >/dev/null
quota_stop="$("$BATON" check --event stop --agent claude < "$REPO/fixtures/claude-stop.json")"
grep -Fq '"hookEventName":"Stop"' <<<"$quota_stop" || fail "Stop quota fallback missing"

"$BATON" init-config --quota false --context true --quota-threshold 92
transcript="$TEST_DATA/transcript.jsonl"
printf '%s\n' '{"message":{"role":"assistant","usage":{"input_tokens":190000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}' > "$transcript"
printf '{"session_id":"context-session","transcript_path":"%s","cwd":"/tmp/example-project","hook_event_name":"Stop","stop_hook_active":false}\n' "$transcript" > "$TEST_DATA/stop.json"
context="$("$BATON" check --event stop --agent claude < "$TEST_DATA/stop.json")"
grep -Fq 'Trigger reason: context' <<<"$context" || fail "Stop context trigger missing"

"$BATON" disable context-session >/dev/null
disabled="$("$BATON" check --event stop --agent claude < "$TEST_DATA/stop.json")"
[[ -z "$disabled" ]] || fail "session disable did not silence context guard"

printf '{"session_id":"loop-session","transcript_path":"%s","cwd":"/tmp/example-project","hook_event_name":"Stop","stop_hook_active":true}\n' "$transcript" > "$TEST_DATA/stop-active.json"
loop="$("$BATON" check --event stop --agent claude < "$TEST_DATA/stop-active.json")"
[[ -z "$loop" ]] || fail "stop_hook_active loop protection failed"

echo "All hook process integration tests passed."
