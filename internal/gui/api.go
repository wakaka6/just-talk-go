// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

// Package gui serves the settings page shared by the desktop window and the
// browser mode. The page is plain HTML/CSS/JS embedded in the binary; it
// talks to Go over the small JSON API in this file.
package gui

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/settings"
	"github.com/c/just-talk-go/plugins/voice"
)

//go:embed assets
var assetsFS embed.FS

// Options configures the page handler.
type Options struct {
	Config *config.Config
	// ConfigPath is the file this instance was started with. Empty falls back
	// to the usual lookup; set, it keeps a -config instance from saving over
	// someone else's config.
	ConfigPath string
	// Apply hands a saved config to the running engine.
	Apply    func(*config.Config) error
	Provider hotkey.ProviderInfo
	// Debug shows hotkey dispatch details in the status line.
	Debug bool
	// OpenURL opens a documentation link in the system browser. Nil in
	// browser mode, where the page opens links itself.
	OpenURL func(string)
	// PickFile shows the native open-file dialog starting next to current and
	// returns the chosen path, "" when cancelled. Nil in browser mode, where
	// the page cannot learn a picked file's path and the path is typed.
	PickFile func(current string) (string, error)
	// ShowSettings brings the settings window forward and Quit ends the
	// app; both are called from the tray panel. Nil in browser mode.
	ShowSettings func()
	Quit         func()
}

type server struct {
	opts Options
	mu   sync.Mutex
	cfg  *config.Config
	// suspended is set while the page records a hotkey: the voice hotkeys
	// are unregistered so the keys reach the page instead of starting a
	// session. resume restores them if the page stops asking.
	suspended bool
	resume    *time.Timer
}

// captureHold is how long hotkeys stay suspended without a keepalive.
const captureHold = 15 * time.Second

// Handler returns the page and its /api routes.
func Handler(opts Options) http.Handler {
	s := &server{opts: opts, cfg: opts.Config}
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/settings", s.save)
	mux.HandleFunc("POST /api/enabled", s.setEnabled)
	mux.HandleFunc("POST /api/continuous", s.setContinuous)
	mux.HandleFunc("POST /api/hotkey/check", s.checkHotkey)
	mux.HandleFunc("POST /api/hotkey/capture", s.capture)
	mux.HandleFunc("POST /api/open", s.open)
	mux.HandleFunc("POST /api/pick-file", s.pickFile)
	mux.HandleFunc("POST /api/window", s.window)
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets are embedded per build; revalidate so an upgraded binary
		// never runs an old script against a new API.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	return mux
}

type settingsJSON struct {
	Enabled      bool     `json:"enabled"`
	PushToTalk   string   `json:"pushToTalk"`
	Mode         string   `json:"mode"`
	Device       string   `json:"device"`
	Language     string   `json:"language"`
	TextOutput   string   `json:"textOutput"`
	StopDelayMs  int      `json:"stopDelayMs"`
	Hotwords     []string `json:"hotwords"`
	HotwordsFile string   `json:"hotwordsFile"`
	StreamAddr   string   `json:"streamAddr"`
	// Continuous* is the transcription service that runs alongside dictation.
	ContinuousEnabled bool   `json:"continuousEnabled"`
	ContinuousDevice  string `json:"continuousDevice"`
	ContinuousHotkey  string `json:"continuousHotkey"`
	OverlayPosition   string `json:"overlayPosition"`
	Engine            string `json:"engine"`
	// EngineConfigs carries only the engines whose fields the page edited.
	EngineConfigs map[string]map[string]string `json:"engineConfigs,omitempty"`
}

type fieldJSON struct {
	settings.Field
	Value string `json:"value"`
}

type engineJSON struct {
	settings.Engine
	Fields []fieldJSON `json:"fields"`
	// Configured reports the saved config; the settings page checks its
	// unsaved edits itself.
	Configured bool `json:"configured"`
}

type stateJSON struct {
	Platform         string              `json:"platform"`
	Shell            string              `json:"shell"`
	Backend          string              `json:"backend"`
	Debug            bool                `json:"debug"`
	ConfigPath       string              `json:"configPath"`
	Settings         settingsJSON        `json:"settings"`
	Engines          []engineJSON        `json:"engines"`
	OverlayPositions []string            `json:"overlayPositions"`
	Languages        []settings.Language `json:"languages"`
	Modes            []string            `json:"modes"`
	// TextOutputs are the valid 识别结果去向 values, in display order.
	TextOutputs []string `json:"textOutputs"`
	// Devices are the input devices found now; an empty device setting means
	// the system default. DevicesError explains an empty list.
	Devices      []voice.InputDevice `json:"devices"`
	DevicesError string              `json:"devicesError,omitempty"`
}

