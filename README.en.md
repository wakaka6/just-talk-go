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
- Recording overlay: on macOS it expands from the notch and shows live recognized text, mic level, and the final result; Linux (Wayland/X11) and Windows get an always-on-top status capsule.
- Desktop settings window with a menu bar/tray icon for hotkeys (primary and secondary slots, recorded by clicking a slot and pressing the combo), mode, recognition engine and credentials, auto-submit, stop delay, hotwords, and overlay position — saved changes apply immediately without a restart. The same page opens in a browser with `--web`, and `--tui` keeps the terminal UI.
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
4. Press the `push_to_talk` hotkey (default `Alt+Super`; `Option+Command` on macOS) to start/stop recording. Recognized text is copied to the clipboard and, when `auto_submit` is on, submitted into the focused field.

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
mode = "toggle"                    # toggle | hold
push_to_talk = "Alt+Super"
language = "zh-CN"
auto_submit = true                 # auto-submit recognized text
stop_delay_ms = 0                  # release delay in hold mode
device = ""                        # recording device (empty = default)
gain = 0                           # microphone gain
app_key = "your-app-key"
access_key = "your-access-key"
resource_id = "volc.bigasr.sauc.duration"
hotwords = ["Wayland", "Sway", "wl-copy", "wtype", "just-talk-go"]

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
