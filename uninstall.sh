#!/usr/bin/env bash
# Reverse install.sh: remove the selected agents' Stop hooks and skills, then
# remove shared command links when no baton-pass agent configuration remains.
#   ./uninstall.sh [--claude-only|--codex-only]
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
BIN_DIR="$HOME/.local/bin"
BATON="$REPO/bin/baton"
CLAUDE_SKILL="$CLAUDE_DIR/skills/baton-pass/SKILL.md"
CODEX_SKILL="$HOME/.agents/skills/baton-pass"
CLAUDE_HOOKS="$CLAUDE_DIR/settings.json"
CODEX_HOOKS="$CODEX_DIR/hooks.json"

usage() {
  cat <<'EOF'
Usage: ./uninstall.sh [--claude-only|--codex-only|--help]

With no flag, remove baton-pass from every detected Claude Code and Codex installation.
  --claude-only  remove baton-pass from Claude Code only
  --codex-only   remove baton-pass from Codex only
  --help         show this help
EOF
}

SELECT_CLAUDE=false
SELECT_CODEX=false

case "$#" in
  0)
    if command -v claude >/dev/null 2>&1 || [[ -d "$CLAUDE_DIR" ]]; then
      SELECT_CLAUDE=true
    fi
    if command -v codex >/dev/null 2>&1 || [[ -d "$CODEX_DIR" ]]; then
      SELECT_CODEX=true
    fi
    if [[ "$SELECT_CLAUDE" == false && "$SELECT_CODEX" == false ]]; then
      echo "ERROR: no Claude Code or Codex installation detected." >&2
      echo "Use --claude-only or --codex-only to remove an agent explicitly." >&2
      exit 1
    fi
    ;;
  1)
    case "$1" in
      --claude-only) SELECT_CLAUDE=true ;;
      --codex-only) SELECT_CODEX=true ;;
      --help)
        usage
        exit 0
        ;;
      *)
        echo "ERROR: unknown option: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
    ;;
  2)
    if [[ "$1" == "--claude-only" && "$2" == "--codex-only" ]] ||
       [[ "$1" == "--codex-only" && "$2" == "--claude-only" ]]; then
      echo "ERROR: --claude-only and --codex-only are mutually exclusive." >&2
    else
      echo "ERROR: expected at most one option." >&2
    fi
    usage >&2
    exit 1
    ;;
  *)
    echo "ERROR: expected at most one option." >&2
    usage >&2
    exit 1
    ;;
esac

link_targets() {
  local link="$1"
  local target="$2"
  [[ -L "$link" && "$(readlink "$link")" == "$target" ]]
}

remove_owned_link() {
  local link="$1"
  local target="$2"
  if link_targets "$link" "$target"; then
    rm -f "$link"
  fi
}

remove_hook() {
  local hook_file="$1"
  if [[ -f "$hook_file" && -x "$BATON" ]]; then
    "$BATON" uninstall-hook "$hook_file"
  elif [[ -f "$hook_file" ]]; then
    echo "  ! bin/baton not built — remove the Stop hook entry from $hook_file manually"
  fi
}

echo "Uninstalling baton-pass..."

if [[ "$SELECT_CLAUDE" == true ]]; then
  remove_owned_link "$CLAUDE_SKILL" "$REPO/SKILL.md"
  rmdir "$CLAUDE_DIR/skills/baton-pass" 2>/dev/null || true
  if [[ -f "$CLAUDE_HOOKS" && -x "$BATON" ]]; then
    "$BATON" uninstall-statusline "$CLAUDE_HOOKS"
  fi
  remove_hook "$CLAUDE_HOOKS"
  echo "  ✓ removed Claude skill, hooks, and quota telemetry"
fi

if [[ "$SELECT_CODEX" == true ]]; then
  remove_owned_link "$CODEX_SKILL" "$REPO"
  remove_hook "$CODEX_HOOKS"
  echo "  ✓ removed Codex skill and hooks"
fi

# The command links are shared by both agents. Leave them in place if an
# unselected agent (or a failed hook cleanup) still references this checkout.
BATON_CONFIG_REMAINS=false
if link_targets "$CLAUDE_SKILL" "$REPO/SKILL.md" ||
   link_targets "$CODEX_SKILL" "$REPO" ||
   { [[ -f "$CLAUDE_HOOKS" ]] && grep -Fq "$REPO/bin/baton" "$CLAUDE_HOOKS"; } ||
   { [[ -f "$CODEX_HOOKS" ]] && grep -Fq "$REPO/bin/baton" "$CODEX_HOOKS"; }; then
  BATON_CONFIG_REMAINS=true
fi

if [[ "$BATON_CONFIG_REMAINS" == false ]]; then
  remove_owned_link "$BIN_DIR/baton" "$BATON"
  remove_owned_link "$BIN_DIR/batonresume" "$REPO/bin/batonresume"
  echo "  ✓ removed shared command symlinks"
else
  echo "  • kept shared command symlinks for the remaining agent configuration"
fi

echo "Done. (Runtime data in handoffs/ and state/ left untouched.)"
