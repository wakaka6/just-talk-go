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

Type less. Talk instead.

Just Talk is a desktop voice input tool. Press a global hotkey to record; audio is streamed to the Doubao large-model ASR, and the recognized text is copied to the clipboard or submitted directly into the focused input field. It is built for coding, chatting, note-taking, and long-form text entry.

![Just Talk TUI](docs/screenshot-tui.png)

## macOS Notch Overlay

On macOS, the recording indicator expands straight out of the notch: live recognized text appears as you speak with a typewriter-style reveal, alongside a mic-level waveform; when recognition finishes, it lingers to show the final text and the "copied / auto-submitted" state.

![macOS notch overlay on real hardware: live recognized text and mic-level waveform while recording](docs/assets/notch-live.gif)

![macOS notch overlay on real hardware: copied state after recognition finishes](docs/assets/notch-live-final.png)

On displays without a notch (external monitors or notch-less models) the same indicator grows out of the top center of the screen, with live text and the waveform. Enable it via `position = "notch"` or the overlay position field in the TUI; see [Configuration](#configuration) for behavior details.

## Features

- Global hotkey recording with `toggle` (press to start, press again to stop) and `hold` (push-to-talk) modes.
- Voice hotkeys are restricted to keys suitable for global shortcuts: modifier-only combos, function keys, Tab, CapsLock, arrow and navigation keys. Letters, digits, punctuation, Space, and other text-producing keys are rejected.
- Doubao large-model streaming ASR (optimized bidirectional streaming plus a second pass) with hotword boosting.
- Automatic clipboard copy, with optional auto-submit into the focused field.
- A continuous transcription service: alongside dictation, it transcribes an external source such as a meeting or system audio, cut into sentences by speech, and can be switched by a hotkey. Results go to programs subscribed over a local WebSocket; nothing is typed or copied.
- Recording overlay: on macOS it expands from the notch and shows live recognized text, mic level, and the final result; Linux (Wayland/X11) and Windows get an always-on-top status capsule.
- Desktop settings window with a menu bar/tray icon for hotkeys (dictation's primary and secondary slots plus the continuous transcription switch, recorded by clicking a slot and pressing the combo; each slot can be cleared), mode, recognition engine and credentials, output destination, stop delay, hotwords, continuous transcription, the result subscription, and overlay position — saved changes apply immediately without a restart. The same page opens in a browser with `--web`, and `--tui` keeps the terminal UI.
- Usage statistics: total sessions, recognized characters, average speed, and recent speed.
- `Esc` cancels a recording; `R` retries after a retryable error.

## Platform Support

| Platform | Status | Notes |
| --- | --- | --- |
| Linux Wayland | Supported | Verified on Sway / wlroots; hotkeys use evdev and require `input` group access; clipboard/auto-submit need `wl-clipboard` plus `wtype` or writable `/dev/uinput` |
| Linux X11 | Supported | Native X11 global hotkeys and overlay, XTest auto-submit |
| macOS | Supported | CGEventTap hotkeys, CoreAudio recording, NSPasteboard clipboard, AppKit NSPanel overlay; requires Accessibility and Microphone permissions |
| Windows 10/11 | Supported | Global key polling plus a low-level keyboard-hook edge fallback, WinMM recording, Unicode clipboard, SendInput auto-submit, and a topmost Win32 overlay |

The settings window uses the system webview. Linux builds link GTK 3 + WebKitGTK 4.1, so its runtime must be installed (`libwebkit2gtk-4.1-0` on Debian/Ubuntu, `webkit2gtk-4.1` on Arch); build with `-tags nogui` if you only use the TUI. Windows uses WebView2 (built into Windows 11); without it Just Talk falls back to the TUI and prints where to install it.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install wakaka6/tap/just-talk
```

Tap: [wakaka6/homebrew-tap](https://github.com/wakaka6/homebrew-tap). The formula covers macOS (Apple Silicon / Intel) and Linux (amd64 / arm64).

### Prebuilt binaries

<details>
<summary>Per-platform archive download and checksum verification</summary>

Download an archive for your platform from [GitHub Releases](https://github.com/wakaka6/just-talk-go/releases), extract it, and put `just-talk` on your `PATH` (for example `~/.local/bin`). Windows ships as `.zip`; other platforms as `.tar.gz`. Verify downloads with the included `SHA256SUMS.txt`.

Available archives: Linux amd64 / arm64, macOS Intel / Apple Silicon, Windows amd64 / arm64.

</details>

### Build from source

<details>
<summary>Build dependencies and install steps</summary>

Just Talk relies on native platform APIs. Linux and macOS builds require cgo; Windows calls Win32 directly from Go and does not need cgo. The desktop window is built with [Wails v3](https://github.com/wailsapp/wails) on the system webview: WKWebView on macOS, WebView2 on Windows, and GTK 3 + WebKitGTK 4.1 on Linux.

Linux build dependencies:

```bash
# Arch Linux
sudo pacman -S --needed go gcc libx11 libxtst libxext wayland gtk3 webkit2gtk-4.1

# Debian / Ubuntu
sudo apt install golang-go build-essential libx11-dev libxtst-dev libxext-dev libxinerama-dev libwayland-dev libgtk-3-dev libwebkit2gtk-4.1-dev
```

macOS needs Apple Command Line Tools (clang and the macOS SDK; full Xcode is not required):

```bash
xcode-select --install
```

Windows only needs Go 1.25 or later — no ffmpeg, SoX, or C compiler:

```powershell
winget install --id GoLang.Go --exact
```

Build:

```bash
# macOS
CGO_ENABLED=1 go build -o build/just-talk ./cmd/just-talk
# Linux (the desktop window uses GTK 3 + WebKitGTK 4.1)
CGO_ENABLED=1 go build -tags gtk3 -o build/just-talk ./cmd/just-talk
# Windows PowerShell:
# go build -o build\just-talk.exe .\cmd\just-talk
```

Add `-tags nogui` to build without the desktop window: the binary does not link a webview and starts the TUI when run without flags.

Install for the current user (`~/.local/bin` on Linux/macOS, `%LOCALAPPDATA%\Programs\Just Talk` on Windows):

```bash
build/just-talk --install
# or
make install
```

If the install directory is not on your `PATH`, follow the hint printed by the command.

</details>

## Quick Start

1. Obtain Doubao large-model ASR credentials (App Key / Access Key).
2. Run `just-talk --doctor` to check the microphone, clipboard tools, and permissions.
3. Run `just-talk` to open the settings window, pick a channel under “识别引擎” (Doubao streaming by default), fill in its credentials, and click “保存” (or press `Cmd/Ctrl+S`). Closing the window keeps Just Talk running. Left-click the menu bar/tray icon for a small panel with the live state, the latest transcript, usage statistics, and the current settings, with a “语音输入” switch to turn voice input on or off; its “打开设置” button reopens the settings window. Right-click opens the tray menu. Quit with “退出” in the panel or the tray menu. Prefer the terminal? Run `just-talk --tui`.
4. Press the `push_to_talk` hotkey (default `Alt+Super`; `Option+Command` on macOS) to start/stop recording. `text_output` decides where recognized text goes; by default it is pasted into the focused field.

### Permissions

<details>
<summary>Per-platform permission requirements</summary>

- **Linux Wayland**: global hotkeys read `/dev/input/event*` through evdev. Add your user to the `input` group and log back in:

  ```bash
  sudo usermod -aG input $USER
  ```

  Clipboard needs `wl-clipboard`; auto-submit needs `wtype` or write access to `/dev/uinput` (uinput is recommended on KDE Plasma — `--doctor` prints the full setup steps).

- **macOS**: grant **Accessibility** and **Microphone** permissions to the terminal app that launches Just Talk (Terminal, iTerm2, etc.) under System Settings → Privacy & Security. No `.app` signing or full Xcode is needed.
- **Windows**: no administrator rights required. If the microphone is unavailable, allow desktop apps under Settings → Privacy & security → Microphone.

</details>

## Hotkeys

### Voice hotkeys

| Key | Action |
| --- | --- |
| Configured `push_to_talk` | Start/stop recording in `toggle` mode; hold to record in `hold` mode |
| Configured `[voice.continuous] hotkey` | Turn the continuous transcription service on/off (toggle only, never written to the config); see [Configuration](#configuration) |
| `Esc` | Cancel the current recording or dismiss an error |
| `R` | Retry recognition after a retryable error |

Voice hotkeys only accept keys suitable for global shortcuts:

- Modifier-only combos: `Alt+Super`, `Ctrl+Alt+Shift`, `Option+Command`, etc.
- `Fn` (macOS / Linux): also accepted as `Function`; can combine with ordinary keys but not with `F1`–`F24`. On Linux, `Fn` is read through evdev; when no backend is specified, a hotkey using `Fn` automatically selects the Wayland/evdev backend.
- Function keys `F1`–`F24`, such as `F9` or `Alt+F8`.
- Non-text control and navigation keys: `Tab`, `Enter`, `Escape`, `Backspace`, `CapsLock`, arrow keys, `Home`, `End`, `PageUp`, `PageDown`, `Insert`, `Delete`.
- The PC keyboard menu key left of right `Ctrl`: `Menu` (also accepted as `Apps` or `Application`).
- Not supported: `Fn+F1`–`Fn+F24`, and letters, digits, punctuation, `Space`, numpad keys, or other text-producing keys without `Fn` (e.g. `Alt+G`, `G`, `Alt+1`, `Alt+Space`).

On Windows, the low-level keyboard hook only observes key edges and never consumes or replays modifiers, so standalone `Alt`, `Super`, and system shortcuts such as `Alt+Tab` keep their normal behavior.

`push_to_talk` accepts several hotkeys separated by commas, e.g. `push_to_talk = "Fn, Menu"`: `Fn` on the built-in keyboard, the menu key on an external PC keyboard (Print Screen, which macOS reports as `F13`, works too). Any of them starts/stops recording; in `hold` mode recording stops only after every held hotkey is released. A comma written right after `+` (e.g. `Fn+,`) is still the comma key. The `Fn` key of Logitech and other third-party keyboards is handled in keyboard firmware and never reaches the OS, so it cannot be used as a hotkey.

### TUI keys

Start the TUI with `just-talk --tui`.

| Key | Action |
| --- | --- |
| `j` / `k` / arrows | Move selection |
| `e` / `i` / `Enter` | Edit the selected item (text input for strings; `j`/`k` cycles options; `Space` toggles booleans) |
| `Enter` | Leave edit mode and save; `Esc` only leaves edit mode |
| `s` | Save all settings (applies immediately, no restart) |
| `l` | Expand/collapse the log area |
| `h` | Toggle help |
| `q` / `Ctrl+C` | Quit |

After saving the engine or its credentials in the settings window or the TUI, the next recording uses the new configuration. An in-progress session keeps the configuration used to establish its connection.

## Configuration

<details>
<summary>Config file paths and recommended config</summary>

Default config path:

```text
# Linux / macOS
~/.config/just-talk/config.toml

# Windows
%APPDATA%\just-talk\config.toml
```

`./config.toml` in the working directory and `$XDG_CONFIG_HOME/just-talk/config.toml` take precedence when present.

Recommended config:

```toml
[voice]
enabled = true
mode = "toggle"                    # how a hotkey starts and ends one dictation: toggle | hold
push_to_talk = "Alt+Super"
language = "zh-CN"
text_output = "paste"              # where recognized text goes: paste | clipboard | none
stop_delay_ms = 0                  # release delay in hold mode
device = ""                        # recording device (empty = default; macOS matches device UID or name)
stream_addr = ""                   # subscription address: "127.0.0.1:7590" or "unix:~/.../asr.sock" (empty = off)
gain = 0                           # microphone gain
app_key = "your-app-key"
access_key = "your-access-key"
resource_id = "volc.bigasr.sauc.duration"
hotwords = ["Wayland", "Sway", "wl-copy", "wtype", "just-talk-go"]

[voice.continuous]                 # the transcription service, alongside dictation (see below)
enabled = false
hotkey = ""                        # press once to start, again to stop (empty = none)
device = ""                        # the external source's device (usually a loopback one)
silence_gate = true                # cut on speech, upload no silence

# Engine: doubao-stream (default, Doubao streaming) / dashscope (Qwen Fun-ASR streaming)
#         openai / auralwise / mimo-asr (batch transcription, no live captions)
engine = "doubao-stream"
# Per-engine credentials are kept separately and never clobber each other:
# [voice.engine_configs.dashscope]
# api_key = "sk-..."
# model = "fun-asr-realtime"

[overlay]
enabled = true
# Defaults to "notch" on macOS and "bottom-center" elsewhere.
# Values: notch, top-left, top-center, top-right, bottom-left, bottom-center, bottom-right
position = "notch"
show_text = true                   # show live/final recognized text in the notch
idle_visible = false               # keep the indicator visible when idle
scale = 1.0

[debug]
enabled = false
hotkeys = []                       # extra hotkeys registered by the debug plugin
```

`position = "notch"` (the macOS default) expands the indicator from the notch, and `show_text` controls whether live/final text is shown inside it; the overlay sticks to the screen under the mouse pointer, and on a screen without a notch it grows out of the top center instead (text included). On Linux/Windows `"notch"` behaves as `top-center`. See [macOS Notch Overlay](#macos-notch-overlay) above for previews and the visual behavior. In the TUI, select the overlay position field, press `e` to edit, `j`/`k` to cycle, and `Enter` to save — it applies immediately. Existing configs that set `position` explicitly are not migrated.

**Recording device**: an empty `device` or `"default"` uses the system default input. Both the settings window and the TUI list the devices grouped by what they capture (the TUI shows one line at a time, so the kind is appended to the entry, e.g. `BlackHole 2ch · 回环`):

| Group | What it records |
|---|---|
| Microphone | The outside world: your voice, the room |
| Loopback | What the computer plays: a meeting, a video. A virtual sound card (BlackHole, Loopback) or an aggregate device; the app you want to record must play into it |

A device named in the config but not connected right now stays in the list marked “not detected”, so opening the settings never silently changes the recording source.

**Platform support differs**:

| | Selecting a device | Telling microphone from loopback | Recording what the computer plays |
|---|---|---|---|
| macOS | ✅ Matched against the device UID, then the name (case-insensitive); an unknown device lists the available ones and refuses to record instead of falling back to the default microphone | ✅ From the CoreAudio transport type (virtual/aggregate with output streams = loopback) | ⚠️ The OS has no “system audio” input: it needs a driver such as BlackHole or Loopback, with the app you want to record playing into it. The UI says so when none is installed |
| Linux | ✅ Passed to `arecord -D` verbatim; arecord fails on an unknown device | ⚠️ Only names containing `.monitor`; everything else counts as a microphone | ⚠️ A PulseAudio / PipeWire `.monitor` source is a loopback, but the list comes from the `hw:` / `plughw:` / `default` entries of `arecord -L`, which usually omits monitor sources: set `device` by hand (see `pactl list short sources`) |
| Windows | ✅ By winmm device name (case-insensitive) or device index | ❌ winmm does not report it; every device counts as a microphone | ❌ Not supported: winmm only records capture devices, and WASAPI loopback is not implemented yet |

**Where recognized text goes**: `text_output` is `paste` (into the focused field, and the clipboard), `clipboard` (clipboard only) or `none` (transcribe only). A legacy `auto_submit` migrates automatically — `true` → `paste`, `false` → `clipboard` — and the key disappears on the next save.

This is **Just Talk's own business**: whether text is pasted into the focused field or copied has nothing to do with offering transcription to other programs. A subscriber is not affected by this setting and the frames do not carry it — a subscriber never has to think about pasting.

Continuous transcription is not on that axis at all: it transcribes an **external source** — a meeting, system output, a room full of people — not you talking to your own machine. Nobody asked for those words to be typed out, so that path outputs no text and `text_output` does not apply to it. That is what it is, not a limit on it.

The two run **in parallel**, not one instead of the other: listening to an external source does not stop you from dictating with a hotkey at the same time. So they are two groups in the settings — "hotkeys" configures dictation, and "continuous transcription" is a switch with its own external-source device and its own on/off hotkey, rather than a third value in one picker.

**An external word list**: hotwords make recognition fit the words actually being said, but a list inside the config is frozen. `hotwords_file` points at a text file (one word per line, `#` starts a comment, `~` is expanded) whose words are merged with `hotwords`. The list reaches the engine when a session opens, so: dictation picks it up on the next press; continuous transcription with the silence gate on every utterance is a new session, so the file is reread for each one and an edit applies to the next sentence; with the gate off a session can stay up for tens of minutes, so Just Talk checks the file every 2 seconds and **reconnects once after an edit** to apply it. No config change, no restart. An unreadable file is a warning: recognition continues with `hotwords`.

**Continuous transcription**: `[voice.continuous] enabled = true` is for transcribing a meeting or other system audio. It runs **alongside** dictation rather than instead of it: while you hold a hotkey and talk into your microphone, it keeps handing the external source to subscribers in the background. Each path has its own recording device (`[voice] device` is the microphone, `[voice.continuous] device` is usually a loopback one) and its own sessions.

This path has no finger on a key: in dictation a person decides where a sentence starts and ends, and here the **silence gate** (`silence_gate`, on by default) does that job. Speech opens an ASR session; about 1.2 seconds of silence closes it (the final packet is flagged `last`, which asks the engine for its final result). So nothing is sent to the cloud while no one is talking and no session sits idle; the 0.6 seconds of audio captured before the gate opened are sent along, so a sentence never loses its first syllable.

**It has a hotkey too**: bind one in `[voice.continuous] hotkey` and press it to start the service, press it again to stop — the same way you switch dictation on and off. The difference is what the key bounds: a whole service rather than a sentence, so it only toggles and there is no hold — nobody holds a key down for an entire meeting, and sentence boundaries are the silence gate's job anyway. The key changes the **running state** only; it never writes the config file. Editing `enabled` in the settings afterwards overrides what the key chose (an explicit instruction wins), while a reload that leaves the setting alone leaves the key's choice alone. It cannot be the same combo as a dictation hotkey; the settings window and the TUI refuse to save that.

It answers to `[voice] enabled`: turning voice input off (including the “语音输入” switch in the tray panel) also stops continuous transcription and unregisters its hotkey. The subscription server depends only on `stream_addr` and keeps running.

An old `mode = "continuous"` migrates on load: `[voice.continuous] enabled = true`, the `device` moves to that path, and **the hotkey is cleared** — that config registered no hotkey, so inventing a dictation that types into the focused window would be the surprise. No hotkey means no dictation, which is how a transcription-only instance is configured.

Set `silence_gate = false` for the older behaviour: one long session that reconnects whenever it ends for any reason (server end, error, network drop) with a backoff from 0.5s capped at 5s, the recording device staying open across reconnects, and up to the last 5 seconds of audio replayed into the new session — at the price of uploading silence, and of a sentence that straddles a reconnect being recognised twice.

This path **never** types, pastes or touches the clipboard; results go only to the log, the overlay and the WebSocket subscription below. It needs a streaming engine (e.g. `doubao-stream`, `dashscope`). On macOS, pair it with a loopback device such as [BlackHole](https://github.com/ExistentialAudio/BlackHole): set the meeting app's speaker output to BlackHole and record from it:

```toml
[voice]
push_to_talk = ""                  # service only: no hotkey means no dictation
stream_addr = "127.0.0.1:7590"

[voice.continuous]
enabled = true
device = "BlackHole 2ch"

[overlay]
enabled = false                    # turn the overlay off if you don't want the notch indicator
```

**Recognition result subscription**: with `stream_addr` set (editable in the settings window and the TUI, both of which show the listening address and how many subscribers are connected), Just Talk serves a WebSocket at `/asr`. A config reload restarts the server only when the address changes. The address comes in two kinds:

| Address | Who can connect | Cost |
|---|---|---|
| `127.0.0.1:7590` | **any** local program, browsers included | no access control. Only loopback addresses (`127.0.0.1`, `::1`, `localhost`) are accepted; anything else is rejected when you save, and browser connections from other origins are rejected |
| `unix:~/.local/state/just-talk/asr.sock` | **only the owner** (the socket is created 0600) | browsers cannot reach it; a client has to dial a Unix socket; it does listen on Windows, but 0600 means nothing there (Windows permission bits only set the read-only attribute) and who can connect depends on the directory's permissions, so use a port on Windows |

A path also settles identity: a port only tells you something is bound, a socket path tells you which instance. A socket file left behind by a crash is cleaned up; if an instance really is listening on it, startup fails loudly instead of stealing the address. If the path holds anything that is not a socket, such as a regular file, Just Talk never removes it: the subscription server does not start and the log says why, so a mistyped address cannot cost you a file.

The socket's directory must already exist; Just Talk does not create it. When it is missing the subscription server does not start, and the log says why.

**Only continuous transcription is published. Your own dictation is not**: dictation is Just Talk typing for you and `text_output` decides where it lands; the subscription is the transcription capability offered to other programs, and it carries the external source. That line matters most when both run in parallel: pressing a hotkey never sends your own words to a subscriber.

**Any number of subscribers, none of them aware of the others**: this is one shared stream of ASR results. No frame is addressed to a particular subscriber and no frame reveals how many others are connected (that count only appears in the settings UI). Each connection has its own buffer, and one that falls behind is disconnected on its own — without slowing down the others or recognition. Subscribers cannot even send anything (inbound frames are read and discarded, only to notice a disconnect). So “filter client-side” means “I only care about the definite sentences”, not “sort someone else’s mail”.

The one exception is **the `service` frame sent on connect**: it describes the service's own state, identical for anyone connecting at that moment, and is nobody else's content.

The server sends JSON text frames only:

```jsonc
// sent once on connect, then broadcast whenever it changes; state is listening
// (capturing, speech will be transcribed) or idle (the service is off or capture died)
{"type":"service","state":"listening","device":"BlackHole 2ch","engine":"doubao-stream","gate":true}
// an ASR session connected
{"type":"session","state":"start","session":"jt-20261024T143352-1"}
// one per recognition result frame: text is the session's full text so far, utterances are the
// server's sentences (definite = will not change), final is true only on the server's last packet
{"type":"asr","session":"jt-20261024T143352-1","text":"Hello. We","utterances":[{"text":"Hello.","definite":true},{"text":"We","definite":false}],"final":false}
// the session ended: reason is last (server's last packet), error (recognition or send error)
// or closed (connection closed, recording stopped, shutdown)
{"type":"session","state":"end","session":"jt-20261024T143352-1","reason":"closed"}
```

While nobody is talking the stream is silent, so "listening, nobody is speaking" and "the process is not running" look exactly the same on the wire — the `service` frame is what tells them apart, and it says far more than whether a port is bound. It does **not** carry `text_output`: where dictated text lands is none of a subscriber's business.

Publishing never slows recognition down: each subscriber has its own buffer, and a subscriber that falls more than 256 frames behind is disconnected without affecting the others.

</details>

## CLI

<details>
<summary>Command-line flags</summary>

```text
just-talk [flags]

                     (no flags: menu bar/tray app with the settings window)
-tui                 Run the terminal UI instead of the desktop window
-web                 Serve the settings page to a browser on 127.0.0.1; prints a link carrying this run's key
-no-tui              Daemon mode without any UI; logs to stderr
-backend <name>      Force a backend: x11 | wayland | darwin (or JUST_TALK_BACKEND)
-config <path>       Use a specific config file
-doctor              Run startup environment checks and exit
-install             Install the executable for the current user
-debug               Enable the debug plugin (with [debug] config)
-verbose             Verbose logging
```

</details>

## Development

<details>
<summary>Build, test, and debug commands</summary>

```bash
make build          # Build for the current platform
make run            # Build and run
go test ./...       # Run all tests (add -tags gtk3 on Linux)
go test ./... -tags "gtk3 no_x11"    # Test without X11 (Linux)
go build -tags nogui ./cmd/just-talk    # Build without the desktop window
goreleaser check    # Validate .goreleaser.yaml
```

Optional Windows integration tests (need a real device/hook environment):

```bash
JUST_TALK_TEST_WINDOWS_AUDIO=1 go test ./plugins/voice -run TestWindowsRecorderIntegration -v
JUST_TALK_TEST_WINDOWS_HOTKEY=1 go test ./hotkey -run TestWindowsHookIntegration -v
```

In TUI and desktop window modes, normal logs go to the log file and the in-app log area so they do not corrupt the Bubble Tea layout (the window falls back to the TUI when it cannot open); debug details are only visible with `--debug`.

The settings page is plain HTML/CSS/JS embedded in the binary (`internal/gui/assets`). It talks to Go over a JSON API on the same handler and is shared by the desktop window and `--web`, so there is no frontend build step.

</details>

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## Maintenance and Contributing

This repository is an independent fork of [whoamihappyhacking/just-talk-go](https://github.com/whoamihappyhacking/just-talk-go). The upstream project is still maintained by its original author; this repository evolves independently with a goal of community-driven development, and all contributors are welcome.

[Issues](https://github.com/wakaka6/just-talk-go/issues) are welcome for bug reports, usage feedback, and feature discussion — and pull requests are welcome too. See [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and requirements: open an issue first to discuss larger changes, and run `gofmt`, `go vet`, and `go test ./...` before submitting.

## License

Just Talk is licensed under the [GNU General Public License v3.0](LICENSE) (GPL-3.0-only).
