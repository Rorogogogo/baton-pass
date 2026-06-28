<div align="center">

# 🏃 baton-pass

**Pass the baton before your context runs out of breath.**

**English** | [简体中文](./README.zh-CN.md)

A Stop-hook + skill that watches your agent's context size and, when it gets
expensive, hands the work off to a *fresh* session — so you stop paying to
re-send a giant transcript on every single turn. It's a single dependency-free
**Go** binary — no Python, Node, or `jq` to install.

![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
![No runtime deps](https://img.shields.io/badge/runtime%20deps-none-success)
![skills.sh](https://img.shields.io/badge/install-skills.sh-black)
![agents](https://img.shields.io/badge/agents-Claude%20Code%20·%20Codex%20·%20Cursor-blue)

</div>

---

## 🧠 Why this exists

Every turn of an agent conversation re-sends the **entire context** to the model.
As a session grows, two bad things happen:

1. **Cost climbs every turn.** A conversation parked at 190K tokens pays for
   ~190K of context *on each reply* — forever, until you stop.
2. **Auto-compaction kicks in.** Near the window limit your agent silently
   compacts the history into a lossy summary you didn't write and can't review.

`baton-pass` fixes both. When you cross a threshold, it offers to write a
clean **handoff document** and restart in a fresh session seeded with just that
doc — resetting your context from *huge* back to *tiny*.

> 🏁 Think of a relay race: each session runs its leg, then passes the baton
> (the handoff doc) to a fresh runner instead of dragging the whole track behind it.

---

## 💰 How much does it save?

The recurring cost of a turn scales with context size. With prompt caching, a
cached re-read costs roughly **10%** of the context's token price — but you pay
it *every turn*. Resetting context is what saves money.

**Illustrative scenario** — you've reached **190K tokens** and still have work to do:

| Remaining work | Without handoff | With handoff* | You save |
| -------------- | --------------- | ------------- | -------- |
| 40 more turns  | ~760K tok-equiv | ~230K tok-equiv | **~70%** |
| 100 more turns | ~1.9M tok-equiv | ~290K tok-equiv | **~85%** |

<sub>\* "With handoff" = one-time ~190K-token summarization pass, then a fresh
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
  └─ over threshold? → one-line notice + a native ↑/↓ picker:
        1. Handoff now            → write handoff doc, then: exit + `batonresume`
        2. Extend +10K            → bump this session's threshold, continue
        3. Disable this convo     → stop asking this session, continue
        4. Skip                   → continue, ask again next turn
```

After "Handoff now":  exit the session (/exit), then run `batonresume` → a fresh
session seeded with the handoff doc as its opening prompt. The agent
(claude / codex) is auto-detected, so it suggests the right command.

`batonresume` finds the right handoff for you, so you never have to paste a long,
space-containing path:

```
batonresume                       # newest handoff for the current folder,
                              #   or newest overall if there's none here
batonresume claude baton-pass  # target a project by name, from anywhere
batonresume --list                # see every saved handoff, newest first
```

A few deliberate design points:

- **The check is free** — it only reads token counts already recorded in the
  transcript file; no model call, no tokens spent.
- **The transcript stays clean** — only a one-line notice is shown; all the
  detail (options, exact commands) rides a silent `additionalContext` channel
  that drives the native picker without cluttering your chat.
- **No stale handoffs** — resuming passes the doc **in the opening prompt** (not
  via a session-start hook), so an unrelated new session never inherits one.

---

## 🚀 Install

### ✨ The AI-native way (recommended)

Don't wire it up by hand — let your agent do it. Paste this into Claude Code,
Codex, or Cursor from inside the folder where you want it:

```
Install baton-pass for me.

1. Clone https://github.com/Rorogogogo/baton-pass (or tell me where it is).
2. Run its ./install.sh (builds the `baton` binary, or downloads a prebuilt one).
3. Add its bin/ directory to my PATH (append to my shell rc).
4. Register the Stop hook from its settings.example.json into MY agent's config:
     - Claude Code → ~/.claude/settings.json  (Stop hook)
     - Codex       → ~/.codex/config.toml      ([hooks], Stop event, codex_hooks=true)
     - Cursor      → my hooks.json             (stop event)
5. Symlink its SKILL.md into my skills directory.
6. Then show me how to use `batonresume`.

Read the repo's README first, confirm the steps, then do it.
```

That's the whole point of an agent — it can read this repo and install itself.

### 🟣 Claude Code — first-class (tested)

**One command:**

```sh
git clone https://github.com/Rorogogogo/baton-pass && cd baton-pass
./install.sh
```

`install.sh` builds the single `baton` binary (or downloads a prebuilt one if you
have no Go toolchain), symlinks the skill and the `baton` / `batonresume` commands onto
your PATH, and **merges** the Stop hook into your `~/.claude/settings.json` —
preserving any existing hooks, backing the file up to `settings.json.bak`, and
skipping if it's already there. Restart Claude Code to load it. Reverse anytime
with `./uninstall.sh`.

> 🪶 **No runtime dependencies.** `baton` is a self-contained Go binary — no Python,
> Node, or `jq` to install. The only optional dependency is a Go toolchain *at
> install time* to build from source; without it, `install.sh` downloads a
> prebuilt static binary from Releases.

<details>
<summary>Prefer to wire it up by hand?</summary>

```sh
go build -o bin/baton ./cmd/baton        # or download bin/baton from Releases
ln -s "$PWD/bin/batonresume" ~/.local/bin/batonresume
ln -s "$PWD/bin/baton"       ~/.local/bin/baton
mkdir -p ~/.claude/skills/baton-pass
ln -s "$PWD/SKILL.md" ~/.claude/skills/baton-pass/SKILL.md
```

Then add to `~/.claude/settings.json` (see `settings.example.json`):

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [
        { "type": "command",
          "command": "\"/ABSOLUTE/PATH/baton-pass/bin/baton\" check" }
      ] }
    ]
  }
}
```
</details>

### 🟢 OpenAI Codex CLI — first-class (needs the hooks flag)

Codex has a compatible `Stop` hook (experimental). Enable it and register
`baton check` on the Stop event in `~/.codex/hooks.json` (and turn the feature on in
`~/.codex/config.toml`):

```toml
# ~/.codex/config.toml
[features]
codex_hooks = true
```

```json
// ~/.codex/hooks.json
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command",
        "command": "\"/ABSOLUTE/PATH/baton-pass/bin/baton\" check" } ] }
    ]
  }
}
```

> ✅ **Auto-trigger works on Codex too.** Codex's Stop payload uses the same
> fields as Claude (`session_id`, `transcript_path`, `cwd`, `stop_hook_active`)
> and also honors `decision: "block"` + `additionalContext`. `baton check` reads
> Codex's rollout `token_count` events natively (it uses
> `info.last_token_usage.input_tokens` as the live context size), so the same
> binary drives the full watch → handoff → `batonresume codex` loop. Tip: Codex
> windows are larger than 200K, so bump `BATON_THRESHOLD` to suit.

### 🔵 Cursor — supported

Cursor 1.7+ has a `stop` hook with the same stdin-JSON model. Register the
script in your Cursor `hooks.json` per the
[Cursor hooks docs](https://cursor.com/docs/hooks). Same transcript-adapter
caveat as Codex applies.

### 🟡 Antigravity CLI & everything else

The **skill** is cross-agent (skills.sh markets one skill across Claude Code,
Codex, Cursor, Gemini, Cline, and more):

```sh
npx skills add Rorogogogo/baton-pass
```

`npx skills` installs the **skill only** — enough to run handoffs **manually**
and use `batonresume`. Automatic context-watching needs that agent's own Stop-hook
mechanism; if it doesn't have one yet, run the skill by hand when a session gets
heavy.

---

## 🔧 Configuration

Set via env vars (in your shell, or on the hook command):

| Variable                    | Default      | Meaning                                                          |
| --------------------------- | ------------ | ---------------------------------------------------------------- |
| `BATON_THRESHOLD`   | `190000`     | Base context threshold in tokens (~95% of a 200K window — fires before auto-compact). |
| `BATON_EXTEND_STEP` | `10000`      | How much "Extend" adds (current + step).                         |
| `BATON_DATA`        | the repo dir | Where `state/` and `handoffs/` live.                             |
| `BATON_TOOL`        | auto-detect  | Force the resume target (`claude`/`codex`). Auto-detected from `$AI_AGENT`/`$CLAUDECODE` otherwise. |

---

## 📂 Data layout

```
handoffs/<project-name>/handoff-<YYYYMMDD-HHMM>.md   # one per handoff, kept for history
state/<session_id>.json                              # { threshold_override, disabled }
```

`<project-name>` is the working directory's basename, so handoffs from different
projects stay separate and auditable. Both folders are git-ignored — local
runtime data, never committed.

> Locally, `BATON_DATA` defaults to this repo folder so everything lives
> in one place. If you install to a read-only/managed location, point it at a
> writable dir (e.g. `~/.baton-pass`).

---

## 🛠️ Commands

| Command | What it does |
| ------- | ------------ |
| `batonresume [claude\|codex] [project\|file]` | Relaunch into a fresh session from a handoff. With no second arg: newest for the current folder, else newest overall. Pass a project name (from anywhere) or a file path to target one. `batonresume --list` shows all. Run it after you exit the old session. |
| `baton extend <session_id> <value>` | Raise a session's threshold. |
| `baton disable <session_id>` | Silence baton-pass for a session. |
| `baton reset <session_id>` | Clear a session's state. |
| `baton check` | The Stop hook itself (reads the hook payload on stdin); you don't call this by hand. |

---

## 🙏 Credits

The handoff **skill** (`SKILL.md` — how the handoff document is written) is adapted
from [Matt Pocock's `handoff` skill](https://github.com/mattpocock/skills/tree/main/skills/productivity/handoff)
(MIT). `baton-pass` wires that idea into an automatic, cost-driven workflow: a
zero-dependency [Go](https://go.dev) Stop-hook that watches context size across
Claude Code and Codex, plus `batonresume` to relaunch a fresh session. Thanks Matt! 🎩

## 📄 License

[MIT](./LICENSE) © 2026 Robert Wang.
Portions of `SKILL.md` © 2026 Matt Pocock, used under the MIT License — see [LICENSE](./LICENSE).
