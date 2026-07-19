# Cross-Agent Installer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `./install.sh` and `./uninstall.sh` configure baton-pass for every detected Claude Code and Codex installation, while allowing users to select only one agent explicitly.

**Architecture:** Keep one shared binary and command installation under `~/.local/bin`. Add a small shell argument/detection layer that selects Claude, Codex, or both; install the skill into each selected agent's documented skill directory and merge the same Stop command into each agent's JSON hook file. Codex's hooks feature is enabled through `codex features enable hooks`; hook trust remains an explicit Codex user decision.

**Tech Stack:** Bash, Go 1.21+, JSON hook merger in `cmd/baton`, Codex CLI, shell integration tests.

---

## File map

- Modify `install.sh`: parse agent flags, detect installed agents, install both skill links, configure both hook files, and enable Codex hooks.
- Modify `uninstall.sh`: remove only baton-pass links/hooks from both agents without deleting shared user configuration.
- Modify `cmd/baton/main.go`: retain the Codex-specific Stop response already implemented and make hook command help text JSON-file-neutral.
- Modify `cmd/baton/main_test.go`: retain schema regression tests and cover Codex/default-mode instructions.
- Create `scripts/test-install.sh`: isolated-home installer/uninstaller integration tests.
- Modify `README.md`: replace Claude-first installation guidance with one cross-agent command and accurate Codex behavior.
- Modify `README.zh-CN.md`: keep the translated installation and Codex sections semantically aligned.

### Task 1: Preserve the Codex Stop-schema fix

**Files:**
- Modify: `cmd/baton/main.go`
- Test: `cmd/baton/main_test.go`

- [ ] **Step 1: Review the existing uncommitted compatibility diff**

Run:

```bash
git diff -- cmd/baton/main.go cmd/baton/main_test.go
```

Expected: Codex output contains only `decision` and `reason`; Claude retains `hookSpecificOutput`; Codex instructions do not require `AskUserQuestion`.

- [ ] **Step 2: Run the focused regression tests**

```bash
go test ./cmd/baton -run 'TestBuild(HookOutput|Instructions)' -v
```

Expected: both Codex regression tests pass.

- [ ] **Step 3: Update the CLI help comments to describe a generic hooks JSON file**

Change the `install-hook` and `uninstall-hook` descriptions from `settings.json` to `Claude settings.json or Codex hooks.json`. Do not change the JSON shape: both products use `hooks.Stop[].hooks[]`.

- [ ] **Step 4: Run the full Go suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit only the compatibility fix**

```bash
git add cmd/baton/main.go cmd/baton/main_test.go
git commit -m "fix: emit Codex-compatible Stop responses"
```

### Task 2: Specify installer selection behavior with shell tests

**Files:**
- Create: `scripts/test-install.sh`
- Modify: `install.sh`

- [ ] **Step 1: Write an isolated-home test harness**

The test must create `TEST_HOME="$(mktemp -d)"`, set `HOME="$TEST_HOME"`, provide temporary fake `claude` and `codex` executables, copy a valid empty hook fixture into each config directory, and clean up through a trap. Assertions must use `test`, `readlink`, and the built `baton` binary—never the developer's real home directory.

Cover these cases:

```text
both commands detected -> Claude and Codex links/hooks installed
--claude-only          -> only Claude configured
--codex-only           -> only Codex configured
neither detected       -> non-zero exit with an actionable flag hint
second install         -> no duplicate Stop entries
```

The fake `codex` executable must log `features enable hooks`; assert that exact invocation occurs.

- [ ] **Step 2: Run the new test and verify the detection cases fail**

```bash
bash scripts/test-install.sh
```

Expected: FAIL because `install.sh` currently configures Claude only and does not accept selection flags.

- [ ] **Step 3: Add installer argument parsing**

Support exactly these public forms:

```text
./install.sh                 configure every detected supported agent
./install.sh --claude-only   configure Claude only
./install.sh --codex-only    configure Codex only
./install.sh --help          print usage without modifying files
```

Reject unknown flags and mutually exclusive agent flags. Detect with `command -v claude` / `command -v codex`, while also treating existing `${CLAUDE_CONFIG_DIR:-$HOME/.claude}` and `${CODEX_HOME:-$HOME/.codex}` directories as installed.

- [ ] **Step 4: Install skills and hooks for each selected agent**

Use these destinations:

