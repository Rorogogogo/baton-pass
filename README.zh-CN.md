<div align="center">

# 🏃 baton-pass

**在上下文喘不过气之前，把接力棒传出去。**

[English](./README.md) | **简体中文**

一个 Stop 钩子 + 技能（skill），它会监控 agent 的上下文体积，当上下文变得
"昂贵"时，把工作交接给一个*全新*的会话——这样你就不必在每一轮对话里都
重新发送一份庞大的对话记录、为此持续付费。它是一个零依赖的单文件
**Go** 二进制——不需要安装 Python、Node 或 `jq`。

![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8?logo=go&logoColor=white)
![No runtime deps](https://img.shields.io/badge/runtime%20deps-none-success)
![skills.sh](https://img.shields.io/badge/install-skills.sh-black)
![agents](https://img.shields.io/badge/agents-Claude%20Code%20·%20Codex%20·%20Cursor-blue)

</div>

---

## 🧠 为什么需要它

Agent 对话的每一轮都会把**整个上下文**重新发送给模型。
随着会话变长，会发生两件糟糕的事：

1. **每一轮的成本都在上涨。** 一个停在 190K tokens 的对话，*每次回复*都要
   为约 190K 的上下文付费——直到你停下来为止。
2. **自动压缩（auto-compaction）会触发。** 接近窗口上限时，你的 agent 会悄悄
   把历史压缩成一份有损的、不是你写的、也无法审阅的摘要。

`baton-pass` 同时解决这两个问题。当你越过阈值时，它会主动提议写一份干净的
**交接文档（handoff document）**，并在一个全新会话里重启，只用这份文档作为
"种子"——把你的上下文从*巨大*重置回*极小*。

> 🏁 把它想象成接力赛：每个会话跑完自己这一棒，然后把接力棒（交接文档）
> 传给一个全新的跑者，而不是拖着整条跑道一起跑。

---

## 💰 到底能省多少？

一轮对话的重复成本随上下文体积而增长。开启提示缓存（prompt caching）后，一次
缓存命中的重新读取大约只花上下文 token 价格的 **10%**——但你*每一轮*都要付这笔钱。
真正省钱的，是重置上下文。

**示意场景** —— 你已经到了 **190K tokens**，但工作还没做完：

| 剩余工作量 | 不交接 | 交接后* | 你能省下 |
| -------------- | --------------- | ------------- | -------- |
| 再跑 40 轮  | ~760K token 等效 | ~230K token 等效 | **~70%** |
| 再跑 100 轮 | ~1.9M token 等效 | ~290K token 等效 | **~85%** |

<sub>\* "交接后" = 一次性的约 190K token 摘要生成，之后是一个全新的约 10K
上下文，每轮按约 10% 的缓存读取计费。数字均为 token 等效值且为近似——实际节省
取决于模型定价、缓存命中率和输出体积。**你越是越过阈值后继续工作，赢得越多。**</sub>

而且成本这本账还*低估*了它：在自动压缩**之前**就交接，也能保住质量——
一份你能读、能改的有意交接，胜过一份悄无声息、有损的自动摘要。

---

## ⚙️ 工作原理

```
Stop 钩子（每轮结束后）
  ├─ 读取上下文体积 + 本会话的阈值          （免费——只读对话记录）
  ├─ 本会话已禁用？               → 什么都不做
  ├─ 未达到阈值？                 → 什么都不做
  └─ 超过阈值？ → 一行提示 + 一个原生 ↑/↓ 选择器：
        1. 立即交接（Handoff now） → 写交接文档，然后：退出 + `batonresume`
        2. 延长 +10K（Extend）     → 提高本会话的阈值，继续
        3. 禁用本对话（Disable）   → 本会话不再询问，继续
        4. 跳过（Skip）            → 继续，下一轮再问
```

选择"立即交接"后：退出会话（/exit），再运行 `batonresume` → 启动一个以交接
文档作为开场提示的全新会话。Agent（claude / codex）会被自动识别，所以它会
建议对应的命令。

`batonresume` 会替你找到正确的交接文档，所以你永远不用粘贴一段又长、又含空格
的路径：

```
batonresume                       # 当前目录下最新的交接，
                              #   若此处没有则取全局最新
batonresume claude baton-pass  # 从任意位置按项目名定位
batonresume --list                # 列出所有已保存的交接，最新在前
```

几个有意为之的设计点：

- **检查是免费的**——它只读取对话记录文件里已经记录好的 token 数；不调用模型，
  不花 token。
- **对话记录保持干净**——只显示一行提示；所有细节（选项、确切命令）都走一条
  静默的 `additionalContext` 通道，驱动原生选择器，而不会弄乱你的聊天。
- **没有过期的交接**——恢复时把文档放在**开场提示**里传入（而不是通过
  session-start 钩子），所以一个无关的新会话绝不会继承到它。

---

## 🚀 安装

### ✨ AI 原生方式（推荐）

别自己动手接线——让你的 agent 来做。在你想安装的目录里，把下面这段粘贴给
Claude Code、Codex 或 Cursor：

```
帮我安装 baton-pass。

1. 克隆 https://github.com/Rorogogogo/baton-pass（或告诉我它在哪）。
2. 运行它的 ./install.sh（构建 `baton` 二进制，或下载预编译版）。
3. 把它的 bin/ 目录加入我的 PATH（追加到我的 shell rc）。
4. 把它 settings.example.json 里的 Stop 钩子注册到「我的」agent 配置中：
     - Claude Code → ~/.claude/settings.json  （Stop 钩子）
     - Codex       → ~/.codex/config.toml      （[hooks]，Stop 事件，codex_hooks=true）
     - Cursor      → 我的 hooks.json            （stop 事件）
5. 把它的 SKILL.md 软链接到我的 skills 目录。
6. 然后告诉我怎么用 `batonresume`。

先读这个仓库的 README，确认步骤，然后再执行。
```

这正是 agent 存在的意义——它能读这个仓库并自我安装。

### 🟣 Claude Code —— 一等公民（已测试）

**一条命令：**

```sh
git clone https://github.com/Rorogogogo/baton-pass && cd baton-pass
./install.sh
```

`install.sh` 会构建单一的 `baton` 二进制（若你没有 Go 工具链，则下载预编译版），
把技能以及 `baton` / `batonresume` 命令软链接到你的 PATH 上，并把 Stop 钩子
**合并**进你的 `~/.claude/settings.json`——保留所有已有钩子，把文件备份到
`settings.json.bak`，若已存在则跳过。重启 Claude Code 加载它。随时可用
`./uninstall.sh` 撤销。

> 🪶 **没有运行时依赖。** `baton` 是一个自包含的 Go 二进制——不需要 Python、
> Node 或 `jq`。唯一的可选依赖是*安装时*的 Go 工具链，用来从源码构建；没有它，
> `install.sh` 会从 Releases 下载一个预编译的静态二进制。

<details>
<summary>想手动接线？</summary>

```sh
go build -o bin/baton ./cmd/baton        # 或从 Releases 下载 bin/baton
ln -s "$PWD/bin/batonresume" ~/.local/bin/batonresume
ln -s "$PWD/bin/baton"       ~/.local/bin/baton
mkdir -p ~/.claude/skills/baton-pass
ln -s "$PWD/SKILL.md" ~/.claude/skills/baton-pass/SKILL.md
```

然后加到 `~/.claude/settings.json`（参见 `settings.example.json`）：

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

### 🟢 OpenAI Codex CLI —— 一等公民（需要钩子开关）

Codex 有兼容的 `Stop` 钩子（实验性）。在 `~/.codex/hooks.json` 里把
`baton check` 注册到 Stop 事件上（并在 `~/.codex/config.toml` 里开启该特性）：

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

> ✅ **自动触发在 Codex 上同样有效。** Codex 的 Stop 负载使用与 Claude 相同的
> 字段（`session_id`、`transcript_path`、`cwd`、`stop_hook_active`），也同样
> 支持 `decision: "block"` + `additionalContext`。`baton check` 原生读取 Codex
> 的 rollout `token_count` 事件（用 `info.last_token_usage.input_tokens` 作为
> 实时上下文体积），所以同一个二进制驱动完整的 监控 → 交接 → `batonresume codex`
> 流程。提示：Codex 的窗口大于 200K，记得相应调高 `BATON_THRESHOLD`。

### 🔵 Cursor —— 已支持

Cursor 1.7+ 有一个 `stop` 钩子，采用同样的 stdin-JSON 模型。按
[Cursor 钩子文档](https://cursor.com/docs/hooks)把脚本注册到你的 Cursor
`hooks.json` 里。与 Codex 相同的对话记录适配器注意事项同样适用。

### 🟡 Antigravity CLI 及其它一切

这个**技能**是跨 agent 的（skills.sh 把同一个技能推广到 Claude Code、Codex、
Cursor、Gemini、Cline 等等）：

```sh
npx skills add Rorogogogo/baton-pass
```

`npx skills` 只安装**技能本身**——足以**手动**执行交接并使用 `batonresume`。
自动的上下文监控需要那个 agent 自带的 Stop 钩子机制；如果它还没有，就在会话
变重时手动运行这个技能。

---

## 🔧 配置

通过环境变量设置（在你的 shell 里，或写在钩子命令上）：

| 变量                    | 默认值      | 含义                                                          |
| --------------------------- | ------------ | ---------------------------------------------------------------- |
| `BATON_THRESHOLD`   | `190000`     | 基础上下文阈值（tokens，约为 200K 窗口的 95%——在自动压缩前触发）。 |
| `BATON_EXTEND_STEP` | `10000`      | "延长"每次增加多少（当前值 + 步长）。                         |
| `BATON_DATA`        | 仓库目录 | `state/` 和 `handoffs/` 的存放位置。                             |
| `BATON_TOOL`        | 自动识别  | 强制指定恢复目标（`claude`/`codex`）。否则从 `$AI_AGENT`/`$CLAUDECODE` 自动识别。 |

---

## 📂 数据布局

```
handoffs/<project-name>/handoff-<YYYYMMDD-HHMM>.md   # 每次交接一份，保留历史
state/<session_id>.json                              # { threshold_override, disabled }
```

`<project-name>` 是工作目录的 basename，所以不同项目的交接彼此分开、可审计。
两个文件夹都被 git 忽略——只是本地运行时数据，永不提交。

> 本地情况下，`BATON_DATA` 默认指向这个仓库目录，让所有东西都集中在一处。
> 如果你装到一个只读/受管的位置，请把它指向一个可写目录（例如 `~/.baton-pass`）。

---

## 🛠️ 命令

| 命令 | 作用 |
| ------- | ------------ |
| `batonresume [claude\|codex] [project\|file]` | 从交接文档重启进入一个全新会话。不带第二个参数时：取当前目录最新，否则取全局最新。传项目名（可从任意位置）或文件路径来定位某一份。`batonresume --list` 列出全部。在退出旧会话后运行它。 |
| `baton extend <session_id> <value>` | 提高某个会话的阈值。 |
| `baton disable <session_id>` | 让 baton-pass 对某个会话保持沉默。 |
| `baton reset <session_id>` | 清除某个会话的状态。 |
| `baton check` | Stop 钩子本身（从 stdin 读取钩子负载）；你不需要手动调用它。 |

---

## 🙏 致谢

交接**技能**（`SKILL.md`——交接文档如何撰写）改编自
[Matt Pocock 的 `handoff` 技能](https://github.com/mattpocock/skills/tree/main/skills/productivity/handoff)
（MIT）。`baton-pass` 把这个想法接入了一套自动的、由成本驱动的工作流：一个
零依赖的 [Go](https://go.dev) Stop 钩子，跨 Claude Code 和 Codex 监控上下文
体积，再加上 `batonresume` 来重启全新会话。谢谢 Matt！🎩

## 📄 许可证

[MIT](./LICENSE) © 2026 Robert Wang。
`SKILL.md` 的部分内容 © 2026 Matt Pocock，依 MIT 许可证使用——参见 [LICENSE](./LICENSE)。
</content>
</invoke>
