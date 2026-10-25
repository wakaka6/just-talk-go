# Changelog
All notable project changes are tracked here.

## Unreleased

- Hot reload now also sees atomic saves. The watcher only reacted to write events, so an editor or script that writes a temp file and renames it over the config changed nothing until the next restart.
- `[voice] text_output` decides where recognized text goes: `paste`, `clipboard` or `none`. `none` transcribes without touching the focused field or the clipboard, which used to be possible only in continuous mode. The old `auto_submit` migrates on load (`true` → `paste`, `false` → `clipboard`) and is dropped from the file on the next save.
- `[voice] hotwords_file` reads hotwords from a text file (one word per line, `#` comments, `~` expanded) and merges them with `hotwords`. The file is re-read at the start of every session, so another program can keep the word list in step with the current context without editing the config or restarting; an unreadable file is a warning, not an error.
- The tray panel has a “语音输入” switch to turn voice input on or off without opening the settings window. It changes only `voice.enabled`, and an open settings window picks it up when focused without discarding its unsaved edits.
- Add `mode = "continuous"` (“持续转写”) for transcribing a meeting or system audio. It starts with Just Talk, registers no hotkeys, never types, pastes or touches the clipboard, and reconnects automatically whenever a session ends (backoff 0.5s–5s). The recording device stays open across reconnects, and about the last 5 seconds of audio captured during a reconnect are replayed into the new session. It needs a streaming engine.
- macOS now honors `[voice] device`: it matches an input device by UID, or by name ignoring case (e.g. `"BlackHole 2ch"`). An unknown device is an error that lists the available input devices; recording no longer silently uses the default microphone.
- The settings window and the TUI pick the recording device. Devices are grouped by what they capture, because recording your own voice and recording what the computer plays are different jobs: a microphone records the outside world, a loopback device (BlackHole, Loopback, an aggregate device…) records what the computer plays into it. A device named in the config but not connected right now stays in the list instead of being silently replaced, and macOS points out that capturing system audio needs a loopback driver when none is installed. What each platform can do differs — only macOS detects loopback devices reliably, and Windows cannot record playback at all — so the README has a support table.
- Add `[voice] stream_addr` (e.g. `"127.0.0.1:7590"`): a local WebSocket at `ws://<stream_addr>/asr` publishes session start/end events and every recognition result, including per-sentence `utterances` with their `definite` flag, in every mode. Only loopback addresses are accepted, and a slow subscriber is disconnected instead of slowing recognition down.

## v0.4.0 - 2026-10-17

- Just Talk is now a menu bar/tray app with a desktop settings window, the default when `just-talk` runs without flags. Saved changes apply immediately without a restart. Closing the window keeps Just Talk running, and a second launch brings the running window forward instead of starting another instance.
- The settings window has three tabs. “通用” covers hotkeys, mode, stop delay, language (a list of common languages plus auto-detect), hotwords (added with Enter and removed individually, duplicates dropped), auto-submit, and overlay position. “识别引擎” lists each engine as streaming or batch and whether it is configured; picking one that still needs credentials opens a sheet with the required fields first and the rest under “高级选项”, and cancelling it restores the previous engine. “活动” shows usage statistics and a timestamped timeline of each session, with streaming partials collapsed and a copy button on every final transcript.
- Left-clicking the menu bar/tray icon opens a compact panel with the live recording state, the latest transcript (with copy), usage statistics, and a summary of the current hotkeys, mode, engine, language, and output behavior. It hides when it loses focus; “打开设置” opens the settings window, “退出” quits, and right-click keeps the tray menu.
- Hotkeys are set by recording them: click the primary or secondary slot and press the combo (modifier-only combos such as Option+Command complete when a modifier is released; Esc cancels). Global hotkeys are paused while a slot records, so the current hotkey can be re-recorded without starting a session. Both slots map onto the comma-separated `push_to_talk`; extra hotkeys from a hand-edited config are kept, and keys the webview cannot see can still be typed via “手动编辑”.
- Add `--web` to open the same settings page in a browser. It listens on 127.0.0.1 only and prints a link carrying a per-run key; requests without it are refused.
- `--tui` now opens the terminal UI (previously the default); `--no-tui` stays daemon mode.
- The window uses the system webview (Wails v3). Linux builds now link GTK 3 + WebKitGTK 4.1 and need its runtime installed; Windows uses WebView2 and falls back to the TUI when it is missing. Build with `-tags nogui` to leave the window out.

## v0.3.0 - 2026-10-16

- Support the PC keyboard menu key (`Menu`, also `Apps`/`Application`) as a hotkey on macOS, Linux, and Windows. On macOS it was previously dropped as an unknown key.
- `push_to_talk` accepts several comma-separated hotkeys (e.g. `"Fn, Menu"`), so a built-in keyboard and an external keyboard can each use their own key. In `hold` mode recording stops only after every held hotkey is released. Existing single-hotkey configs are unchanged.
- macOS screens without a notch (external monitors, notch-less models) now show the full notch overlay growing out of the top center of the screen, including live recognized text, mic level, and the final result, instead of a text-less status capsule.

## v0.2.0 - 2026-10-10

- Add multi-engine ASR support behind a pluggable engine registry. `[voice] engine` selects `doubao-stream` (default), `dashscope` (Qwen Fun-ASR realtime streaming), or batch engines `openai`, `auralwise`, `mimo-asr`; per-engine credentials live under `[voice.engine_configs.<engine>]` and are kept separately. Streaming engines keep live captions; batch engines produce only the final result. Existing Doubao credentials keep working unchanged.
- TUI gains an engine picker and per-engine config modal: select 引擎 → Enter to choose a channel, then configure its keys in the follow-up modal; `o` opens the engine's documentation.

## v0.1.0 - 2026-10-05

