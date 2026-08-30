#!/usr/bin/env bash
# One-command installer for baton-pass.
#   ./install.sh [--claude-only|--codex-only] [guard options]
# No runtime dependencies: ships a single Go binary (`baton`). If a Go toolchain
# is present it builds from source; otherwise it downloads the matching prebuilt
# binary from GitHub Releases. Idempotent. Reverse with ./uninstall.sh.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
BIN_DIR="$HOME/.local/bin"
SLUG="Rorogogogo/baton-pass"
BATON="$REPO/bin/baton"
CODEX_SKILL="$HOME/.agents/skills/baton-pass"
DATA_DIR="${BATON_DATA:-$REPO}"

usage() {
  cat <<'EOF'
Usage: ./install.sh [--claude-only|--codex-only|--help]

With no flag, configure every detected Claude Code and Codex installation.
  --claude-only  configure Claude Code only
  --codex-only   configure Codex only
  --quota        enable the Claude 5-hour quota guard
  --no-quota     disable the quota guard
  --context      enable the context-size guard
  --no-context   disable the context-size guard
  --quota-threshold N  hand off at N percent (default: 92)
  --help         show this help
EOF
}

SELECT_CLAUDE=false
SELECT_CODEX=false
AGENT_EXPLICIT=false
CONFIG_EXPLICIT=false
CONFIG_QUOTA=""
CONFIG_CONTEXT=""
QUOTA_THRESHOLD="92"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --claude-only)
      [[ "$AGENT_EXPLICIT" == false ]] || { echo "ERROR: agent selectors are mutually exclusive." >&2; exit 1; }
      SELECT_CLAUDE=true
      AGENT_EXPLICIT=true
      ;;
    --codex-only)
      [[ "$AGENT_EXPLICIT" == false ]] || { echo "ERROR: agent selectors are mutually exclusive." >&2; exit 1; }
      SELECT_CODEX=true
      AGENT_EXPLICIT=true
      ;;
    --quota) CONFIG_QUOTA=true; CONFIG_EXPLICIT=true ;;
    --no-quota) CONFIG_QUOTA=false; CONFIG_EXPLICIT=true ;;
    --context) CONFIG_CONTEXT=true; CONFIG_EXPLICIT=true ;;
    --no-context) CONFIG_CONTEXT=false; CONFIG_EXPLICIT=true ;;
    --quota-threshold)
      [[ "$#" -ge 2 ]] || { echo "ERROR: --quota-threshold requires a value." >&2; exit 1; }
      QUOTA_THRESHOLD="$2"
      CONFIG_QUOTA=true
      CONFIG_EXPLICIT=true
      shift
      ;;
    --help) usage; exit 0 ;;
    *) echo "ERROR: unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
  shift
done

if [[ "$AGENT_EXPLICIT" == false ]]; then
  if command -v claude >/dev/null 2>&1 || [[ -d "$CLAUDE_DIR" ]]; then
    SELECT_CLAUDE=true
  fi
  if command -v codex >/dev/null 2>&1 || [[ -d "$CODEX_DIR" ]]; then
    SELECT_CODEX=true
  fi
  if [[ "$SELECT_CLAUDE" == false && "$SELECT_CODEX" == false ]]; then
    echo "ERROR: no Claude Code or Codex installation detected." >&2
    echo "Use --claude-only or --codex-only to configure an agent explicitly." >&2
    exit 1
  fi
fi

if [[ "$CONFIG_QUOTA" == true && "$SELECT_CLAUDE" == false ]]; then
  echo "ERROR: the quota guard is Claude Code-specific; include Claude or omit --quota." >&2
  exit 1
fi

# Validate selected destinations before building or modifying any installation.
if [[ "$SELECT_CODEX" == true && -e "$CODEX_SKILL" && ! -L "$CODEX_SKILL" ]]; then
  echo "ERROR: Codex skill destination exists and is not a symlink: $CODEX_SKILL" >&2
  exit 1
fi

# Fresh interactive installs get the quota-first setup. Existing installations
# without config.json retain the legacy context-on/quota-off migration defaults.
EXISTING_INSTALL=false
if { [[ -f "$CLAUDE_DIR/settings.json" ]] && grep -Eq 'baton[^" ]*.*check|handoff_baton_check|hb check' "$CLAUDE_DIR/settings.json"; } ||
   { [[ -f "$CODEX_DIR/hooks.json" ]] && grep -Eq 'baton[^" ]*.*check|handoff_baton_check|hb check' "$CODEX_DIR/hooks.json"; }; then
  EXISTING_INSTALL=true
fi

