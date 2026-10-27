<p align="center">
  <img src="docs/assets/brand/icon-d.svg" alt="Just Talk" width="96">
</p>

# Just Talk

[中文](README.md) · [English](README.en.md)

[![Release](https://img.shields.io/github/v/release/wakaka6/just-talk-go?label=release)](https://github.com/wakaka6/just-talk-go/releases)
[![Release workflow](https://github.com/wakaka6/just-talk-go/actions/workflows/release.yml/badge.svg)](https://github.com/wakaka6/just-talk-go/actions/workflows/release.yml)
[![Go version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![Homebrew](https://img.shields.io/badge/Homebrew-wakaka6%2Ftap-FBB040?logo=homebrew&logoColor=white)](https://github.com/wakaka6/homebrew-tap)

减少用键盘的次数，改用口喷吧。

Just Talk 是一个面向桌面环境的语音输入工具。它通过全局快捷键录音，把语音流式发送到豆包大模型 ASR，识别结果自动复制到剪贴板，或直接上屏到当前输入框——适合写代码、聊天、记笔记和处理长文本输入。

![Just Talk TUI](docs/screenshot-tui.png)

## macOS 刘海浮层

在 macOS 上，录音提示直接从刘海展开：实时识别文字随语音流式显现（打字机式逐段展开），旁边同步显示麦克风音量波形；识别完成后停留展示最终文字与“已复制 / 已上屏”状态。

![macOS 实机刘海浮层：录音中的实时识别文字与音量波形](docs/assets/notch-live.gif)

![macOS 实机刘海浮层：识别完成后的已复制状态](docs/assets/notch-live-final.png)

屏幕没有刘海（外接显示器或无刘海机型）时，浮层从屏幕顶部中央以同样的形态展开，同样显示实时文字和音量波形。通过 `position = "notch"` 或 TUI 的“提示位置”启用，行为细节见 [配置](#配置)。

## 功能

- 全局快捷键录音，支持 `toggle`（按一下开始、再按一下停止）和 `hold`（按住说话）两种模式。
- 语音热键限定为适合作为全局快捷键的按键：纯修饰键组合、功能键、Tab、CapsLock、方向键和导航键等；不支持字母、数字、标点、空格等普通文本键。
- 豆包大模型流式 ASR（双向流优化版 + 二遍识别），支持热词增强。
- 识别结果自动复制到剪贴板，可选自动上屏到焦点输入框。
- 持续转写服务：与听写并行，把会议、系统声音等外部音源按说话声切句转写，可用热键开关；结果通过本机 WebSocket 推给订阅程序，不上屏、不写剪贴板。
- 录音状态浮层：macOS 默认从刘海展开，显示实时识别文字、麦克风音量和完成结果；Linux（Wayland / X11）和 Windows 使用置顶状态胶囊。
- 桌面设置窗口 + 菜单栏/托盘图标：热键（听写的主热键/第二热键和持续转写的开关热键，点击插槽后直接按下快捷键录制，每个都可清空）、模式、识别引擎与凭据、输出去向、停止延迟、热词、持续转写、识别结果订阅、提示位置等，保存后即时生效，无需重启；同一页面也可用 `--web` 在浏览器中打开，终端用户仍可用 `--tui`。
- 录音历史统计：累计次数、总字数、平均速度和最近速度。
- 录音中 `Esc` 取消，可重试错误时 `R` 重试。

## 平台支持

| 平台 | 状态 | 说明 |
| --- | --- | --- |
| Linux Wayland | 已支持 | 已验证 Sway / wlroots；快捷键基于 evdev，需要 `input` 组权限；剪贴板/上屏依赖 `wl-clipboard` + `wtype` 或 `/dev/uinput` |
| Linux X11 | 已支持 | 原生 X11 全局热键与浮层，XTest 自动上屏 |
| macOS | 已支持 | CGEventTap 热键、CoreAudio 录音、NSPasteboard 剪贴板、AppKit NSPanel 浮层；需要辅助功能和麦克风权限 |
| Windows 10/11 | 已支持 | 全局按键轮询 + 低级键盘钩子边沿回退、WinMM 录音、Unicode 剪贴板、SendInput 上屏、Win32 置顶浮层 |

设置窗口使用系统 webview：Linux 版本链接 GTK 3 + WebKitGTK 4.1，运行前需安装其运行库（Debian/Ubuntu 的 `libwebkit2gtk-4.1-0`、Arch 的 `webkit2gtk-4.1`），只用 TUI 时可自行用 `-tags nogui` 构建；Windows 使用 WebView2（Windows 11 自带），缺失时自动回退到 TUI 并提示安装地址。

## 安装

### Homebrew（macOS / Linux）

```bash
brew install wakaka6/tap/just-talk
```

来源：[wakaka6/homebrew-tap](https://github.com/wakaka6/homebrew-tap)。Formula 覆盖 macOS（Apple Silicon / Intel）和 Linux（amd64 / arm64）。

### 预编译二进制

<details>
<summary>展开查看各平台归档下载与校验说明</summary>

从 [GitHub Releases](https://github.com/wakaka6/just-talk-go/releases) 下载对应平台的归档，解压后将 `just-talk` 放入 `PATH`（如 `~/.local/bin`）。Windows 提供 `.zip`，其他平台为 `.tar.gz`，可用归档中的 `SHA256SUMS.txt` 校验。

当前提供：Linux amd64 / arm64、macOS Intel / Apple Silicon、Windows amd64 / arm64。

</details>

### 源码构建

<details>
<summary>展开查看构建依赖与安装步骤</summary>

Just Talk 依赖平台原生能力：Linux 和 macOS 需要启用 cgo；Windows 使用纯 Go 调用 Win32，不需要 cgo。桌面窗口基于 [Wails v3](https://github.com/wailsapp/wails)，使用系统自带的 webview：macOS 为 WKWebView，Windows 为 WebView2，Linux 为 GTK 3 + WebKitGTK 4.1。

Linux 构建依赖：

```bash
# Arch Linux
sudo pacman -S --needed go gcc libx11 libxtst libxext wayland gtk3 webkit2gtk-4.1

# Debian / Ubuntu
sudo apt install golang-go build-essential libx11-dev libxtst-dev libxext-dev libxinerama-dev libwayland-dev libgtk-3-dev libwebkit2gtk-4.1-dev
```

macOS 构建依赖 Apple Command Line Tools（提供 clang 和 macOS SDK，不需要完整 Xcode）：

```bash
xcode-select --install
```

Windows 只需要 Go 1.25 或更高版本，不需要 ffmpeg、SoX 或 C 编译器：

```powershell
winget install --id GoLang.Go --exact
```

构建：

```bash
# macOS
CGO_ENABLED=1 go build -o build/just-talk ./cmd/just-talk
# Linux（桌面窗口使用 GTK 3 + WebKitGTK 4.1）
CGO_ENABLED=1 go build -tags gtk3 -o build/just-talk ./cmd/just-talk
# Windows PowerShell：
# go build -o build\just-talk.exe .\cmd\just-talk
```

不需要桌面窗口时可加 `-tags nogui`：构建产物不链接 webview，无参数启动时进入 TUI。

安装到用户目录（Linux/macOS 为 `~/.local/bin`，Windows 为 `%LOCALAPPDATA%\Programs\Just Talk`）：

```bash
build/just-talk --install
# 或
make install
```

如提示安装目录不在 `PATH` 中，按命令输出的说明添加即可。

</details>

## 快速开始

1. 获取豆包大模型语音识别凭证（App Key / Access Key）。
2. 运行 `just-talk --doctor` 检查麦克风、剪贴板工具和权限。
3. 运行 `just-talk` 打开设置窗口，在“识别引擎”中选择渠道（默认豆包流式）并填入凭证，点击“保存”（或 `Cmd/Ctrl+S`）。关闭窗口不会退出。左键点击菜单栏/托盘图标会弹出小面板，显示当前状态、最近一次识别结果、使用统计和当前配置，可直接用“语音输入”和“持续转写”两个开关启用或停用，面板里的“打开设置”可回到设置窗口；右键是托盘菜单。退出请用面板或托盘菜单中的“退出”。习惯终端可用 `just-talk --tui`。
4. 按 `push_to_talk` 热键（默认 `Alt+Super`；macOS 为 `Option+Command`）开始/停止录音。识别结果送到哪里由 `text_output` 决定，默认上屏到当前输入框。

### 权限

<details>
<summary>展开查看各平台权限要求</summary>

- **Linux Wayland**：全局热键通过 evdev 读取 `/dev/input/event*`，需要把当前用户加入 `input` 组后重新登录：

  ```bash
  sudo usermod -aG input $USER
  ```

  剪贴板需要 `wl-clipboard`；自动上屏需要 `wtype`，或授予 `/dev/uinput` 写权限（KDE Plasma 推荐 uinput，`--doctor` 会打印完整配置步骤）。

- **macOS**：在“系统设置 → 隐私与安全性”中为启动 Just Talk 的终端应用（Terminal、iTerm2 等）授予 **辅助功能** 和 **麦克风** 权限。不需要 `.app` 签名或完整 Xcode。
- **Windows**：无需管理员权限。若麦克风不可用，在“设置 → 隐私和安全性 → 麦克风”中允许桌面应用访问麦克风。

</details>

## 快捷键

### 语音热键

| 按键 | 行为 |
| --- | --- |
| 配置的 `push_to_talk` | `toggle` 模式下开始/停止录音；`hold` 模式下按住说话 |
| 配置的 `[voice.continuous] hotkey` | 打开/关闭持续转写服务（只切换，不写配置），见 [持续转写](#配置) |
| `Esc` | 取消当前录音或错误提示 |
| `R` | 出现可重试错误时重新识别 |

语音热键只支持适合作为全局快捷键的按键：

- 纯修饰键组合：`Alt+Super`、`Ctrl+Alt+Shift`、`Option+Command` 等。
- `Fn`（macOS / Linux）：可写作 `Function`，可与普通键组合，但不能与 `F1`–`F24` 组合。Linux 上 `Fn` 通过 evdev 读取；配置了 `Fn` 的热键在未指定后端时自动选择 Wayland/evdev 后端。
- 功能键 `F1`–`F24`，如 `F9`、`Alt+F8`。
- 非文本控制键和导航键：`Tab`、`Enter`、`Escape`、`Backspace`、`CapsLock`、方向键、`Home`、`End`、`PageUp`、`PageDown`、`Insert`、`Delete`。
- PC 键盘右 `Ctrl` 左边的菜单键：`Menu`（也可写 `Apps`、`Application`）。
- 不支持：`Fn+F1`–`Fn+F24`，以及不带 `Fn` 的字母、数字、标点、`Space`、小键盘键等会输入文本的按键（如 `Alt+G`、`G`、`Alt+1`、`Alt+Space`）。

Windows 上低级键盘钩子只观察按键边沿，不消费、不回放修饰键，因此单独的 `Alt`、`Super` 和 `Alt+Tab` 等系统快捷键保持原有行为。

`push_to_talk` 可以配多个热键，用逗号分隔，例如 `push_to_talk = "Fn, Menu"`：内置键盘按 `Fn`，外接 PC 键盘按菜单键（Print Screen 在 macOS 上识别为 `F13`，也可以用）。任一热键都能开始/停止录音；`hold` 模式下要等所有按住的热键都松开才停止。逗号键本身写在 `+` 后面（如 `Fn+,`）时仍是按键。罗技等第三方键盘的 `Fn` 由键盘固件处理、不会发给系统，无法作为热键。

### TUI 快捷键

使用 `just-talk --tui` 进入 TUI。

| 按键 | 行为 |
| --- | --- |
| `j` / `k` / 方向键 | 移动选中项 |
| `e` / `i` / `Enter` | 编辑选中项（字符串字段进入输入；选项字段用 `j`/`k` 切换；布尔字段用 `Space` 切换）；“引擎”/“引擎配置”行回车打开弹层 |
| `Enter` | 退出编辑并保存；`Esc` 仅退出编辑 |
| `s` | 保存全部配置（即时生效，无需重启） |
| `l` | 展开/收起日志区域 |
| `h` | 帮助开关 |
| `q` / `Ctrl+C` | 退出 |

在设置窗口或 TUI 中保存引擎或引擎凭证后，下一次录音即使用新配置；正在进行的识别仍使用建立连接时的配置。

## 配置

<details>
<summary>展开查看配置文件路径与推荐配置</summary>

默认配置路径：

```text
# Linux / macOS
~/.config/just-talk/config.toml

# Windows
%APPDATA%\just-talk\config.toml
```

同时会优先读取当前目录下的 `./config.toml` 和 `$XDG_CONFIG_HOME/just-talk/config.toml`。

推荐配置：

```toml
[voice]
enabled = true
mode = "toggle"                    # 热键怎么开始和结束一次听写：toggle | hold
push_to_talk = "Alt+Super"
language = "zh-CN"
text_output = "paste"              # 识别结果送到哪里：paste 上屏 / clipboard 仅剪贴板 / none 不输出
stop_delay_ms = 0                  # hold 模式松开后的停止延迟
device = ""                        # 录音设备（留空使用默认；macOS 按设备 UID 或名称匹配）
stream_addr = "127.0.0.1:7590"     # 订阅地址：回环 host:port 或 "unix:~/.../asr.sock"，持续转写开着时才监听
gain = 0                           # 麦克风增益
app_key = "your-app-key"
access_key = "your-access-key"
resource_id = "volc.bigasr.sauc.duration"
hotwords = ["Wayland", "Sway", "wl-copy", "wtype", "just-talk-go"]
hotwords_file = ""                 # 外部词表路径，与 hotwords 合并（留空关闭）

[voice.continuous]                 # 与听写并行的转写服务，见下文
enabled = false
hotkey = ""                        # 按一下开、再按一下关（留空 = 不注册）
device = ""                        # 外部音源的设备（通常是回环设备）
silence_gate = true                # 按说话声切句，静音不上传

# 引擎：doubao-stream（豆包流式，默认）/ dashscope（千问 Fun-ASR 流式）
#       openai / auralwise / mimo-asr（整段转写，无实时字幕）
engine = "doubao-stream"
# 各引擎的凭据与参数独立保存，互不影响：
# [voice.engine_configs.dashscope]
# api_key = "sk-..."
# model = "fun-asr-realtime"

[overlay]
enabled = true
# macOS 默认 "notch"，其他平台默认 "bottom-center"
# 可选：notch、top-left、top-center、top-right、bottom-left、bottom-center、bottom-right
position = "notch"
show_text = true                   # 刘海中是否显示实时/最终识别文字
idle_visible = false               # 空闲时是否显示提示
scale = 1.0

[debug]
enabled = false
hotkeys = []                       # 调试插件额外注册的热键
```

`position = "notch"`（macOS 默认）让提示从刘海展开，`show_text` 控制刘海内是否显示实时/最终文字；浮层固定在鼠标所在屏幕，屏幕无刘海时从顶部中央展开（同样显示文字），Linux / Windows 上 `"notch"` 按 `top-center` 处理。预览与效果说明见上文 [macOS 刘海浮层](#macos-刘海浮层)。在 TUI 中选中“提示位置”按 `e` 进入编辑、`j`/`k` 切换、`Enter` 保存即可即时修改；已有配置中显式写了 `position` 的不会被迁移。

**录音设备**：`device` 留空或为 `"default"` 时使用系统默认输入。设置窗口和 TUI 里都可以直接选，列表按用途分组（TUI 单行显示，类别标在条目后面，如 `BlackHole 2ch · 回环`）：

| 分组 | 录的是什么 |
|---|---|
| 麦克风 | 外部声音：你自己说话、房间里的声音 |
| 回环设备 | 本机播放的声音：会议、视频。虚拟声卡（BlackHole、Loopback）或聚合设备，需先把要录的应用的播放输出指向它 |

配置里写了、但当前没接上的设备会标成“未检测到”留在列表里，不会因为打开一次界面就被改掉。

**各平台的支持程度不同**：

| | 指定设备录音 | 列表里区分麦克风 / 回环 | 录本机播放的声音 |
|---|---|---|---|
| macOS | ✅ 先按 UID 精确匹配，再按名称（不区分大小写）；找不到不会退回默认麦克风，而是列出可用设备并拒绝录音 | ✅ 按 CoreAudio 传输类型自动判断（虚拟 / 聚合且有输出流 = 回环） | ⚠️ 系统没有“录系统声音”这种输入，必须装 BlackHole / Loopback 等虚拟声卡，并把要录的应用的播放输出指向它；未检测到回环设备时界面会提示 |
| Linux | ✅ 原样传给 `arecord -D`；设备名不对时由 arecord 报错 | ⚠️ 只能按名称含 `.monitor` 判断；其余一律当麦克风 | ⚠️ PulseAudio / PipeWire 的 `.monitor` 源就是回环，但列表取自 `arecord -L` 的 `hw:` / `plughw:` / `default` 条目，monitor 源通常不在其中，需要手填 `device`（如 `pactl list short sources` 里的 monitor 名） |
| Windows | ✅ 按 winmm 设备名（不区分大小写）或设备编号 | ❌ winmm 不提供这个信息，全部当麦克风 | ❌ 暂不支持：winmm 只能录采集设备，录本机播放需要 WASAPI loopback，还没实现 |

**识别结果送到哪里**：`text_output` 有三个值——`paste` 上屏到当前输入框（同时进剪贴板）、`clipboard` 只写剪贴板、`none` 不输出（只转写）。旧配置里的 `auto_submit` 会自动迁移：`true` → `paste`，`false` → `clipboard`，下次存盘后该键消失。

这是 **Just Talk 自己的事**：文字要不要粘到当前输入框、要不要进剪贴板，跟「把转写能力提供给别的程序」是两件事。订阅识别结果的程序既不受这个设置影响，帧里也不带它——订阅者不用知道上不上屏这回事。

持续转写则根本不在这个维度上：它转的是**外部音源**（会议、系统声音、一屋子人），不是你对着机器在说话。没人要求把这些话打出来，所以那一路不输出文本，`text_output` 对它不适用。这是它的定义，不是它的限制。

两者是**并行**的，不是二选一：收听外部音源不妨碍你同时用热键听写。所以设置里它们是两个分组——「快捷键」管听写，「持续转写」是一个开关、它自己的外部音源设备、它自己的开关热键，以及把结果推给别的程序的订阅，而不是同一个选择器里的第三个值。

**外部词表**：热词让识别更贴当下的上下文，但写在配置里的是一份死名单。`hotwords_file` 指向一个文本文件（一行一个词，`#` 开头为注释，支持 `~`），内容与 `hotwords` 合并去重。桌面设置窗口里点“选择…”从系统文件选择框挑文件；`--web` 拿不到浏览器里所选文件的路径，仍手填。词表是在会话开始时发给引擎的，所以：听写下一次按键生效；持续转写在静音门控开着时每句话就是一个新会话，词表每句都重读，改完下一句生效；把门控关掉后会话可能几十分钟不断，所以 Just Talk 每 2 秒查一次文件，**改动后自动重连一次让新词生效**。不用改配置、不用重启。读不到文件只警告并沿用 `hotwords`，不会阻止识别。

**最近活动**：设置窗口和 TUI 里的“最近活动”（识别结果、状态、报错）会存到 `~/.local/state/just-talk/<配置文件名>.activity.jsonl`（一行一条 `{"time","text"}`；设了 `XDG_STATE_HOME` 时在其下，Windows 在 `%LOCALAPPDATA%\just-talk`），重启后还在，别的程序也能读。它和界面里的那份一样只留最近几百条、不存流式中间结果；文件权限 0600，但内容是你最近说过的话的明文。按配置文件名分开，是为了用 `-config` 同时跑的几个实例不串。

**持续转写**：`[voice.continuous] enabled = true` 用于转写会议或系统音频。它和听写**并行**，不是二选一：你按热键对着麦克风说话上屏的同时，它在后台把外部音源转给订阅者。两路各有自己的录音设备（`[voice] device` 是麦克风，`[voice.continuous] device` 通常是回环设备）、自己的会话。

这一路没有「手指」：听写里是人按下和松开在决定一句话从哪开始到哪结束，这里由**静音门控**（`silence_gate`，默认开）做：听到说话声才打开 ASR 会话，静音约 1.2 秒就主动把这一句收掉（最后一包带 `last` 标记，让引擎吐出定稿）。于是静音期间既不往云端送音频，也没有挂着的会话；打开会话前约 0.6 秒的音频会被留着一起送出去，句子的第一个字不会被切掉。

**它也能用热键开关**：`[voice.continuous] hotkey` 设一个键，按一下开、再按一下关，跟你现在开关听写一样。区别在于它划的不是一句话的边界，而是整条服务的开关，所以只有切换、没有「按住」——没人会按着一个键开完一场会，而且句子边界已经有静音门控在做。按键只改**当前运行状态**，不写配置文件；之后在设置里改 `enabled` 会盖过按键的选择（显式指令优先），不动它的重载则不会。这个键不能和听写热键重复，设置窗口和 TUI 保存时就拒绝。

它归 `[voice] enabled` 管：关掉语音输入（包括托盘面板的“语音输入”开关）会同时停掉持续转写并注销它的热键；识别结果订阅服务跟着持续转写走，也一起停。

托盘面板里也有一个“持续转写”开关。它显示的是**当前运行状态**，热键开了它也跟着变；点它既写配置（重启后保持），也直接开关正在运行的服务——所以热键在配置为“关”时打开的服务，在面板上点关也能停掉。语音输入关着时这个开关不可点。

旧配置里的 `mode = "continuous"` 会自动迁移：`[voice.continuous] enabled = true`，`device` 搬到那一路，并且**清掉热键**——那份配置本来就不注册热键，迁完凭空多出一路能往输入框打字的听写才是意外。热键留空 = 不听写，这是转写专用实例的正常配法。

写 `silence_gate = false` 退回老行为：会话一直连着，因任何原因结束（服务端结束、错误、断网）都自动重连（0.5s 起退避，最长 5s），录音设备在重连期间保持打开，重连间隙里最近约 5 秒的音频补发给新会话——代价是静音也在上传，而且补发可能让压在重连点上的句子被识别两遍。

这一路**从不**上屏、不写剪贴板，也**不占用刘海 / 状态浮层**：它只是给别的程序订阅的后台服务，刘海和状态行只属于听写，出现了就一定是你在听写。结果只进入日志和下文的 WebSocket 订阅；服务开没开看托盘面板的“持续转写”开关。需要流式引擎（如 `doubao-stream`、`dashscope`）。macOS 上可配合 [BlackHole](https://github.com/ExistentialAudio/BlackHole) 等虚拟音频设备：把会议软件的扬声器输出设为 BlackHole，再让它从 BlackHole 录音：

```toml
[voice]
push_to_talk = ""                  # 只做转写服务：没有热键就不听写
stream_addr = "127.0.0.1:7590"

[voice.continuous]
enabled = true
device = "BlackHole 2ch"

[overlay]
enabled = false                    # 不需要刘海提示时关闭浮层
```

**识别结果订阅**：**没有单独的开关，它跟着持续转写走**——持续转写在跑，Just Talk 就在 `stream_addr`（默认 `127.0.0.1:7590`，留空也按默认处理）的 `/asr` 上提供 WebSocket 订阅；持续转写关了（包括用热键或托盘开关关掉、或语音输入关掉），订阅服务随之停止，端口释放。订阅推的只有持续转写的结果，它不跑就没有东西可推，所以不需要第二个开关。设置窗口和 TUI 里它是持续转写分组里的「订阅地址」一项，下面实时显示监听中 / 未启动 / 未监听和当前订阅者数。改地址会重启订阅服务，但不会打断正在进行的转写。

本机的程序不必自己实现 WebSocket 客户端：`just-talk subscribe` 就是现成的订阅者，按子进程契约工作（与 `lark-cli event consume` 同款）——识别帧逐行 NDJSON 输出到 stdout，连上时 stderr 打 ready 标记，退出时带原因（`limit` / `timeout` / `signal` / `closed`）和退出码，`--timeout` / `--max-events` 限时限次，`--addr` 可换地址。探活一帧即可：

```bash
just-talk subscribe --max-events 1      # 拿到 service 帧即在监听
just-talk subscribe --timeout 10m        # 读 10 分钟
```

这样一来，只写了 `stream_addr`、没开持续转写的配置不再监听——以前地址本身就是开关，现在要在「持续转写」里打开它。

地址有两种写法：

| 写法 | 谁连得上 | 代价 |
|---|---|---|
| `127.0.0.1:7590` | 本机**任何**程序，网页也行 | 没有访问控制。只允许回环地址（`127.0.0.1`、`::1`、`localhost`），其他地址保存时就被拒绝；来自其他网页来源的浏览器连接会被拒绝 |
| `unix:~/.local/state/just-talk/asr.sock` | **只有文件主**（socket 建为 0600） | 浏览器连不了；客户端要会拨 Unix socket；**不支持 Windows** |

> **Windows 不支持 `unix:` 地址，请用 `127.0.0.1:<端口>`。** 在 Windows 上保存这种地址会直接被拒绝。Windows 上 0600 不起作用（Windows 的权限位只管只读属性），它要提供的「只有文件主能连」在那里不成立。

路径式地址顺带解决身份问题：端口只能告诉你「有人占着」，socket 路径说得出是哪个实例。崩溃残留的 socket 文件会被自动清掉；要是那上面真有实例在听，启动直接报错，而不是把它抢走。路径上如果是普通文件或别的什么、不是 socket，Just Talk 绝不删它，订阅服务不启动，日志里写明原因——地址写错不会让你丢文件。

socket 所在的目录要事先存在，Just Talk 不会替你创建；目录不存在时订阅服务起不来，原因写在日志里。

**推送的只有持续转写那一路。你自己的听写不进订阅流**：听写是 Just Talk 替你打字，去向由 `text_output` 决定；订阅流是对外提供的转写能力，转的是外部音源。两路并行跑的时候这条界线尤其要紧：订阅者不会因为你按了一下热键，就收到你自己说的话。

**订阅者数量不限，彼此互不可见**：这是一条共享的 ASR 结果流，没有任何帧是寄给某个订阅者的，也没有任何帧告诉你还有几个人在连（订阅者数只出现在设置界面）。每个连接有自己的缓冲，跟不上的那个被单独断开，不拖累别人、也不拖累识别。订阅者连发都发不了（入站帧读完即丢，只用来感知断开）。所以“自己过滤”指的是“我只关心 definite 的句子”，不是“从别人的信里挑我的”。

唯一的例外是**连上时的那一帧 `service`**：它说的是服务自己的状态，同一刻连上来的任何人拿到的都一样，不是别人的内容。

服务端只发送 JSON 文本帧：

```jsonc
// 连上时先来一帧，之后每次状态变化再广播；
// state 为 listening（在录音，有人说话就会转）或 idle（持续转写没开或录音掉了）
{"type":"service","state":"listening","device":"BlackHole 2ch","engine":"doubao-stream","gate":true}
// ASR 会话连接成功
{"type":"session","state":"start","session":"jt-20261024T143352-1"}
// 每个识别结果帧：text 为本会话累计全文，utterances 为服务端分句（definite = 该句已确定），
// final 仅在服务端最后一包时为 true
{"type":"asr","session":"jt-20261024T143352-1","text":"你好。我们","utterances":[{"text":"你好。","definite":true},{"text":"我们","definite":false}],"final":false}
// 会话结束；reason 为 last（服务端最后一包）、error（识别或发送出错）或 closed（连接关闭、停止录音、退出）
{"type":"session","state":"end","session":"jt-20261024T143352-1","reason":"closed"}
```

没人说话时订阅流是静的，所以「在听，只是没人说话」和「进程根本没跑」在线上长得一模一样——`service` 帧就是用来分这两件事的，它比「端口通不通」强得多。它**不**带 `text_output`：听写的文字往哪里去不关订阅者的事。

推送不会拖慢识别：每个订阅者有独立缓冲，缓冲堆满（超过 256 帧没读走）的订阅者会被断开，不影响其他订阅者。

</details>

## 命令行

<details>
<summary>展开查看 CLI 参数</summary>

```text
just-talk [flags]

                     （无参数：菜单栏/托盘应用 + 设置窗口）
-tui                 使用终端 TUI，不打开桌面窗口
-web                 在浏览器中打开设置页：只监听 127.0.0.1，启动时输出带本次运行密钥的链接
-no-tui              后台模式，无界面，日志输出到 stderr
-backend <name>      强制后端：x11 | wayland | darwin（也可设 JUST_TALK_BACKEND）
-config <path>       指定配置文件路径
-doctor              运行启动环境检查后退出
-install             为当前用户安装可执行文件
-debug               启用调试插件（配合配置 [debug]）
-verbose             详细日志
```

子命令 `just-talk subscribe`：把识别结果订阅当子进程消费（见上文「识别结果订阅」）。识别帧逐行 NDJSON 到 stdout，ready 标记和退出原因到 stderr；`--timeout` / `--max-events` 限时限次，`--addr` / `--config` 选地址和配置，四种正常退出原因部退出码 0，连不上为 1。

</details>

## 开发

<details>
<summary>展开查看构建、测试与调试命令</summary>

```bash
make build          # 构建当前平台
make run            # 直接运行
go test ./...       # 运行全部测试（Linux 加 -tags gtk3）
go test ./... -tags "gtk3 no_x11"    # 无 X11 环境下测试（Linux）
go build -tags nogui ./cmd/just-talk    # 不含桌面窗口的构建
goreleaser check    # 校验 .goreleaser.yaml
```

可选的 Windows 集成测试（需要真实设备/钩子环境）：

```bash
JUST_TALK_TEST_WINDOWS_AUDIO=1 go test ./plugins/voice -run TestWindowsRecorderIntegration -v
JUST_TALK_TEST_WINDOWS_HOTKEY=1 go test ./hotkey -run TestWindowsHookIntegration -v
```

TUI 和桌面窗口模式下普通日志只写入日志文件与界面内的日志区域，避免破坏 Bubble Tea 布局（桌面窗口不可用时会回退到 TUI）；调试详情仅在 `--debug` 时可见。

设置页是嵌入二进制的纯 HTML/CSS/JS（`internal/gui/assets`），通过同一个 handler 上的 JSON API 与 Go 通信，桌面窗口与 `--web` 共用，不需要前端构建步骤。

</details>

## 更新日志

见 [CHANGELOG.md](CHANGELOG.md)。

## 维护与贡献

本仓库是 [whoamihappyhacking/just-talk-go](https://github.com/whoamihappyhacking/just-talk-go) 的独立 fork。上游项目仍由原作者维护；本仓库独立演进，目标是社区驱动的开发，欢迎所有贡献者参与。

欢迎通过 [Issue](https://github.com/wakaka6/just-talk-go/issues) 反馈 bug、使用体验和功能建议；也欢迎 Pull Request。贡献流程与要求见 [CONTRIBUTING.md](CONTRIBUTING.md)：建议先开 Issue 讨论改动方向，提交前运行 `gofmt`、`go vet` 和 `go test ./...`。

## 许可证

Just Talk 使用 [GNU General Public License v3.0](LICENSE)（GPL-3.0-only）开源。
