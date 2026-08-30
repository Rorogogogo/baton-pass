<div align="center">

# 🏃 baton-pass

**Preserve progress before context or usage limits interrupt the work.**

**English** | [简体中文](./README.zh-CN.md)

A local continuity tool with optional context and Claude 5-hour quota guards.
At a safe boundary it reuses the same `baton-pass` skill to hand work to a
*fresh* session. It is one dependency-free **Go** binary—no Python, Node, `jq`,
network service, daemon, or Notchy dependency.

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

`baton-pass` fixes both and can also hand off before Claude Code's real 5-hour
usage window is exhausted. Context, quota, and manual handoff are independent:

```text
Context guard ─┐
Quota guard   ─┼─→ baton-pass skill → handoff document → batonresume
Manual request ┘
```

The runtime decides **when**; the existing skill decides **how**. No second
quota-specific handoff system is introduced.

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
Claude statusline JSON → silent local usage writer → usage.json
                                                   ↓
Claude hooks / Codex Stop hook → baton policy engine
  ├─ context guard enabled and threshold reached?
  ├─ Claude quota guard enabled and fresh telemetry reached threshold?
  └─ trigger once at a safe boundary → existing baton-pass skill
```

Claude quota checks run at `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, and
`Stop`. Healthy checks produce no output and add zero context. `PostToolUse` is
the primary handoff boundary; `PreToolUse` prevents starting another phase;
`Stop` remains the context guard and quota fallback. Missing, malformed, stale,
or expired telemetry always fails open.

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
2. Read its README and run `./install.sh`.
3. Confirm which detected agents were configured and whether Codex needs `/hooks` approval.
4. Then show me how to use `batonresume`.

Read the repo's README first, confirm the steps, then do it.
```

That's the whole point of an agent — it can read this repo and install itself.

### 🟣 Claude Code + 🟢 Codex — one installer

**One command:**

```sh
git clone https://github.com/Rorogogogo/baton-pass && cd baton-pass
./install.sh
```

`install.sh` builds the single `baton` binary (or downloads a prebuilt one if you
have no Go toolchain), installs the shared `baton` / `batonresume` commands under
`~/.local/bin`, and configures **every detected supported agent**. If both Claude
Code and Codex are installed, one run configures both. Use an explicit selector
when you only want one:

```sh
./install.sh --claude-only
./install.sh --codex-only

# Non-interactive quota-only setup
./install.sh --claude-only --quota --no-context --quota-threshold 92
```

On a fresh interactive install, Baton asks whether to watch 5-hour quota,
context size, both, or neither. The recommended fresh setup is quota-only with
a balanced 92% handoff threshold. Existing installations migrate to context-on
and quota-off, so upgrading never silently enables a new quota trigger.

The installer creates these agent-specific links and merges idempotent hooks:

| Agent | Skill | Stop hooks |
|---|---|---|
| Claude Code | `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/skills/baton-pass/SKILL.md` | Prompt, pre-tool, post-tool, and Stop in `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json` |
| Codex | `~/.agents/skills/baton-pass` (folder symlink to this repository) | Stop only in `${CODEX_HOME:-$HOME/.codex}/hooks.json` |

When quota is enabled, the installer wraps Claude's existing `statusLine`
command. Baton extracts only numeric rate-limit fields, then runs the original
command with the original JSON and preserves its output and settings metadata.
Reruns do not nest wrappers; uninstall restores the exact prior command.

Existing hook files are preserved and backed up beside the original as `.bak`.
Running the installer again does not duplicate Stop entries. For Codex, the
installer runs `codex features enable hooks`; if the CLI is not on `PATH`, it
prints the equivalent `[features]` / `hooks = true` configuration. It deliberately
does not write a hook trust hash: inspect and approve baton-pass with `/hooks` in
your next Codex session.

Restart every configured Claude Code or Codex session so it reloads its hooks.
Reverse the detected installations with `./uninstall.sh`, or select one with
`./uninstall.sh --claude-only` / `./uninstall.sh --codex-only`. Uninstall removes
only baton-pass entries and leaves handoffs, state, unrelated hooks, and Codex's
global hooks feature intact.

> 🪶 **No runtime dependencies.** `baton` is a self-contained Go binary — no Python,
> Node, or `jq` to install. The only optional dependency is a Go toolchain *at
> install time* to build from source; without it, `install.sh` downloads a
> prebuilt static binary from Releases.

<details>
<summary>Prefer to inspect the hook shape?</summary>

```sh
go build -o bin/baton ./cmd/baton        # or download bin/baton from Releases
ln -s "$PWD/bin/batonresume" ~/.local/bin/batonresume
ln -s "$PWD/bin/baton"       ~/.local/bin/baton
mkdir -p ~/.claude/skills/baton-pass
ln -s "$PWD/SKILL.md" ~/.claude/skills/baton-pass/SKILL.md
```

Then add to `~/.claude/settings.json` (see `settings.example.json`). The
installer is preferred because it safely merges lifecycle hooks and statusline
telemetry without replacing unrelated settings.

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

### Codex behavior

Codex hooks use `~/.codex/hooks.json`, while the feature switch in
`~/.codex/config.toml` is:

```toml
# ~/.codex/config.toml
[features]
hooks = true
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

