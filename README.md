<div align="center">

# 🏃 handoff-baton

**Pass the baton before your context runs out of breath.**

A Stop-hook + skill that watches your agent's context size and, when it gets
expensive, hands the work off to a *fresh* session — so you stop paying to
re-send a giant transcript on every single turn.

![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)
![skills.sh](https://img.shields.io/badge/install-skills.sh-black)
![agents](https://img.shields.io/badge/agents-Claude%20Code%20·%20Codex%20·%20Cursor-blue)

</div>

---

## 🧠 Why this exists

Every turn of an agent conversation re-sends the **entire context** to the model.
As a session grows, two bad things happen:

1. **Cost climbs every turn.** A conversation parked at 160K tokens pays for
   ~160K of context *on each reply* — forever, until you stop.
2. **Auto-compaction kicks in.** Near the window limit your agent silently
   compacts the history into a lossy summary you didn't write and can't review.

`handoff-baton` fixes both. When you cross a threshold, it offers to write a
clean **handoff document** and restart in a fresh session seeded with just that
doc — resetting your context from *huge* back to *tiny*.

> 🏁 Think of a relay race: each session runs its leg, then passes the baton
> (the handoff doc) to a fresh runner instead of dragging the whole track behind it.

---

## 💰 How much does it save?

The recurring cost of a turn scales with context size. With prompt caching, a
cached re-read costs roughly **10%** of the context's token price — but you pay
it *every turn*. Resetting context is what saves money.

**Illustrative scenario** — you've reached **160K tokens** and still have work to do:

| Remaining work | Without handoff | With handoff* | You save |
| -------------- | --------------- | ------------- | -------- |
| 40 more turns  | ~640K tok-equiv | ~200K tok-equiv | **~70%** |
| 100 more turns | ~1.6M tok-equiv | ~260K tok-equiv | **~84%** |

<sub>\* "With handoff" = one-time ~160K-token summarization pass, then a fresh
~10K context billed at ~10% cached reads per turn. Figures are token-equivalents
and approximate — actual savings depend on model pricing, cache hit rate, and
output size. **The longer you keep working past the threshold, the bigger the win.**</sub>

And the cost story *understates* it: handing off **before** auto-compaction also
preserves quality — a deliberate handoff you can read and edit beats a silent,
lossy auto-summary.

---

## ⚙️ How it works

```
Stop hook (after every turn)
  ├─ read context size + this session's threshold   (free — just reads the transcript)
  ├─ disabled for this session?  → do nothing
  ├─ under threshold?            → do nothing
  └─ over threshold? → block the stop and ask you:
        1. Handoff now            → write handoff doc, then run `hresume`
        2. Extend +10K            → bump this session's threshold, continue
        3. Disable this convo     → stop asking this session, continue
        4. Skip                   → continue, ask again next turn

hresume claude|codex   → fresh session, handoff doc passed in as the opening prompt
```

The check is **free** — it only reads token counts already recorded in the
transcript file, no model call. Resuming passes the handoff **in the opening
prompt** (not via a session-start hook), so an unrelated new session never
inherits a stale handoff.

---

## 🚀 Install

### ✨ The AI-native way (recommended)

Don't wire it up by hand — let your agent do it. Paste this into Claude Code,
Codex, or Cursor from inside the folder where you want it:

```
Install handoff-baton for me.

1. Clone https://github.com/Rorogogogo/handoff-baton (or tell me where it is).
2. chmod +x its hooks/handoff_baton_check.py, bin/hb-state, bin/hresume.
3. Add its bin/ directory to my PATH (append to my shell rc).
4. Register the Stop hook from its settings.example.json into MY agent's config:
     - Claude Code → ~/.claude/settings.json  (Stop hook)
     - Codex       → ~/.codex/config.toml      ([hooks], Stop event, codex_hooks=true)
     - Cursor      → my hooks.json             (stop event)
5. Symlink its SKILL.md into my skills directory.
6. Then show me how to use `hresume`.

Read the repo's README first, confirm the steps, then do it.
```

That's the whole point of an agent — it can read this repo and install itself.

### 🟣 Claude Code — first-class (tested)

```sh
git clone https://github.com/Rorogogogo/handoff-baton && cd handoff-baton
chmod +x hooks/handoff_baton_check.py bin/hb-state bin/hresume
echo "export PATH=\"$PWD/bin:\$PATH\"" >> ~/.zshrc

# install the skill
mkdir -p ~/.claude/skills/handoff-baton
ln -s "$PWD/SKILL.md" ~/.claude/skills/handoff-baton/SKILL.md
```

Then merge `settings.example.json` into `~/.claude/settings.json` (replace the
path):

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [
        { "type": "command",
          "command": "python3 /ABSOLUTE/PATH/handoff-baton/hooks/handoff_baton_check.py" }
      ] }
    ]
  }
}
```

### 🟢 OpenAI Codex CLI — supported (needs the hooks flag)

Codex has a compatible `Stop` hook (experimental). Enable it and register the
script in `~/.codex/config.toml`:

```toml
[features]
codex_hooks = true

