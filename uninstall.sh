#!/usr/bin/env bash
# Reverse install.sh: remove the Stop hook, skill, and command symlinks.
#   ./uninstall.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
BIN_DIR="$HOME/.local/bin"
SETTINGS="$CLAUDE_DIR/settings.json"

echo "Uninstalling handoff-baton..."

# commands
rm -f "$BIN_DIR/hresume" "$BIN_DIR/hresume-open" "$BIN_DIR/hb-state"
echo "  ✓ removed command symlinks"

# skill
rm -f "$CLAUDE_DIR/skills/handoff-baton/SKILL.md"
rmdir "$CLAUDE_DIR/skills/handoff-baton" 2>/dev/null || true
echo "  ✓ removed skill"

# Stop hook entry
if [ -f "$SETTINGS" ]; then
  python3 - "$SETTINGS" <<'PY'
import json, os, shutil, sys
settings_path = sys.argv[1]
with open(settings_path) as f:
    data = json.load(f)

stop = data.get("hooks", {}).get("Stop", [])
kept = [
    grp for grp in stop
    if not any("handoff_baton_check.py" in h.get("command", "") for h in grp.get("hooks", []))
]
if len(kept) != len(stop):
    shutil.copy(settings_path, settings_path + ".bak")
    data["hooks"]["Stop"] = kept
    with open(settings_path, "w") as f:
        json.dump(data, f, indent=2)
    print(f"  ✓ removed Stop hook (backup: {os.path.basename(settings_path)}.bak)")
else:
    print("  ✓ no Stop hook entry found")
PY
fi

echo "Done. (Runtime data in handoffs/ and state/ left untouched.)"