> ✅ **Auto-trigger works on Codex too.** `baton check` returns Codex's supported
> Stop response: `decision: "block"` with the four choices in `reason`; it does
> not emit Claude's `hookSpecificOutput.additionalContext`. Claude Code receives
> its native option-picker instructions, while Codex Default mode receives a
> normal four-choice prompt. `baton check` reads Codex rollout `token_count`
> events natively (it uses
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

Persistent configuration lives in `<data>/config.json`. Use:

```sh
baton status
baton config
baton enable quota
baton disable quota
baton enable context
baton disable context
baton config quota 92
baton config context 190000
```

The default quota levels are aware 75%, caution 85%, handoff 92%, and emergency
96%. Only the handoff threshold is exposed by the short non-interactive command;
the JSON remains intentionally small and inspectable.

Environment overrides retained for compatibility:

| Variable                    | Default      | Meaning                                                          |
| --------------------------- | ------------ | ---------------------------------------------------------------- |
| `BATON_THRESHOLD`   | `190000`     | Base context threshold in tokens (~95% of a 200K window — fires before auto-compact). |
| `BATON_EXTEND_STEP` | `10000`      | How much "Extend" adds (current + step).                         |
| `BATON_DATA`        | the repo dir | Where `state/` and `handoffs/` live.                             |
| `BATON_TOOL`        | auto-detect  | Force the resume target (`claude`/`codex`). Auto-detected from `$AI_AGENT`/`$CLAUDECODE` otherwise. |

---

## 📂 Data layout

```
config.json                                          # independent guard configuration
usage.json                                           # numeric Claude quota telemetry only
handoffs/<project-name>/handoff-<YYYYMMDD-HHMM>.md  # one per handoff, kept for history
state/<session_id>.json                              # overrides and quota-window deduplication
```

`<project-name>` is the working directory's basename, so handoffs from different
projects stay separate and auditable. These runtime paths are git-ignored and
never committed.

> Locally, `BATON_DATA` defaults to this repo folder so everything lives
> in one place. If you install to a read-only/managed location, point it at a
> writable dir (e.g. `~/.baton-pass`).

---

## 🛠️ Commands

| Command | What it does |
| ------- | ------------ |
| `batonresume [claude\|codex] [project\|file]` | Relaunch into a fresh session from a handoff. With no second arg: newest for the current folder, else newest overall. Pass a project name (from anywhere) or a file path to target one. `batonresume --list` shows all. Run it after you exit the old session. |
| `baton extend <session_id> <value>` | Raise a session's threshold. |
| `baton disable <session_id>` | Silence all automatic guards for a session. |
| `baton reset <session_id>` | Clear a session's state. |
| `baton status` | Show enabled guards and fresh Claude 5-hour usage, or `unavailable`. |
| `baton enable/disable context/ quota` | Independently toggle automatic guards. |
| `baton config` | Interactive persistent configuration. |
| `baton check` | Lifecycle hook entry point; you don't normally call this by hand. |

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