if [[ "$CONFIG_EXPLICIT" == false && ! -f "$DATA_DIR/config.json" && "$EXISTING_INSTALL" == false && -t 0 && -t 1 ]]; then
  echo "Baton Pass setup"
  echo
  if [[ "$SELECT_CLAUDE" == true ]]; then
    echo "What should Baton watch?"
    echo "  1. 5-hour quota (recommended)"
    echo "  2. Context size"
    echo "  3. Both"
    echo "  4. Manual handoff only"
    read -r -p "Choice [1]: " guard_choice
    case "${guard_choice:-1}" in
      1) CONFIG_QUOTA=true;  CONFIG_CONTEXT=false ;;
      2) CONFIG_QUOTA=false; CONFIG_CONTEXT=true ;;
      3) CONFIG_QUOTA=true;  CONFIG_CONTEXT=true ;;
      4) CONFIG_QUOTA=false; CONFIG_CONTEXT=false ;;
      *) echo "ERROR: expected 1, 2, 3, or 4." >&2; exit 1 ;;
    esac
  else
    echo "What should Baton watch?"
    echo "  1. Context size"
    echo "  2. Manual handoff only"
    read -r -p "Choice [1]: " guard_choice
    case "${guard_choice:-1}" in
      1) CONFIG_QUOTA=false; CONFIG_CONTEXT=true ;;
      2) CONFIG_QUOTA=false; CONFIG_CONTEXT=false ;;
      *) echo "ERROR: expected 1 or 2." >&2; exit 1 ;;
    esac
  fi
  CONFIG_EXPLICIT=true
  if [[ "$CONFIG_QUOTA" == true ]]; then
    echo
    echo "When should Baton suggest handoff?"
    echo "  1. Balanced — 92%"
    echo "  2. Conservative — 85%"
    echo "  3. Late — 96%"
    echo "  4. Custom"
    read -r -p "Choice [1]: " quota_choice
    case "${quota_choice:-1}" in
      1) QUOTA_THRESHOLD=92 ;;
      2) QUOTA_THRESHOLD=85 ;;
      3) QUOTA_THRESHOLD=96 ;;
      4) read -r -p "Percentage: " QUOTA_THRESHOLD ;;
      *) echo "ERROR: expected 1, 2, 3, or 4." >&2; exit 1 ;;
    esac
  fi
fi

if [[ "$CONFIG_EXPLICIT" == true ]]; then
  CONFIG_QUOTA="${CONFIG_QUOTA:-false}"
  CONFIG_CONTEXT="${CONFIG_CONTEXT:-false}"
fi

echo "Installing baton-pass from: $REPO"

# 1. obtain the baton binary --------------------------------------------------
if command -v go >/dev/null 2>&1; then
  echo "  • building baton from source (go $(go version | awk '{print $3}'))"
  ( cd "$REPO" && go build -o "$BATON" ./cmd/baton )
else
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
  esac
  asset="baton_${os}_${arch}"
  url="https://github.com/$SLUG/releases/latest/download/$asset"
  echo "  • no Go toolchain found — downloading prebuilt binary: $asset"
  if ! curl -fsSL "$url" -o "$BATON"; then
    echo "ERROR: could not download $url" >&2
    echo "Install Go (https://go.dev/dl/) and re-run, or grab a binary from" >&2
    echo "https://github.com/$SLUG/releases" >&2
    exit 1
  fi
fi
chmod +x "$BATON" "$REPO/bin/batonresume"

if [[ "$CONFIG_EXPLICIT" == true ]]; then
  BATON_DATA="$DATA_DIR" "$BATON" init-config \
    --quota "$CONFIG_QUOTA" \
    --context "$CONFIG_CONTEXT" \
    --quota-threshold "$QUOTA_THRESHOLD"
fi

# 2. put commands on PATH -----------------------------------------------------
mkdir -p "$BIN_DIR"
ln -sf "$BATON"                "$BIN_DIR/baton"
ln -sf "$REPO/bin/batonresume" "$BIN_DIR/batonresume"
echo "  ✓ baton, batonresume → $BIN_DIR"

# 3. install each selected agent's skill and lifecycle hooks ------------------
if [[ "$SELECT_CLAUDE" == true ]]; then
  mkdir -p "$CLAUDE_DIR/skills/baton-pass"
  ln -sf "$REPO/SKILL.md" "$CLAUDE_DIR/skills/baton-pass/SKILL.md"
  echo "  ✓ Claude skill → $CLAUDE_DIR/skills/baton-pass/"
  "$BATON" install-hook "$CLAUDE_DIR/settings.json" claude
  if BATON_DATA="$DATA_DIR" "$BATON" is-enabled quota; then
    "$BATON" install-statusline "$CLAUDE_DIR/settings.json"
  fi
fi

if [[ "$SELECT_CODEX" == true ]]; then
  mkdir -p "$(dirname "$CODEX_SKILL")"
  ln -sfn "$REPO" "$CODEX_SKILL"
  echo "  ✓ Codex skill → $CODEX_SKILL"
  "$BATON" install-hook "$CODEX_DIR/hooks.json" codex

  if command -v codex >/dev/null 2>&1; then
    codex features enable hooks
    echo "  ✓ Codex hooks feature enabled"
  else
    echo "  ! Codex CLI not found; add this to $CODEX_DIR/config.toml:"
    echo "[features]"
    echo "hooks = true"
  fi
  echo "  ! Inspect and approve the baton-pass hook with /hooks in your next Codex session."
fi

echo
echo "Done. Restart the configured agent (or start a new session) to load the hook."
BATON_DATA="$DATA_DIR" "$BATON" status
