#!/usr/bin/env bash
# One-command installer for baton-pass.
#   ./install.sh [--claude-only|--codex-only]
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

usage() {
  cat <<'EOF'
Usage: ./install.sh [--claude-only|--codex-only|--help]

With no flag, configure every detected Claude Code and Codex installation.
  --claude-only  configure Claude Code only
  --codex-only   configure Codex only
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
      echo "Use --claude-only or --codex-only to configure an agent explicitly." >&2
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

# 2. put commands on PATH -----------------------------------------------------
mkdir -p "$BIN_DIR"
ln -sf "$BATON"                "$BIN_DIR/baton"
ln -sf "$REPO/bin/batonresume" "$BIN_DIR/batonresume"
echo "  ✓ baton, batonresume → $BIN_DIR"

# 3. install each selected agent's skill and Stop hook ------------------------
if [[ "$SELECT_CLAUDE" == true ]]; then
  mkdir -p "$CLAUDE_DIR/skills/baton-pass"
  ln -sf "$REPO/SKILL.md" "$CLAUDE_DIR/skills/baton-pass/SKILL.md"
  echo "  ✓ Claude skill → $CLAUDE_DIR/skills/baton-pass/"
  "$BATON" install-hook "$CLAUDE_DIR/settings.json"
fi

if [[ "$SELECT_CODEX" == true ]]; then
  CODEX_SKILL="$HOME/.agents/skills/baton-pass"
  mkdir -p "$(dirname "$CODEX_SKILL")"
  if [[ -e "$CODEX_SKILL" && ! -L "$CODEX_SKILL" ]]; then
    echo "ERROR: Codex skill destination exists and is not a symlink: $CODEX_SKILL" >&2
    exit 1
  fi
  ln -sfn "$REPO" "$CODEX_SKILL"
  echo "  ✓ Codex skill → $CODEX_SKILL"
  "$BATON" install-hook "$CODEX_DIR/hooks.json"

  if command -v codex >/dev/null 2>&1; then
    codex features enable hooks
    echo "  ✓ Codex hooks feature enabled"
  else
    echo "  ! Codex CLI not found; enable hooks manually in $CODEX_DIR/config.toml: [features] hooks = true"
  fi
  echo "  ! Inspect and approve the baton-pass hook with /hooks in your next Codex session."
fi

echo
echo "Done. Restart the configured agent (or start a new session) to load the hook."
echo "Default threshold: 190000 tokens — override with BATON_THRESHOLD."
