---
name: just-talk-stream
description: Consume Just Talk's live transcription as a subprocess. `just-talk subscribe` prints every recognition frame as NDJSON on stdout, with a ready marker and exit reasons on stderr, and `--timeout` / `--max-events` bounds. Use when the user asks to watch, capture, or process continuous transcription results in real time (meetings, room audio), to check whether the transcription service is listening, or to feed the stream into scripts and agents.
---

# Just Talk 实时转写订阅

Just Talk 是本地应用：持续转写把每一条识别结果通过本机 WebSocket 推给订阅者。别的程序不必自己实现 WebSocket 客户端——`just-talk subscribe` 就是那个订阅者，按子进程契约工作，与 `lark-cli event consume`、omp 的 RPC 模式同一家族。

## 命令

```bash
just-talk subscribe                        # 一直读到服务停止或 Ctrl+C
just-talk subscribe --max-events 1         # 取一帧就退（探活）
just-talk subscribe --timeout 10m         # 读 10 分钟后自己退
just-talk subscribe --addr 127.0.0.1:7590  # 指定地址；默认读配置里的 stream_addr
just-talk subscribe --config ~/.config/just-talk/config.toml
```

地址也可以写 `unix:<路径>`（0600，只有文件主能连）。`--timeout` 和 `--max-events` 同时给时先到的生效。

## 契约

- **stdout**：一行一个 JSON 帧，原样透传，无前缀无包装。别往 stdout 打诊断信息。
- **stderr**：连上后先出一行 `[subscribe] ready addr=…`，父进程等这一行再开始读 stdout，不要 sleep。退出时最后一行 `[subscribe] exited — received N frame(s) in Xs (reason: …)`。
- **reason**：`limit`（够了 N 帧）、`timeout`（到了时限）、`signal`（收到 SIGTERM/Ctrl+C）、`closed`（服务停了，连接被关）。四种都退出码 0，算正常完成。
- **退出码 1** 只表示连接失败：多半是持续转写没开——订阅跟着持续转写走，它不跑就没有东西可推。错误信息里带地址。
- stdin 完全不读，不用喂。

## 帧

| type | 何时出现 | 关键字段 |
|---|---|---|
| `service` | 连上时，和服务状态变化时 | `state`：`listening` / `idle`；`device`、`engine`、`gate` |
| `session` | 每个转写会话的开始与结束 | `state`：`start` / `end`；`session`；end 带 `reason`：`last` / `error` / `closed` |
| `asr` | 每条识别结果 | `text`（本会话累计全文）、`utterances[]`（每句 `text` + `definite` 是否已定稿）、`final` |

只推持续转写的结果；用户自己的听写不进这条流。

## 常用做法

探活（区分「在听没人说话」和「没在跑」）：

```bash
just-talk subscribe --max-events 1
```

拿到 `service` 帧就说明服务在监听；`state: listening` 说明采集已开。

读一场会的话，只取定稿句子：

```bash
just-talk subscribe --timeout 30m | jq -r 'select(.type=="asr") | .utterances[]? | select(.definite) | .text'
```

要原始流就重定向：`just-talk subscribe --timeout 1h > meeting.ndjson`。
