#!/usr/bin/env bash
# One-command installer for baton-pass.
#   ./install.sh
# No runtime dependencies: ships a single Go binary (`baton`). If a Go toolchain
# is present it builds from source; otherwise it downloads the matching prebuilt
# binary from GitHub Releases. Idempotent. Reverse with ./uninstall.sh.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
BIN_DIR="$HOME/.local/bin"
SETTINGS="$CLAUDE_DIR/settings.json"
SLUG="Rorogogogo/baton-pass"
BATON="$REPO/bin/baton"

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

# 3. install the skill --------------------------------------------------------
mkdir -p "$CLAUDE_DIR/skills/baton-pass"
ln -sf "$REPO/SKILL.md" "$CLAUDE_DIR/skills/baton-pass/SKILL.md"
echo "  ✓ skill → $CLAUDE_DIR/skills/baton-pass/"

# 4. register the Stop hook (merge into existing settings, idempotent) --------
"$BATON" install-hook "$SETTINGS"

echo
echo "Done. Restart Claude Code (or start a new session) to load the hook."
echo "Default threshold: 190000 tokens — override with BATON_THRESHOLD."
