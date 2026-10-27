---
name: just-talk-hotwords
description: Refresh a Just Talk hotwords_file from the agent sessions you are working in and the words Just Talk misheard lately. Run it on a loop with a config path, e.g. `/loop 10m /just-talk-hotwords ~/.config/just-talk/config.toml`.
---

# Just Talk 热词表

每轮把 Just Talk 的热词文件刷新成「眼下最可能说出口的词」。输入是一个 Just Talk 配置文件路径：`$ARGUMENTS`。没给路径就停下，说明用法，什么都不写。

热词按顺序发给语音识别模型，流式识别只收大约前 100 个 token，配置里的固定词排在最前面。所以这份文件是**一小撮**当前最相关的词，越重要越靠前，不是越攒越长的名单。

## 1. 读配置

从配置文件的 `[voice]` 取：

- `hotwords`：用户固定的词，只读，不要复制进热词文件。
- `hotwords_file`：要写的文件，`~` 是 home。为空就停下，告诉用户先在配置里设上它。

活动记录在 `${XDG_STATE_HOME:-~/.local/state}/just-talk/<配置文件名去掉扩展名>.activity.jsonl`，一行一个 `{"time","text"}`。

配置文件本身只读不写。

完成：拿到固定词、热词文件路径、活动记录路径。

## 2. 看最近在干什么

只看上一轮以来（约 10 分钟）的内容，排除这个 loop 自己的会话。会话里只看用户和助手的消息正文，跳过工具输出、系统提示和费用、状态一类的记录。

- **在 herdr 里**（`HERDR_ENV=1`）：`herdr agent list` 列出开着的 agent，跳过 `pane_id` 等于 `$HERDR_PANE_ID` 的那个（就是本 loop）。很多 agent 开着却闲了很久，只看 `agent_status` 为 `working`/`blocked`，或者会话最近 10 分钟有更新的。`agent_session.value` 是文件路径就读它的末尾；不是就按 `agent` 字段到下表找它的存储；表里没有（比如 Cursor）就用 `herdr agent read <pane_id> --source recent-unwrapped --lines 200` 读屏。
- **不在 herdr 里**：按下表把每家最近 10 分钟有更新的会话都读一遍，不存在的路径直接跳过。跳过当前工作目录对应的那个 Claude 项目目录，那是本 loop 自己。表里没有的 agent 这时读不到。

| agent                         | 会话在哪                                                                                                                 |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Claude Code                   | `~/.claude/projects/<cwd>/*.jsonl`                                                                                       |
| Codex                         | `~/.codex/sessions/<年>/<月>/<日>/*.jsonl`                                                                               |
| pi                            | `~/.pi/agent/sessions/--<cwd>--/*.jsonl`                                                                                 |
| omp                           | `~/.omp/agent/sessions/<cwd>/*.jsonl`                                                                                    |
| Droid                         | `~/.factory/sessions/<cwd>/*.jsonl`                                                                                      |
| Copilot CLI                   | `~/.copilot/session-state/<id>/events.jsonl`                                                                             |
| Gemini CLI                    | `~/.gemini/tmp/<hash>/chats/` 下的 json                                                                                  |
| Kimi                          | `~/.kimi-code/sessions/`（旧版 `~/.kimi/sessions/`），每个会话一个目录                                                   |
| Hermes                        | `~/.hermes/state.db` 表 `messages`（`role`、`content`、`timestamp`）；profile 在 `~/.hermes/profiles/*/state.db`         |
| Devin                         | `~/.local/share/devin/cli/sessions.db` 表 `message_nodes`（`chat_message` 是带 `role`、`content` 的 JSON，`created_at`） |
| OpenCode（含 oh-my-opencode） | `~/.local/share/opencode/opencode.db`：`message.data` 的 `$.role`，`part.data` 的 `$.text`；旧版是 `storage/` 下的 json  |

SQLite 一律用 `sqlite3 -readonly` 读，时间列按数值大小判断是秒还是毫秒。

完成：每个活跃会话都读过，或者确认没有活跃会话。

## 3. 找听错的词

读活动记录里这段时间以 `🎤` 开头的行，那是 Just Talk 的识别结果。找在上下文里说不通、但读音像某个上下文术语的词：比如上下文在聊 skill，识别结果里出现「Skia」「Scare」，说明 `skill` 该进表。

完成：列出这一轮发现的误识别及其正确写法，没有就是空。

## 4. 定新词表

候选来自第 2、3 步，加上热词文件里现有的词。值得进表的是 ASR 容易认错的词：项目名、工具和库名、标识符、人名、中文里夹着说的英文术语。第 3 步发现的误识别优先。

不收：

- 已经在固定词里的词。
- 常用词：ASR 本来就认得准。
- 任何像密钥、token、哈希、内网地址、文件路径、邮箱的东西：热词会随每次识别发到云端。

按和当前工作的相关程度排序，保留大约 20 个。现有的词如果这一轮上下文里还在出现就留着，不再出现的让位给新词。

完成：一个排好序的词表，每个词都能在这一轮的上下文或误识别里找到依据。

## 5. 写回

新词表与文件现有内容逐行比较：完全一样就不动文件，因为每次改写都可能让正在进行的持续转写重连一次。不一样就在同目录写临时文件，再 `mv` 覆盖过去，让 Just Talk 不会读到半份文件。文件第一行写注释 `# 由 just-talk-hotwords 生成；要固定的词写进配置的 [voice] hotwords`，之后一行一个词。

完成：用一行汇报这一轮加了什么、去了什么，或者「无变化」。