```text
Claude skill: ${CLAUDE_CONFIG_DIR:-$HOME/.claude}/skills/baton-pass/SKILL.md
Claude hooks: ${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json
Codex skill:  $HOME/.agents/skills/baton-pass -> repository directory
Codex hooks:  ${CODEX_HOME:-$HOME/.codex}/hooks.json
```

For each selected agent, call `"$BATON" install-hook "$HOOK_FILE"`. For Codex, run `codex features enable hooks` when the executable exists; otherwise print the exact manual `[features] hooks = true` instruction. Do not write a trust hash—tell users to inspect/approve the new hook with `/hooks` in their next Codex session.

- [ ] **Step 5: Run syntax and integration tests**

```bash
bash -n install.sh scripts/test-install.sh
bash scripts/test-install.sh
go test ./...
```

Expected: all commands pass, and the test reports every selection case as successful.

- [ ] **Step 6: Commit installer behavior**

```bash
git add install.sh scripts/test-install.sh
git commit -m "feat: install baton-pass for Claude and Codex"
```

### Task 3: Make uninstall symmetric and safe

**Files:**
- Modify: `uninstall.sh`
- Modify: `scripts/test-install.sh`

- [ ] **Step 1: Extend the integration test with uninstall assertions**

After installing both agents, run `./uninstall.sh` under the isolated home and assert:

```text
baton and batonresume command symlinks are removed
both baton skill links are removed
baton Stop entries are removed from both JSON files
unrelated Stop entries remain byte-semantically present
handoffs/ and state/ are untouched
```

- [ ] **Step 2: Run the test and verify it fails on Codex cleanup**

```bash
bash scripts/test-install.sh
```

Expected: FAIL because the current uninstaller only removes Claude configuration.

- [ ] **Step 3: Implement symmetric agent selection and cleanup**

Accept the same `--claude-only`, `--codex-only`, and auto-detection behavior as `install.sh`. Remove `~/.agents/skills/baton-pass` only if it is a symlink targeting this repository. Call `baton uninstall-hook` for both selected hook files. Never disable Codex's global hooks feature because other installed hooks may rely on it.

- [ ] **Step 4: Verify uninstall and idempotency**

```bash
bash -n uninstall.sh
bash scripts/test-install.sh
go test ./...
```

Expected: PASS, including a second uninstall with nothing left to remove.

- [ ] **Step 5: Commit uninstall behavior**

```bash
git add uninstall.sh scripts/test-install.sh
git commit -m "feat: uninstall baton-pass from Claude and Codex"
```

### Task 4: Rewrite installation documentation for both agents

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] **Step 1: Update the English README**

Make `./install.sh` the recommended path for both Claude Code and Codex. Document auto-detection, both explicit flags, destinations, backups, Codex `/hooks` approval, and restart requirements. Correct stale Codex statements: the feature key is `hooks`, the Stop block uses `decision: "block"` plus `reason`, and Codex Default mode receives a normal four-choice prompt rather than Claude's native picker.

- [ ] **Step 2: Update the Simplified Chinese README with the same behavior**

Keep commands, paths, flags, and schema names identical to the English README. Translate explanatory prose only.

- [ ] **Step 3: Scan for stale guidance**

```bash
rg -n 'codex_hooks|additionalContext|Claude Code — first-class|needs the hooks flag|~/.codex/skills' README.md README.zh-CN.md
```

Expected: no stale `codex_hooks`, Codex `additionalContext`, or `~/.codex/skills` guidance remains.

- [ ] **Step 4: Verify docs against the installer**

```bash
./install.sh --help
bash scripts/test-install.sh
go test ./...
```

Expected: README commands match help output and all tests pass.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: document cross-agent installation"
```

### Task 5: Final cross-agent smoke test

**Files:**
- No source changes expected

- [ ] **Step 1: Verify a disposable Claude configuration**

Run the installer with an isolated `HOME` and `--claude-only`, feed a representative Stop payload to the installed command, and assert the output retains Claude's `hookSpecificOutput.additionalContext`.

- [ ] **Step 2: Verify a disposable Codex configuration**

Run with an isolated `HOME` and `--codex-only`, assert the skill is a folder symlink under `.agents/skills`, then feed a real Codex rollout path at `BATON_THRESHOLD=1`. Assert `decision == "block"`, `reason` contains all four choices, and `hookSpecificOutput` is absent.

- [ ] **Step 3: Verify the repository and exclude unrelated files**

```bash
git status --short
git log -4 --oneline
```

Expected: only intended committed files are present; `.DS_Store` and `social-card-baton-pass/` remain untracked and are not added.