func (s *server) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.stateOf(cfg))
}

func (s *server) stateOf(cfg *config.Config) stateJSON {
	vc := cfg.Voice
	shell := "web"
	if s.opts.OpenURL != nil {
		shell = "app"
	}
	path := config.SavePath(s.opts.ConfigPath)
	st := stateJSON{
		Platform:   runtime.GOOS,
		Shell:      shell,
		Backend:    s.opts.Provider.Backend,
		Debug:      s.opts.Debug,
		ConfigPath: path,
		Settings: settingsJSON{
			Enabled:           vc.Enabled,
			PushToTalk:        canonicalHotkeys(vc.PushToTalk),
			Mode:              vc.Mode,
			Device:            vc.Device,
			Language:          vc.Language,
			TextOutput:        vc.TextOutput,
			StopDelayMs:       vc.StopDelayMs,
			Hotwords:          append([]string{}, vc.Hotwords...),
			HotwordsFile:      vc.HotwordsFile,
			StreamAddr:        vc.StreamAddr,
			ContinuousEnabled: vc.Continuous.Enabled,
			ContinuousDevice:  vc.Continuous.Device,
			ContinuousHotkey:  canonicalHotkeys(vc.Continuous.Hotkey),
			OverlayPosition:   cfg.Overlay.Position,
			Engine:            settings.CurrentEngine(vc),
		},
		OverlayPositions: settings.OverlayPositionOptions(cfg.Overlay.Position),
		Languages:        settings.LanguageOptions(vc.Language),
		Modes:            config.ModeOptions(),
		TextOutputs:      []string{config.OutputPaste, config.OutputClipboard, config.OutputNone},
	}
	if devices, err := voice.ListDevices(); err != nil {
		st.DevicesError = err.Error()
	} else {
		st.Devices = devices
	}
	engines := settings.Engines
	if current := st.Settings.Engine; !slices.ContainsFunc(engines, func(e settings.Engine) bool { return e.Name == current }) {
		engines = append(slices.Clone(engines), settings.EngineFor(current))
	}
	for _, e := range engines {
		ec := vc.EngineConfigFor(e.Name)
		ej := engineJSON{Engine: e, Fields: []fieldJSON{}, Configured: settings.EngineConfigured(vc, e.Name)}
		for _, f := range settings.EngineFields(e.Name) {
			ej.Fields = append(ej.Fields, fieldJSON{Field: f, Value: settings.EngineValue(vc, e.Name, ec, f.Key)})
		}
		st.Engines = append(st.Engines, ej)
	}
	return st
}

type statusJSON struct {
	State           string `json:"state"`
	Detail          string `json:"detail"`
	StopInMs        int64  `json:"stopInMs"`
	ErrorHoldS      int64  `json:"errorHoldS"`
	PendingFinishes int    `json:"pendingFinishes"`
	SessionID       uint64 `json:"sessionId"`
	// StreamURL is where subscribers connect; empty when the server is off.
	StreamURL         string `json:"streamUrl"`
	StreamSubscribers int    `json:"streamSubscribers"`
	// ContinuousOn is the service's running switch, which its hotkey can flip
	// without touching the saved setting.
	ContinuousOn bool     `json:"continuousOn"`
	Debug        []string `json:"debug,omitempty"`
}

type statsJSON struct {
	Sessions    uint64 `json:"sessions"`
	Chars       uint64 `json:"chars"`
	AudioMs     int64  `json:"audioMs"`
	LastChars   int    `json:"lastChars"`
	LastAudioMs int64  `json:"lastAudioMs"`
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	vs := voice.TUIStatus()
	st := statusJSON{
		State:           vs.State,
		Detail:          vs.Detail,
		PendingFinishes: vs.PendingFinishes,
		SessionID:       vs.SessionID,
	}
	st.StreamURL, st.StreamSubscribers = voice.StreamStatus()
	st.ContinuousOn = voice.ContinuousOn()
	if !vs.StopAt.IsZero() {
		st.StopInMs = max(vs.StopAt.Sub(now).Milliseconds(), 0)
	}
	if vs.State == "error" && !vs.ErrorUntil.IsZero() {
		st.ErrorHoldS = max(int64(vs.ErrorUntil.Sub(now).Seconds()), 0)
	}
	if s.opts.Debug {
		age := func(t time.Time) int64 { return max(now.Sub(t).Milliseconds(), 0) }
		if !vs.UpdatedAt.IsZero() {
			st.Debug = append(st.Debug, fmt.Sprintf("更新 %dms 前", age(vs.UpdatedAt)))
		}
		if !vs.LastHotkeyAt.IsZero() {
			st.Debug = append(st.Debug, fmt.Sprintf("热键 %s %dms 前", vs.LastHotkeyType, age(vs.LastHotkeyAt)))
		}
		if !vs.LastHandledAt.IsZero() {
			st.Debug = append(st.Debug, fmt.Sprintf("处理 %s %dms 前", vs.LastHandledType, age(vs.LastHandledAt)))
		}
		if vs.QueuedHotkeys > 0 || vs.HandledHotkeys > 0 {
			st.Debug = append(st.Debug, fmt.Sprintf("入队/处理 %d/%d q=%d", vs.QueuedHotkeys, vs.HandledHotkeys, vs.EventQueueLen))
		}
	}
	stats := voice.TUIStats()
	type logJSON struct {
		Time int64  `json:"time"` // unix ms
		Text string `json:"text"`
	}
	entries := voice.TUILogEntries()
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}
	logs := make([]logJSON, len(entries))
	for i, e := range entries {
		logs[i] = logJSON{Time: e.Time.UnixMilli(), Text: e.Text}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": st,
		"stats": statsJSON{
			Sessions:    stats.Sessions,
			Chars:       stats.Chars,
			AudioMs:     stats.AudioDuration.Milliseconds(),
			LastChars:   stats.LastTextChars,
			LastAudioMs: stats.LastAudioDuration.Milliseconds(),
		},
		"logs": logs,
	})
}

