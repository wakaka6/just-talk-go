---
name: just-talk-hotwords
description: Refresh a Just Talk hotwords_file from the words you fix by hand after dictating, the words Just Talk misheard lately, and the agent sessions you are working in. Run it on a loop with a config path, e.g. `/loop 10m /just-talk-hotwords ~/.config/just-talk/config.toml`.
---

# Just Talk 热词表

每轮把 Just Talk 的热词文件刷新成「眼下最可能说出口、又最容易听错的词」。输入是一个 Just Talk 配置文件路径：`$ARGUMENTS`。没给路径就停下，说明用法，什么都不写。

热词按顺序发给语音识别模型，流式识别只收大约前 100 个 token，配置里的固定词排在最前面。所以这份文件是**一小撮**当前最相关的词，越重要越靠前，不是越攒越长的名单。

## 1. 读配置

从配置文件的 `[voice]` 取：

- `hotwords`：用户固定的词，只读，不要复制进热词文件。
- `hotwords_file`：要写的文件，`~` 是 home。为空就停下，告诉用户先在配置里设上它。

配置文件本身只读不写。

完成：拿到固定词和热词文件路径。

## 2. 对照听写和发出的消息

运行与本文件同目录的 `corrections.py`：

```bash
python3 <本 skill 目录>/corrections.py <配置文件路径>
```

它读活动记录（`${XDG_STATE_HOME:-~/.local/state}/just-talk/<配置名>.activity.jsonl`），取出上一轮以来每次听写最终上屏的文本，再到 Claude Code、Codex、pi、omp、Droid、Copilot CLI 的会话里找用户随后发出的那条消息，逐字比对。输出：

- `# window`：本轮覆盖的时间段。脚本会记住进度（同目录的 `<配置名>.hotwords-cursor`），每轮只看新的，所以**一轮只跑一次**。最近 3 分钟的听写留到下一轮，用户可能还在改那条消息。
- `# gap`：活动记录有条数上限，上一轮之后的一部分已经被挤掉了。在汇报里提一句「loop 间隔太长」。
- 每次听写一段：`heard` 是识别结果；`sent to` 是它最终进了哪个会话，没找到就是 `(no matching message)`；`fixed` 是用户手动改掉的部分，`识别结果 → 用户写法`。
- `# sessions updated`：上一轮以来更新过的会话文件。

没有任何听写，`# sessions updated` 里也只剩这个 loop 自己的会话，就回一行「无变化」，到此结束。

完成：拿到本轮的听写、修正和更新过的会话。

## 3. 看最近在干什么

只看本轮窗口内的内容，排除这个 loop 自己的会话：**包含 `/just-talk-hotwords` 调用的那个会话就是自己**。不要按 pane 或目录判断：`$HERDR_PANE_ID` 不一定出现在 `herdr agent list` 里，同一个目录下也可能开着别的 agent。会话里只看用户和助手的消息正文，跳过工具输出、系统提示和费用、状态一类的记录。

- **在 herdr 里**（`HERDR_ENV=1`）：`herdr agent list` 列出开着的 agent。只看 `agent_status` 为 `working`/`blocked`，或者会话在本轮窗口内有更新的。`agent_session.value` 是文件路径就读它的末尾；不是就按 `agent` 字段到下表找它的存储；表里没有（比如 Cursor）就用 `herdr agent read <pane_id> --source recent-unwrapped --lines 200` 读屏。
- **不在 herdr 里**：读 `# sessions updated` 里的会话，再按下表补上脚本不读的几家（Gemini、Kimi、Hermes、Devin、OpenCode），不存在的路径直接跳过。

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

## 4. 找听错的词

按证据强弱分两类：

1. **用户改过的**：第 2 步的 `fixed`。这是用户亲手给出的正确写法，最可靠。但要过滤掉不是在纠正读音的改动：改语气或改虚字（`但 → 像`）、顺手补的内容，以及看着像地址、路径的东西（比如 `127.0.0.1:59000`，那是粘贴进来的）。改成的是常用词（`和新 → 核心`），说明 ASR 在这个上下文里会认错；但常用词本身不进表，要看它所在的专名或短语里有没有值得收的词。
2. **用户没改、但说不通的**：其余的 `heard`，以及没对上任何消息的听写。找在上下文里说不通、但读音像某个上下文术语的词：比如上下文在聊会话，识别结果里出现「绘画」，说明 `会话` 该进表。

完成：列出这一轮发现的误识别及其正确写法，没有就是空。

## 5. 定新词表

候选来自第 3、4 步，加上热词文件里现有的词。只收两类：

- 有误识别证据的词（第 4 步）。用户改过的排最前。
- ASR 不认识的专名：项目名、工具和库名、标识符、人名、中文里夹着说的英文术语。

不收：

- 已经在固定词里的词，大小写不同也算重复。同一个词的大小写变体只留一个。
- 通用词，中文的尤其如此：「编译」「测试」「还原」「边界」「占位符」这类词 ASR 本来就认得准，进表只会占掉名额。没有误识别证据的通用词一律不收。
- 任何像密钥、token、哈希、地址、文件路径、邮箱的东西：热词会随每次识别发到云端。

按和当前工作的相关程度排序，最多 20 个左右；够用就行，不要为了凑数加词。现有的词如果这一轮上下文里还在出现就留着，不再出现的让位给新词。用户改过的词只要还和当前工作沾边，就一直留着。

完成：一个排好序的词表，每个词都能在这一轮的上下文或误识别里找到依据。

## 6. 写回

新词表与文件现有内容逐行比较：完全一样就不动文件，因为每次改写都可能让正在进行的持续转写重连一次。不一样就在同目录写临时文件，再 `mv` 覆盖过去，让 Just Talk 不会读到半份文件。文件第一行写注释 `# 由 just-talk-hotwords 生成；要固定的词写进配置的 [voice] hotwords`，之后一行一个词。

用户又一次改了一个已经在热词文件里的词，说明光放在文件里还不够：在汇报里建议把它移进配置的 `[voice] hotwords`。配置文件仍然由用户自己改。

完成：用一行汇报这一轮加了什么、去了什么（或者「无变化」），有建议固定的词就附上。
