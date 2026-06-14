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


def build_reason(*, tokens, threshold, session_id, handoff_dir, hb_state, extend_value):
    return f"""[handoff-baton] Context is at {tokens:,} tokens (threshold {threshold:,}).

Before continuing, ask the user how they want to proceed. Offer exactly these
options and act on their choice — do not choose for them:

1. Handoff now — Invoke the `handoff-baton` skill to write a handoff document to:
     {handoff_dir}/handoff-<timestamp>.md
   Then tell the user to resume in a fresh session with:  hresume claude
   (or:  hresume codex)

2. Extend +10K — Raise this session's threshold, then continue. Run:
     {hb_state} extend {session_id} {extend_value}

3. Disable for this conversation — Stop asking in this session, then continue. Run:
     {hb_state} disable {session_id}

4. Skip — Do nothing; you'll be asked again on the next turn. Just continue."""


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
    hb_state = os.path.join(repo_root, "bin", "hb-state")
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

    reason = build_reason(
        tokens=tokens,
        threshold=threshold,
        session_id=session_id,
        handoff_dir=os.path.join(data_dir, "handoffs", project),
        hb_state=hb_state,
        extend_value=tokens + EXTEND_STEP,
    )
    print(json.dumps({"decision": "block", "reason": reason}))
    sys.exit(0)


if __name__ == "__main__":
    main()