# register hooks/handoff_baton_check.py on the Stop event — see the Codex hooks
# docs for the exact [hooks] / hooks.json schema:
# https://developers.openai.com/codex/hooks
```

> ⚠️ Codex's hook input JSON and transcript format differ from Claude Code's, so
> the token-reading in `handoff_baton_check.py` may need a small per-agent
> adapter. The skill, `hresume`, and `hb-state` work as-is. PRs welcome.

### 🔵 Cursor — supported

Cursor 1.7+ has a `stop` hook with the same stdin-JSON model. Register the
script in your Cursor `hooks.json` per the
[Cursor hooks docs](https://cursor.com/docs/hooks). Same transcript-adapter
caveat as Codex applies.

### 🟡 Antigravity CLI & everything else

The **skill** is cross-agent (skills.sh markets one skill across Claude Code,
Codex, Cursor, Gemini, Cline, and more):

```sh
npx skills add Rorogogogo/handoff-baton
```

`npx skills` installs the **skill only** — enough to run handoffs **manually**
and use `hresume`. Automatic context-watching needs that agent's own Stop-hook
mechanism; if it doesn't have one yet, run the skill by hand when a session gets
heavy.

---

## 🔧 Configuration

Set via env vars (in your shell, or on the hook command):

| Variable                    | Default      | Meaning                                                          |
| --------------------------- | ------------ | ---------------------------------------------------------------- |
| `HANDOFF_BATON_THRESHOLD`   | `160000`     | Base context threshold in tokens (~80% of a 200K window — fires before auto-compact). |
| `HANDOFF_BATON_EXTEND_STEP` | `10000`      | How much "Extend" adds (current + step).                         |
| `HANDOFF_BATON_DATA`        | the repo dir | Where `state/` and `handoffs/` live.                             |

---

## 📂 Data layout

```
handoffs/<project-name>/handoff-<YYYYMMDD-HHMM>.md   # one per handoff, kept for history
state/<session_id>.json                              # { threshold_override, disabled }
```

`<project-name>` is the working directory's basename, so handoffs from different
projects stay separate and auditable. Both folders are git-ignored — local
runtime data, never committed.

> Locally, `HANDOFF_BATON_DATA` defaults to this repo folder so everything lives
> in one place. If you install to a read-only/managed location, point it at a
> writable dir (e.g. `~/.handoff-baton`).

---

## 🛠️ Commands

| Command | What it does |
| ------- | ------------ |
| `hresume [claude\|codex] [file]` | Relaunch into a fresh session from a handoff (newest for the current project if no file given). |
| `hb-state extend <session_id> <value>` | Raise a session's threshold. |
| `hb-state disable <session_id>` | Silence handoff-baton for a session. |
| `hb-state reset <session_id>` | Clear a session's state. |

---

## 📄 License

[MIT](./LICENSE) © Robert Wang
