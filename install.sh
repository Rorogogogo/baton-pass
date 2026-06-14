#!/usr/bin/env bash
# One-command installer for handoff-baton (Claude Code).
#   ./install.sh
# Idempotent. Backs up settings.json before editing. Reverse with ./uninstall.sh.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
BIN_DIR="$HOME/.local/bin"
SETTINGS="$CLAUDE_DIR/settings.json"

echo "Installing handoff-baton from: $REPO"

# 1. make scripts executable
chmod +x "$REPO/hooks/handoff_baton_check.py" "$REPO/bin/hb-state" "$REPO/bin/hresume"

# 2. put commands on PATH
mkdir -p "$BIN_DIR"
ln -sf "$REPO/bin/hresume"  "$BIN_DIR/hresume"
ln -sf "$REPO/bin/hb-state" "$BIN_DIR/hb-state"
echo "  ✓ hresume, hb-state → $BIN_DIR"

# 3. install the skill
mkdir -p "$CLAUDE_DIR/skills/handoff-baton"
ln -sf "$REPO/SKILL.md" "$CLAUDE_DIR/skills/handoff-baton/SKILL.md"
echo "  ✓ skill → $CLAUDE_DIR/skills/handoff-baton/"

# 4. register the Stop hook (merge into existing settings, idempotent)
python3 - "$SETTINGS" "$REPO" <<'PY'
import json, os, shutil, sys
settings_path, repo = sys.argv[1], sys.argv[2]
cmd = f'python3 "{repo}/hooks/handoff_baton_check.py"'

data = {}
if os.path.exists(settings_path):
    with open(settings_path) as f:
        data = json.load(f)

stop = data.setdefault("hooks", {}).setdefault("Stop", [])
present = any(
    "handoff_baton_check.py" in h.get("command", "")
    for grp in stop for h in grp.get("hooks", [])
)
if present:
    print("  ✓ Stop hook already registered")
else:
    os.makedirs(os.path.dirname(settings_path), exist_ok=True)
    if os.path.exists(settings_path):
        shutil.copy(settings_path, settings_path + ".bak")
    stop.append({"hooks": [{"type": "command", "command": cmd}]})
    with open(settings_path, "w") as f:
        json.dump(data, f, indent=2)
    print(f"  ✓ Stop hook registered (backup: {os.path.basename(settings_path)}.bak)")
PY

echo
echo "Done. Restart Claude Code (or start a new session) to load the hook."
echo "Default threshold: 160000 tokens — override with HANDOFF_BATON_THRESHOLD."
