#!/usr/bin/env python3
"""handoff-baton Stop hook.

Runs after each assistant turn. Reads the current context size from the
transcript (free — no model call, just a file read), compares it against this
session's threshold, and if it's over, blocks the stop and asks Claude to offer
the user a handoff. Per-session state (threshold overrides, disable flag) lives
in <data>/state/<session_id>.json.
"""
import sys
import os
import json
import shutil

BASE_THRESHOLD = int(os.environ.get("HANDOFF_BATON_THRESHOLD", "160000"))
EXTEND_STEP = int(os.environ.get("HANDOFF_BATON_EXTEND_STEP", "10000"))


def read_context_tokens(path):
    """Return the context size used on the last assistant turn, or None."""
    if not path or not os.path.exists(path):
        return None
    last = None
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except ValueError:
                    continue
                msg = obj.get("message")
                if isinstance(msg, dict) and msg.get("role") == "assistant":
                    usage = msg.get("usage")
                    if isinstance(usage, dict):
                        last = usage
    except OSError:
        return None
    if not last:
        return None
    return (
        last.get("input_tokens", 0)
        + last.get("cache_read_input_tokens", 0)
        + last.get("cache_creation_input_tokens", 0)
    )


def resolve_cmd(name, repo_root):
    """Prefer the bare command name when it's installed on PATH; else absolute."""
    if shutil.which(name) or os.path.exists(os.path.expanduser(f"~/.local/bin/{name}")):
        return name
    return os.path.join(repo_root, "bin", name)


def build_instructions(*, session_id, handoff_dir, hb_state, hresume, extend_value):
    """Detailed, model-facing instructions — delivered silently via additionalContext."""
    return (
        "The handoff-baton context threshold was reached. Use the AskUserQuestion tool "
        "(the native option picker) to ask how to proceed — do NOT ask in plain text. "
        "Offer these four options and act on the choice:\n\n"
        f"• Handoff now — run the `handoff-baton` skill to write the handoff doc under "
        f"{handoff_dir}/, then tell the user to resume with `{hresume} claude` (or `{hresume} codex`).\n"
        f"• Extend +10K — run `{hb_state} extend {session_id} {extend_value}`, then continue.\n"
        f"• Disable here — run `{hb_state} disable {session_id}`, then continue.\n"
        "• Skip — continue; you'll be asked again next turn."
    )


def main():
    try:
        data = json.load(sys.stdin)
    except (ValueError, OSError):
        sys.exit(0)

    # Avoid an infinite block loop: if we're already inside a stop-hook
    # continuation, don't block again.
    if data.get("stop_hook_active"):
        sys.exit(0)

    session_id = data.get("session_id", "unknown")
    transcript_path = data.get("transcript_path", "")
    cwd = data.get("cwd") or os.getcwd()
    project = os.path.basename(cwd.rstrip("/")) or "default"

    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    data_dir = os.environ.get("HANDOFF_BATON_DATA", repo_root)
    state_path = os.path.join(data_dir, "state", f"{session_id}.json")

    state = {}
    if os.path.exists(state_path):
        try:
            with open(state_path) as f:
                state = json.load(f)
        except (ValueError, OSError):
            state = {}

    if state.get("disabled"):
        sys.exit(0)

    threshold = int(state.get("threshold_override") or BASE_THRESHOLD)

    tokens = read_context_tokens(transcript_path)
    if tokens is None or tokens < threshold:
        sys.exit(0)

    instructions = build_instructions(
        session_id=session_id,
        handoff_dir=os.path.join(data_dir, "handoffs", project),
        hb_state=resolve_cmd("hb-state", repo_root),
        hresume=resolve_cmd("hresume", repo_root),
        extend_value=tokens + EXTEND_STEP,
    )
    # Short, user-visible notice; the actionable detail rides the silent
    # additionalContext channel so it never clutters the transcript.
    notice = f"[handoff-baton] Context {tokens:,} ≥ {threshold:,} — pick how to proceed."
    print(json.dumps({
        "decision": "block",
        "reason": notice,
        "hookSpecificOutput": {
            "hookEventName": "Stop",
            "additionalContext": instructions,
        },
    }))
    sys.exit(0)


if __name__ == "__main__":
    main()
