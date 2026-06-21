#!/usr/bin/env bash
# Reverse install.sh: remove the Stop hook, skill, and command symlinks.
#   ./uninstall.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
BIN_DIR="$HOME/.local/bin"
SETTINGS="$CLAUDE_DIR/settings.json"

echo "Uninstalling baton-pass..."

# commands (incl. legacy names from earlier installs)
rm -f "$BIN_DIR/batonresume" "$BIN_DIR/baton" "$BIN_DIR/hresume" "$BIN_DIR/hb" "$BIN_DIR/hb-state"
echo "  ✓ removed command symlinks"

# skill (incl. legacy name)
rm -f "$CLAUDE_DIR/skills/baton-pass/SKILL.md" "$CLAUDE_DIR/skills/handoff-baton/SKILL.md"
rmdir "$CLAUDE_DIR/skills/baton-pass" "$CLAUDE_DIR/skills/handoff-baton" 2>/dev/null || true
echo "  ✓ removed skill"

# Stop hook entry
if [ -f "$SETTINGS" ] && [ -x "$REPO/bin/baton" ]; then
  "$REPO/bin/baton" uninstall-hook "$SETTINGS"
elif [ -f "$SETTINGS" ]; then
  echo "  ! bin/baton not built — remove the Stop hook entry from $SETTINGS manually"
fi

echo "Done. (Runtime data in handoffs/ and state/ left untouched.)"