func (s *server) save(w http.ResponseWriter, r *http.Request) {
	var in settingsJSON
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := applySettings(s.cfg, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.commitLocked(w, next)
}

// setEnabled turns voice input on or off ({"enabled": bool}) without
// touching any other setting, for the tray panel's switch.
func (s *server) setEnabled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := *s.cfg
	next.Voice.Enabled = in.Enabled
	s.commitLocked(w, &next)
}

// setContinuous turns continuous transcription on or off ({"enabled": bool})
// for the tray panel's switch. It saves the setting, so the choice survives a
// restart, and switches the running service too: after its hotkey turned it
// on over a saved "off", saving "off" changes nothing in the config.
func (s *server) setContinuous(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := *s.cfg
	next.Voice.Continuous.Enabled = in.Enabled
	resp, err := s.applyLocked(&next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	voice.SetContinuous(in.Enabled)
	writeJSON(w, http.StatusOK, resp)
}

// commitLocked saves next, hands it to the engine and replies with the new
// state.
func (s *server) commitLocked(w http.ResponseWriter, next *config.Config) {
	resp, err := s.applyLocked(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// applyLocked saves next and hands it to the engine, returning the reply.
func (s *server) applyLocked(next *config.Config) (map[string]any, error) {
	if err := config.Save(next, s.opts.ConfigPath); err != nil {
		return nil, errors.New("保存失败: " + err.Error())
	}
	s.cfg = next
	s.stopResumeLocked()
	s.suspended = false
	resp := map[string]any{"state": s.stateOf(next)}
	if s.opts.Apply != nil {
		if err := s.opts.Apply(next); err != nil {
			resp["warning"] = "已保存，但热键注册失败: " + err.Error()
		}
	}
	return resp, nil
}

// applySettings returns a copy of cur with the page's values, or a
// user-facing error when a value is invalid. cur is never modified.
func applySettings(cur *config.Config, in settingsJSON) (*config.Config, error) {
	pushToTalk := strings.TrimSpace(in.PushToTalk)
	if err := settings.ValidateHotkeys(pushToTalk); err != nil {
		return nil, err
	}
	continuousHotkey := strings.TrimSpace(in.ContinuousHotkey)
	if err := settings.ValidateContinuousHotkey(continuousHotkey, pushToTalk); err != nil {
		return nil, err
	}
	if !config.ValidMode(in.Mode) {
		return nil, fmt.Errorf("未知模式: %s", in.Mode)
	}
	if !config.ValidTextOutput(in.TextOutput) {
		return nil, fmt.Errorf("未知输出去向: %s", in.TextOutput)
	}
	if err := voice.CheckStreamAddr(in.StreamAddr); err != nil {
		return nil, err
	}
	if in.StopDelayMs < 0 {
		return nil, errors.New("停止延迟不能为负数")
	}
	if !slices.Contains(settings.OverlayPositionOptions(cur.Overlay.Position), in.OverlayPosition) {
		return nil, fmt.Errorf("未知提示位置: %s", in.OverlayPosition)
	}
	engine := strings.TrimSpace(in.Engine)
	if engine == "" {
		return nil, errors.New("请选择识别引擎")
	}

	next := *cur
	vc := &next.Voice
	vc.Hotwords = settings.SplitList(strings.Join(in.Hotwords, "\n"))
	vc.EngineConfigs = make(map[string]map[string]string, len(cur.Voice.EngineConfigs))
	for name, ec := range cur.Voice.EngineConfigs {
		vc.EngineConfigs[name] = cloneMap(ec)
	}
	vc.Enabled = in.Enabled
	vc.PushToTalk = pushToTalk
	vc.Mode = in.Mode
	// Any name is accepted: a device may be unplugged while being configured.
	// The recorder reports an unknown device when it starts.
	vc.Device = strings.TrimSpace(in.Device)
	vc.Language = strings.TrimSpace(in.Language)
	vc.TextOutput = in.TextOutput
	vc.HotwordsFile = strings.TrimSpace(in.HotwordsFile)
	if vc.StreamAddr = strings.TrimSpace(in.StreamAddr); vc.StreamAddr == "" {
		vc.StreamAddr = config.DefaultStreamAddr
	}
	vc.Continuous.Enabled = in.ContinuousEnabled
	vc.Continuous.Device = strings.TrimSpace(in.ContinuousDevice)
	vc.Continuous.Hotkey = continuousHotkey
	vc.StopDelayMs = in.StopDelayMs
	vc.Engine = engine
	next.Overlay.Position = in.OverlayPosition

	for name, values := range in.EngineConfigs {
		fields := settings.EngineFields(name)
		if fields == nil {
			continue
		}
		ec := cloneMap(vc.EngineConfigFor(name))
		for _, f := range fields {
			v, ok := values[f.Key]
			if !ok {
				continue
			}
			if v = strings.TrimSpace(v); v == "" {
				delete(ec, f.Key)
			} else {
				ec[f.Key] = v
			}
		}
		vc.SetEngineConfig(name, ec)
	}
	return &next, nil
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// canonicalHotkeys rewrites a push_to_talk list in the names the page
// records ("option+command" becomes "Alt+Super"), so an unchanged list
// compares equal. A list that does not parse is returned as written.
func canonicalHotkeys(s string) string {
	combos, err := config.ParseHotkeyList(s)
	if err != nil {
		return s
	}
	names := make([]string, len(combos))
	for i, c := range combos {
		names[i] = c.String()
	}
	return strings.Join(names, ", ")
}

// checkHotkey validates one recorded hotkey and returns its config name.
func (s *server) checkHotkey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	combo, err := config.ParseHotkey(in.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无法识别的热键: "+in.Value)
		return
	}
	name := combo.String()
	if err := settings.ValidateHotkeys(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"value": name})
}

// capture suspends the voice hotkeys while the page records one
// ({"active": true}, repeated as a keepalive) and restores them after.
func (s *server) capture(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !in.Active {
		s.resumeLocked()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.opts.Apply == nil || !s.cfg.Voice.Enabled {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.stopResumeLocked()
	s.resume = time.AfterFunc(captureHold, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.resumeLocked()
	})
	if !s.suspended {
		off := *s.cfg
		off.Voice.Enabled = false
		if err := s.opts.Apply(&off); err != nil {
			writeError(w, http.StatusInternalServerError, "暂停热键失败: "+err.Error())
			return
		}
		s.suspended = true
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) resumeLocked() {
	s.stopResumeLocked()
	if !s.suspended {
		return
	}
	s.suspended = false
	if err := s.opts.Apply(s.cfg); err != nil {
		slog.Warn("restore hotkeys after recording failed", "error", err)
	}
}

func (s *server) stopResumeLocked() {
	if s.resume != nil {
		s.resume.Stop()
		s.resume = nil
	}
}

func (s *server) open(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// Only catalog links: the page must not become a way to launch
	// arbitrary URLs or file paths.
	if s.opts.OpenURL == nil || !slices.ContainsFunc(settings.Engines, func(e settings.Engine) bool { return e.Doc == in.URL }) {
		writeError(w, http.StatusForbidden, "不支持打开该链接")
		return
	}
	s.opts.OpenURL(in.URL)
	w.WriteHeader(http.StatusNoContent)
}

// pickFile lets the user choose a file: {"path": current} → {"path": chosen},
// "" when cancelled.
func (s *server) pickFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if s.opts.PickFile == nil {
		writeError(w, http.StatusNotFound, "不支持该操作")
		return
	}
	path, err := s.opts.PickFile(strings.TrimSpace(in.Path))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("无法打开文件选择框: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// window runs a tray panel action: {"action": "settings"} or {"action": "quit"}.
func (s *server) window(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	var act func()
	switch in.Action {
	case "settings":
		act = s.opts.ShowSettings
	case "quit":
		act = s.opts.Quit
	}
	if act == nil {
		writeError(w, http.StatusNotFound, "不支持该操作")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	act()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