- Add a macOS notch overlay. It is the new macOS default (`[overlay] position = "notch"`) and shows live recognized text, mic level, and a short pasted/copied result after each finished session. Cancelled or empty sessions show no result. The overlay follows the screen under the mouse pointer, picked at the start of each recording and kept for that recording, so no extra permission is needed. Screens without a notch show a top-center status capsule instead. Existing configs that set `position` explicitly keep it until changed in the TUI or by hand. Linux, Windows, and other macOS positions keep the original status capsule.
- Add `[overlay] show_text` (default `true`) to hide recognized text in the notch overlay.
- Refresh the macOS notch text as soon as a new partial ASR result arrives instead of on the next polling tick. New text replaces the old with a roughly 100 ms fade and no typewriter delay. ASR service latency itself is unchanged.
- Add an overlay position option (`提示位置`) to the TUI. It writes `[overlay] position` (`notch`, `top-left`, `top-center`, `top-right`, `bottom-left`, `bottom-center`, `bottom-right`) when saved. Saving other TUI settings keeps an explicitly configured position.
- Apply `[overlay]` config changes without restarting: `enabled` starts or stops the overlay, `show_text` and `idle_visible` apply on the next refresh, and `position` or `scale` changes replace the overlay window, which then shows the current recording state.
- Add macOS and Linux `Fn`/`Function` hotkey support. `Fn` can combine with ordinary keys, while `Fn+F1` through `Fn+F24` remain unsupported because macOS may treat them as system controls.
- Fix TUI configuration reloads retaining old ASR credentials: after saving with `s`, the next recording uses the updated App Key and Access Key without restarting.
- Add a responsive Chinese project introduction website with a simulated recording and R-triggered retry demo, platform-specific launch commands, static HTTP preview on port 7788, and automated GitHub Pages deployment linked from both READMEs.

## v0.0.3 - 2026-07-29

- Reworked Windows modifier-only hotkeys so the low-level hook only observes key edges and never consumes or replays modifiers. `Alt+Super` works through exact physical-state matching, stale hook state is cleared without combining separate single-key presses, and system shortcuts such as `Alt+Tab` remain untouched.

## v0.0.2 - 2026-07-21

- Fixed intermittent Windows `Alt+Super` false activations where stale low-level hook state could make a later Alt-only press look like the full shortcut; Windows modifier combinations now require an exact modifier set and reset stale suppressed state after physical release.

## v0.0.1 - 2026-07-17

- Added a tag-triggered GoReleaser v2 pipeline using the official GitHub Action to build Linux, macOS, and Windows amd64/arm64 archives on native runners and publish SHA256 checksums.
- Added Windows 10/11 support with global key-state monitoring, native WinMM microphone capture, Unicode clipboard access, SendInput auto-submit, a click-through Win32 status overlay, and Windows environment checks.
- Added Windows-standard config, state, log, and per-user installation paths. Windows builds no longer require external recording or clipboard commands.
- Added an opt-in Windows microphone integration test using `JUST_TALK_TEST_WINDOWS_AUDIO=1`.
- Added a low-level Windows keyboard-hook fallback so modifier-only shortcuts such as `Alt+Super` still work when `GetAsyncKeyState` misses the physical Windows key.
- Consume Windows modifier-only voice shortcuts such as `Alt+Super` so they do not activate menus or toolbars in the focused application, while replaying unrelated modifier shortcuts normally.
- Fixed recording shutdown getting stuck while ASR audio writes were still in flight, and accept empty final ASR responses without waiting for a false timeout.
- Redesigned the Windows status overlay with per-pixel transparency, antialiased animated waveform bars, smoother capsule edges, and a soft shadow.
- Clarified README build and install setup steps for the repository directory and `~/.local/bin` PATH.
- Restricted voice hotkeys to non-text global shortcut keys, rejecting letters, digits, punctuation, Space, and similar text-producing keys.
- Avoid duplicate auto-submit on KDE Plasma by using uinput directly and not writing the Wayland primary selection there.
- TUI is now the default startup mode.
- Added persistent usage statistics for total sessions, recognized characters, average speed, and recent speed.
- Added configurable ASR hotwords.
- Added TUI help toggle with `h`.
- Improved Wayland clipboard and auto-submit behavior with `wl-copy` and `wtype`.
- Added Linux recording status overlay for X11 and Wayland.
- Added macOS support for global hotkeys, native recording, clipboard, auto-submit, recording status overlay, and environment checks.
- Removed non-cgo macOS fallback builds; Just Talk now requires cgo for native platform integration.
- Replaced the old Claude-specific agent guide with `AGENTS.md` and clarified build documentation.
- Improved toggle and hold hotkey behavior for fast repeated key presses.
- Show ASR connection and final-result timeout errors in the status UI/overlay instead of immediately falling back to idle.
- Added transient `Esc` cancel and `R` retry hotkeys while recording or showing retryable errors.
- Improved X11 overlay placement on multi-monitor setups and switched X11 rendering to an ARGB window for smoother rounded corners.
- Fixed a Wayland overlay shutdown race that could crash while closing the app, and surfaced Linux `arecord` microphone/device failures in the UI.
- Made `Esc` cancel active overlay states, including the final ASR wait state, and suppress output from canceled pending sessions.
- Improved Wayland overlay rounded-corner antialiasing, especially on KDE Plasma.
- Added `just-talk --install` and `make install` to install the binary into `~/.local/bin`.

## 2026-05-30

- Initial Linux-focused development snapshot.
- Supported Linux Wayland hotkeys via evdev.
- Supported Linux X11 hotkeys via native X11 grabs.
- Added Doubao streaming ASR integration.
- Added TUI configuration interface.
- Added automatic clipboard copy and auto-submit.
