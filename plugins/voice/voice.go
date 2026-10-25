package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/engine"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/asrcfg"
	"github.com/c/just-talk-go/internal/autotype"
	"github.com/c/just-talk-go/internal/clipboard"
	"github.com/c/just-talk-go/internal/speech"
)

const (
	defaultStopDelayMs = 800
	errorHoldDuration  = 10 * time.Second
	audioSendTimeout   = 3 * time.Second
	audioDrainTimeout  = 4 * time.Second
)

var (
	cancelRecordingCombo = hotkey.Combo{Mods: hotkey.ModNone, Key: hotkey.KeyEscape}
	retryErrorCombo      = hotkey.Combo{Mods: hotkey.ModNone, Key: hotkey.KeyR}
)

var TUILog func(string)
var tuiLogBuf []LogEntry
var tuilogMu sync.Mutex
var outputMu sync.Mutex
var outputWriter io.Writer = os.Stdout
var tuiStatus = TUIVoiceStatus{State: "idle", UpdatedAt: time.Now()}
var tuiStatusMu sync.Mutex
var tuiStats TUIVoiceStats
var tuiStatsMu sync.Mutex

type TUIVoiceStatus struct {
	State           string
	Detail          string
	Recording       bool
	Stopping        bool
	StopAt          time.Time
	ErrorUntil      time.Time
	UpdatedAt       time.Time
	SessionID       uint64
	PendingFinishes int
	LastHotkeyAt    time.Time
	LastHotkeyType  string
	LastHandledAt   time.Time
	LastHandledType string
	QueuedHotkeys   uint64
	HandledHotkeys  uint64
	EventQueueLen   int
}

type TUIVoiceStats struct {
	Sessions          uint64
	Chars             uint64
	AudioDuration     time.Duration
	LastTextChars     int
	LastAudioDuration time.Duration
}

func pout(format string, args ...interface{}) {
	msg := strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " ")
	if TUILog != nil {
		TUILog(msg)
		return
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	if outputWriter != nil {
		fmt.Fprint(outputWriter, msg)
	}
}

func SetOutput(w io.Writer) {
	outputMu.Lock()
	outputWriter = w
	outputMu.Unlock()
}

func SetupTUILog() {
	TUILog = func(msg string) {
		tuilogMu.Lock()
		entry := LogEntry{Time: time.Now(), Text: msg}
		// A streaming partial ("\r…") replaces the one before it, so one
		// dictation cannot push earlier sessions out of the buffer.
		if n := len(tuiLogBuf); n > 0 && isPartialLog(msg) && isPartialLog(tuiLogBuf[n-1].Text) {
			tuiLogBuf[n-1] = entry
		} else {
			tuiLogBuf = append(tuiLogBuf, entry)
		}
		if len(tuiLogBuf) > 200 {
			tuiLogBuf = tuiLogBuf[len(tuiLogBuf)-100:]
		}
		tuilogMu.Unlock()
	}
}

func DisableTUILog() {
	tuilogMu.Lock()
	TUILog = nil
	tuiLogBuf = nil
	tuilogMu.Unlock()
}

// LogEntry is one buffered voice log line and when it was written.
type LogEntry struct {
	Time time.Time
	Text string
}

// isPartialLog reports a streaming partial result line, written as "\r🎤 …".
func isPartialLog(msg string) bool {
	return strings.HasPrefix(msg, "\r")
}

// TUILogs returns a copy of the buffered voice log lines, oldest first.
func TUILogs() []string {
	tuilogMu.Lock()
	defer tuilogMu.Unlock()
	lines := make([]string, len(tuiLogBuf))
	for i, e := range tuiLogBuf {
		lines[i] = e.Text
	}
	return lines
}

// TUILogEntries returns a copy of the buffered voice log, oldest first.
func TUILogEntries() []LogEntry {
	tuilogMu.Lock()
	defer tuilogMu.Unlock()
	return append([]LogEntry(nil), tuiLogBuf...)
}

func TUIStats() TUIVoiceStats {
	tuiStatsMu.Lock()
	defer tuiStatsMu.Unlock()
	return tuiStats
}

func recordTUIStats(text string, audioDuration time.Duration) {
	chars := countTextRunes(text)
	tuiStatsMu.Lock()
	tuiStats.Sessions++
	tuiStats.Chars += uint64(chars)
	tuiStats.AudioDuration += audioDuration
	tuiStats.LastTextChars = chars
	tuiStats.LastAudioDuration = audioDuration
	snapshot := tuiStats
	tuiStatsMu.Unlock()
	go saveTUIStats(snapshot)
}

func countTextRunes(text string) int {
	n := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

type persistedStats struct {
	Sessions        uint64 `json:"sessions"`
	Chars           uint64 `json:"chars"`
	AudioDurationMs int64  `json:"audio_duration_ms"`
}

func loadTUIStats() {
	data, err := os.ReadFile(statsPath())
	if err != nil {
		return
	}
	var ps persistedStats
	if json.Unmarshal(data, &ps) != nil {
		return
	}
	tuiStatsMu.Lock()
	tuiStats.Sessions = ps.Sessions
	tuiStats.Chars = ps.Chars
	tuiStats.AudioDuration = time.Duration(ps.AudioDurationMs) * time.Millisecond
	tuiStats.LastTextChars = 0
	tuiStats.LastAudioDuration = 0
	tuiStatsMu.Unlock()
}

func saveTUIStats(stats TUIVoiceStats) {
	path := statsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	ps := persistedStats{
		Sessions:        stats.Sessions,
		Chars:           stats.Chars,
		AudioDurationMs: stats.AudioDuration.Milliseconds(),
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func statsPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		if cache, err := os.UserCacheDir(); runtime.GOOS == "windows" && err == nil {
			base = cache
		} else if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".local", "state")
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "just-talk", "stats.json")
}

// idleDetail is what the status line says when nothing is happening. It
// depends on the mode (a hotkey to press, or speech to wait for), so an
// expiring error must reuse the last one instead of naming a hotkey.
var idleDetail = "等待热键"

func TUIStatus() TUIVoiceStatus {
	tuiStatusMu.Lock()
	defer tuiStatusMu.Unlock()
	if tuiStatus.State == "error" && !tuiStatus.ErrorUntil.IsZero() && time.Now().After(tuiStatus.ErrorUntil) {
		tuiStatus.State = "idle"
		tuiStatus.Detail = idleDetail
		tuiStatus.Recording = false
		tuiStatus.Stopping = false
		tuiStatus.StopAt = time.Time{}
		tuiStatus.ErrorUntil = time.Time{}
		tuiStatus.UpdatedAt = time.Now()
	}
	return tuiStatus
}

func setTUIStatus(update func(*TUIVoiceStatus)) {
	tuiStatusMu.Lock()
	defer tuiStatusMu.Unlock()
	update(&tuiStatus)
	tuiStatus.UpdatedAt = time.Now()
}

func markTUIHotkey(evt hotkey.Event) {
	tuiStatusMu.Lock()
	defer tuiStatusMu.Unlock()
	tuiStatus.LastHotkeyAt = time.Now()
	tuiStatus.LastHotkeyType = evt.Type.String()
	tuiStatus.UpdatedAt = time.Now()
}

func markTUIQueued(evt hotkey.Event, queueLen int) {
	tuiStatusMu.Lock()
	defer tuiStatusMu.Unlock()
	tuiStatus.LastHotkeyAt = time.Now()
	tuiStatus.LastHotkeyType = evt.Type.String()
	tuiStatus.QueuedHotkeys++
	tuiStatus.EventQueueLen = queueLen
	tuiStatus.UpdatedAt = time.Now()
}

func markTUIHandled(evt hotkey.Event) {
	tuiStatusMu.Lock()
	defer tuiStatusMu.Unlock()
	tuiStatus.LastHandledAt = time.Now()
	tuiStatus.LastHandledType = evt.Type.String()
	tuiStatus.HandledHotkeys++
	tuiStatus.UpdatedAt = time.Now()
}

type VoicePlugin struct {
	env       engine.PluginEnv
	logger    *slog.Logger
	cfg       *config.Config
	mu        sync.Mutex
	eventOnce sync.Once
	events    chan hotkey.Event
	combos    []hotkey.Combo
	// heldCombos tracks hold-mode hotkeys currently pressed; recording stops
	// only after every one of them is released.
	heldCombos             map[hotkey.Combo]bool
	mode                   string
	recording              bool
	stopping               bool
	holdReleased           bool
	userStopped            bool
	stopTimer              *time.Timer
	stopAt                 time.Time
	startedAt              time.Time
	sessionID              uint64
	sessionGen             uint64
	recorder               *Recorder
	asrClient              speech.StreamSession
	asrCancel              context.CancelFunc
	audioDone              <-chan struct{}
	textOutput             string
	stopDelayMs            int
	pendingDone            int
	finishingSessions      map[uint64]struct{}
	canceledSessions       map[uint64]struct{}
	outputSessions         map[uint64]struct{}
	errorUntil             time.Time
	errorTimer             *time.Timer
	lastError              string
	flowActive             bool
	cancelHotkeyRegistered bool
	retryHotkeyRegistered  bool
	// streamPrefix makes published session ids unique across restarts.
	streamPrefix string
	stream       *streamServer
	streamAddr   string
	// hotwords reads hotwords_file at the start of every session.
	hotwords hotwordsLoader
	// runCtx is the Start context; nil before Start and after Stop.
	runCtx     context.Context
	contCancel context.CancelFunc
	contDone   chan struct{}
	contCfg    config.VoiceConfig
	// contState and contDetail are what the transcription service is doing.
	// It shares one status line with dictation, which outranks it.
	contState  string
	contDetail string
	contCombos []hotkey.Combo
	// contEnabled is the live on/off that the hotkey flips; contConfigured is
	// the last value the config asked for, so editing the setting still wins
	// over an earlier toggle.
	contEnabled    bool
	contConfigured bool
	// contSync wakes the worker that starts and stops the service. Tearing it
	// down can take seconds, and the hotkey callback runs on the global event
	// tap: doing it there would stall every keystroke on the machine.
	contSync     chan struct{}
	contSyncOnce sync.Once
}

type recordingSession struct {
	sessionID   uint64
	recorder    *Recorder
	asrClient   speech.StreamSession
	asrCancel   context.CancelFunc
	audioDone   <-chan struct{}
	textOutput  string
	userStopped bool
	startedAt   time.Time
}

func NewVoicePlugin() *VoicePlugin {
	return &VoicePlugin{
		stopDelayMs:  defaultStopDelayMs,
		streamPrefix: "jt-" + time.Now().Format("20060102T150405"),
	}
}

func (p *VoicePlugin) Name() string    { return "voice" }
func (p *VoicePlugin) Version() string { return "0.6.0" }

func (p *VoicePlugin) Init(env engine.PluginEnv) error {
	p.env = env
	p.logger = env.Logger()
	p.cfg = env.Config()
	loadTUIStats()
	return p.registerFromConfig(env.Config())
}

func (p *VoicePlugin) Start(ctx context.Context) error {
	p.logger.Info("voice plugin started", "mode", p.mode)
	p.startEventWorker(ctx)
	p.startContSyncWorker(ctx)
	p.mu.Lock()
	p.runCtx = ctx
	p.publishStatusLocked()
	p.mu.Unlock()
	p.syncContinuous()
	<-ctx.Done()
	return ctx.Err()
}

func (p *VoicePlugin) Stop() error {
	p.mu.Lock()
	p.runCtx = nil
	p.mu.Unlock()
	p.syncContinuous()
	p.mu.Lock()
	session := p.detachRecordingLocked()
	p.trackFinishLocked(session)
	p.publishStatusLocked()
	p.mu.Unlock()
	p.finishRecordingSession(session)
	p.mu.Lock()
	p.syncStreamServerLocked("")
	p.mu.Unlock()
	return nil
}

func (p *VoicePlugin) OnConfigReload(cfg *config.Config) error { return p.registerFromConfig(cfg) }

func (p *VoicePlugin) registerFromConfig(cfg *config.Config) error {
	p.mu.Lock()
	p.cfg = cfg
	p.syncStreamServerLocked(cfg.Voice.StreamAddr)
	p.mu.Unlock()
	vc := cfg.Voice
	if !vc.Enabled {
		p.mu.Lock()
		oldCombos := p.combos
		oldCont := p.contCombos
		p.cleanupTransientHotkeysLocked()
		p.combos = nil
		p.contCombos = nil
		clear(p.heldCombos)
		p.mu.Unlock()
		for _, c := range append(oldCombos, oldCont...) {
			p.env.UnregisterHotkey(c)
		}
		p.logger.Info("voice disabled")
		p.syncContinuous()
		return nil
	}
	mode := vc.Mode
	if mode == "" {
		mode = config.ModeHold
	}
	combos, err := config.ParseHotkeyList(vc.PushToTalk)
	if err != nil {
		return fmt.Errorf("parse hotkey %q: %w", vc.PushToTalk, err)
	}
	for _, c := range combos {
		if err := validateVoiceHotkey(c); err != nil {
			return err
		}
	}
	contCombos, err := config.ParseHotkeyList(vc.Continuous.Hotkey)
	if err != nil {
		return fmt.Errorf("parse hotkey %q: %w", vc.Continuous.Hotkey, err)
	}
	for _, c := range contCombos {
		if err := validateVoiceHotkey(c); err != nil {
			return err
		}
		if slices.Contains(combos, c) {
			return fmt.Errorf("%s 已经是听写热键，不能再用来开关持续转写", c)
		}
	}

	p.mu.Lock()
	oldCombos := p.combos
	modeChanged := p.mode != mode
	p.combos = combos
	p.mode = mode
	p.textOutput = vc.TextOutput
	p.stopDelayMs = vc.StopDelayMs
	oldCont := p.contCombos
	p.contCombos = contCombos
	// An edited setting wins over an earlier toggle; a reload that leaves it
	// alone leaves the hotkey's choice alone.
	if vc.Continuous.Enabled != p.contConfigured {
		p.contEnabled, p.contConfigured = vc.Continuous.Enabled, vc.Continuous.Enabled
	}
	for c := range p.heldCombos {
		if modeChanged || !slices.Contains(combos, c) {
			delete(p.heldCombos, c)
		}
	}
	p.mu.Unlock()

	p.logger.Info("config_reloaded", "hotkeys", combos, "mode", mode,
		"text_output", vc.TextOutput, "stop_delay_ms", vc.StopDelayMs)

	// Suppress depends on mode, so a mode change re-registers every combo;
	// otherwise only added/removed combos are touched.
	var registered []hotkey.Combo
	for _, c := range oldCombos {
		if modeChanged || !slices.Contains(combos, c) {
			p.env.UnregisterHotkey(c)
		} else {
			registered = append(registered, c)
		}
	}
	for _, c := range combos {
		if !modeChanged && slices.Contains(oldCombos, c) {
			continue
		}
		suppress := mode == config.ModeHold
		if runtime.GOOS == "windows" && c.IsModifierOnly() {
			suppress = false
		}
		opts := hotkey.RegisterOptions{Suppress: suppress}
		if err := p.env.RegisterHotkeyWithOptions(c, opts, p.onHotkey); err != nil {
			// Remember only what is actually registered so the next reload
			// retries the failed combo instead of assuming it is in place.
			p.mu.Lock()
			p.combos = registered
			p.mu.Unlock()
			return fmt.Errorf("register hotkey %s: %w", c, err)
		}
		registered = append(registered, c)
		p.logger.Info("hotkey_registered", "combo", c, "suppress", opts.Suppress)
	}
	for _, c := range oldCont {
		if !slices.Contains(contCombos, c) {
			p.env.UnregisterHotkey(c)
		}
	}
	for _, c := range contCombos {
		if slices.Contains(oldCont, c) {
			continue
		}
		// A tap, never held: no reason to swallow the key from other apps.
		if err := p.env.RegisterHotkeyWithOptions(c, hotkey.RegisterOptions{}, p.onContinuousHotkey); err != nil {
			return fmt.Errorf("register hotkey %s: %w", c, err)
		}
		p.logger.Info("hotkey_registered", "combo", c, "purpose", "continuous")
	}
	p.syncContinuous()
	return nil
}

func (p *VoicePlugin) onHotkey(evt hotkey.Event) {
	markTUIHotkey(evt)
	p.logger.Debug("voice hotkey received", "type", evt.Type, "combo", evt.Combo)
	p.startEventWorker(p.env.Engine().Context())
	p.mu.Lock()
	mode := p.mode
	p.mu.Unlock()
	if mode == config.ModeToggle && evt.Type != hotkey.KeyDown {
		p.logger.Debug("voice hotkey ignored", "type", evt.Type, "mode", mode)
		return
	}
	p.events <- evt
	markTUIQueued(evt, len(p.events))
	p.logger.Debug("voice hotkey queued", "type", evt.Type, "queue_len", len(p.events))
}

func (p *VoicePlugin) onCancelHotkey(evt hotkey.Event) {
	if evt.Type != hotkey.KeyDown {
		return
	}
	p.cancelRecording()
}

// onContinuousHotkey turns the transcription service on and off. This hotkey
// bounds the service, not a sentence, so it only toggles: nobody holds a key
// down for a whole meeting. It changes the running state, not the config.
func (p *VoicePlugin) onContinuousHotkey(evt hotkey.Event) {
	if evt.Type != hotkey.KeyDown {
		return
	}
	markTUIHandled(evt)
	p.mu.Lock()
	p.contEnabled = !p.contEnabled
	on := p.contEnabled
	p.mu.Unlock()
	p.logger.Info("continuous toggled by hotkey", "enabled", on)
	if on {
		pout("\U0001f3a7 持续转写：开")
	} else {
		pout("\U0001f3a7 持续转写：关")
	}
	// Hand the work to the sync worker; see contSync.
	select {
	case p.contSync <- struct{}{}:
	default:
	}
}

// startContSyncWorker serialises start/stop so two quick presses cannot run
// two teardowns at once. A pending wake is enough: syncContinuous always acts
// on the current desired state.
func (p *VoicePlugin) startContSyncWorker(ctx context.Context) {
	p.contSyncOnce.Do(func() {
		p.contSync = make(chan struct{}, 1)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-p.contSync:
					p.syncContinuous()
				}
			}
		}()
	})
}

func (p *VoicePlugin) onRetryHotkey(evt hotkey.Event) {
	if evt.Type != hotkey.KeyDown {
		return
	}
	p.retryLastError()
}

func (p *VoicePlugin) startEventWorker(ctx context.Context) {
	p.eventOnce.Do(func() {
		p.events = make(chan hotkey.Event, 256)
		go p.eventLoop(ctx)
	})
}

func (p *VoicePlugin) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-p.events:
			p.handleHotkey(evt)
		}
	}
}

func (p *VoicePlugin) handleHotkey(evt hotkey.Event) {
	markTUIHandled(evt)
	p.mu.Lock()
	mode, rec, stopping := p.mode, p.recording, p.stopping
	p.mu.Unlock()
	p.logger.Debug("voice hotkey handling", "type", evt.Type, "mode", mode, "recording", rec, "stopping", stopping)
	switch mode {
	case config.ModeHold:
		if evt.Type == hotkey.KeyDown {
			p.setHeld(evt.Combo, true)
			p.clearHoldReleased()
			if stopping {
				p.cancelStopDelay()
			} else if !rec {
				p.startRecording()
			}
		} else if evt.Type == hotkey.KeyUp && !p.setHeld(evt.Combo, false) {
			p.markHoldReleased()
		}
	case config.ModeToggle:
		if evt.Type != hotkey.KeyDown {
			return
		}
		if !rec {
			p.startRecording()
		} else if p.stopping {
			p.restartRecording()
		} else {
			p.startStopDelay()
		}
	}
}

func (p *VoicePlugin) startStopDelay() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping || !p.recording {
		return
	}
	p.holdReleased = false
	p.stopping, p.userStopped = true, true
	delay := time.Duration(p.stopDelayMs) * time.Millisecond
	p.stopAt = time.Now().Add(delay)
	p.stopTimer = time.AfterFunc(delay, func() {
		p.stopRecordingAsync()
	})
	p.publishStatusLocked()
	pout("🎤 即将停止... (%dms 缓冲)", p.stopDelayMs)
}

func (p *VoicePlugin) cancelStopDelay() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelStopDelayLocked()
}

func (p *VoicePlugin) cancelStopDelayLocked() {
	p.cancelStopDelayOnlyLocked()
	p.holdReleased = false
	p.publishStatusLocked()
}

func (p *VoicePlugin) cancelStopDelayOnlyLocked() {
	if p.stopTimer != nil {
		p.stopTimer.Stop()
		p.stopTimer = nil
	}
	if p.stopping {
		pout("🎤 继续录音")
	}
	p.stopping = false
	p.stopAt = time.Time{}
}

func (p *VoicePlugin) clearHoldReleased() {
	p.mu.Lock()
	p.holdReleased = false
	p.mu.Unlock()
}

// setHeld records a hold-mode hotkey press or release and reports whether any
// voice hotkey is still held afterwards.
func (p *VoicePlugin) setHeld(combo hotkey.Combo, down bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if down {
		if p.heldCombos == nil {
			p.heldCombos = make(map[hotkey.Combo]bool)
		}
		p.heldCombos[combo] = true
	} else {
		delete(p.heldCombos, combo)
	}
	return len(p.heldCombos) > 0
}

func (p *VoicePlugin) markHoldReleased() {
	p.mu.Lock()
	p.holdReleased = true
	shouldStop := p.recording && !p.stopping
	p.mu.Unlock()
	if shouldStop {
		p.startStopDelay()
	}
}

func (p *VoicePlugin) startRecording() {
	p.mu.Lock()
	p.cancelStopDelayOnlyLocked()
	if p.recording || p.asrClient != nil {
		p.mu.Unlock()
		pout("⚠️  已经在录音中")
		return
	}
	vc := p.cfg.Voice
	var rec *Recorder
	if vc.Device != "" {
		rec = NewRecorderWithDevice(p.logger, vc.Device, vc.Gain)
	} else {
		rec = NewRecorder(p.logger, vc.Gain)
	}
	if err := rec.Start(); err != nil {
		p.sessionID++
		sessionID := p.sessionID
		p.publishErrorLocked("录音启动失败: "+shortError(err), sessionID)
		p.mu.Unlock()
		pout("❌ 录音启动失败: %v", err)
		return
	}
	pout("🎤 开始录音... (后端: %s)", rec.Backend())
	ctx, cancel := context.WithCancel(context.Background())
	p.sessionID++
	p.sessionGen++
	sessionID := p.sessionID
	sessionGen := p.sessionGen
	startedAt := time.Now()
	resetOverlaySession(sessionID)
	activeRecorder.Store(rec)
	p.recorder, p.recording, p.userStopped = rec, true, false
	p.startedAt = startedAt
	p.stopping = false
	p.stopAt = time.Time{}
	p.clearErrorLocked()
	p.asrCancel = cancel
	shouldStopImmediately := p.mode == config.ModeHold && p.holdReleased
	p.publishStatusLocked()
	p.mu.Unlock() // Release lock before slow WebSocket dial

	go p.connectASR(ctx, cancel, sessionID, sessionGen, rec)
	if shouldStopImmediately {
		p.startStopDelay()
	}
}

func (p *VoicePlugin) connectASR(ctx context.Context, cancel context.CancelFunc, sessionID, sessionGen uint64, rec *Recorder) {
	vc := p.cfg.Voice
	eng, err := asrcfg.Build(vc)
	params := speech.StreamParams{Lang: vc.Language, Hotwords: p.hotwords.load(p.logger, vc)}
	var client speech.StreamSession
	if err == nil {
		if streaming, ok := eng.(speech.StreamingEngine); ok {
			client, err = streaming.NewStream(ctx, params)
		} else {
			client = speech.NewBatchSession(eng, params)
		}
	}
	if err == nil {
		err = client.Connect(ctx)
	}
	if err != nil {
		wasCanceled := ctx.Err() != nil
		cancel()
		p.mu.Lock()
		currentSession := p.sessionGen == sessionGen
		if currentSession {
			rec.Stop()
			activeRecorder.CompareAndSwap(rec, nil)
			p.stopping, p.recorder, p.recording = false, nil, false
			p.stopAt = time.Time{}
			p.asrCancel = nil
			if !wasCanceled {
				p.publishErrorLocked("ASR 连接失败: "+asrConnectErrorDetail(err), sessionID)
			} else {
				p.publishStatusLocked()
			}
		}
		p.mu.Unlock()
		if currentSession && !wasCanceled {
			pout("❌ ASR 连接失败: %v", err)
		}
		return
	}
	pout("✅ ASR 已连接")

	p.mu.Lock()
	if p.sessionGen != sessionGen || !p.recording || p.recorder != rec {
		p.mu.Unlock()
		client.Close()
		cancel()
		return
	}
	audioDone := make(chan struct{})
	p.asrClient = client
	p.audioDone = audioDone
	p.publishStatusLocked()
	p.mu.Unlock()
	// Dictation never reaches stream subscribers. It is Just Talk typing for
	// you; the subscription carries the transcription service's external
	// audio, and mixing your own answers into it would be a surprise to every
	// subscriber.

	go func() {
		defer close(audioDone)
		p.streamAudio(ctx, rec, client)
	}()
	go func() {
		for result := range client.Results() {
			if result.Error != nil {
				pout("❌ ASR 错误: %v", result.Error)
				continue
			}
			if result.Text != "" {
				setOverlayPartial(sessionID, result.Text)
			}
			if result.IsFinal && result.Text != "" {
				pout("\n🎤 最终: %s", result.Text)
			} else if result.Text != "" {
				pout("\r🎤 %s", result.Text)
			}
		}
	}()
}

func (p *VoicePlugin) restartRecording() {
	p.mu.Lock()
	session := p.detachRecordingLocked()
	p.trackFinishLocked(session)
	p.publishStatusLocked()
	p.mu.Unlock()
	if session != nil {
		go p.finishRecordingSession(session)
	}
	p.startRecording()
}

func (p *VoicePlugin) cancelRecording() {
	p.mu.Lock()
	session := p.detachRecordingLocked()
	hadError := p.lastError != "" && time.Now().Before(p.errorUntil)
	hadPending := p.pendingDone > 0
	if hadPending {
		if p.canceledSessions == nil {
			p.canceledSessions = make(map[uint64]struct{})
		}
		for id := range p.finishingSessions {
			p.canceledSessions[id] = struct{}{}
		}
		p.pendingDone = 0
	}
	p.clearErrorLocked()
	p.publishStatusLocked()
	p.mu.Unlock()

	if session != nil {
		if session.asrCancel != nil {
			session.asrCancel()
		}
		if session.recorder != nil {
			_, _ = session.recorder.Stop()
		}
		if session.asrClient != nil {
			_ = session.asrClient.Close()
		}
		pout("🎤 已取消本次录音")
	} else if hadPending {
		pout("🎤 已取消等待识别结果")
	} else if hadError {
		pout("⚠️  已关闭错误状态")
	}
}

func (p *VoicePlugin) retryLastError() {
	p.mu.Lock()
	if p.recording || p.stopping || p.pendingDone > 0 || p.lastError == "" || time.Now().After(p.errorUntil) {
		p.mu.Unlock()
		return
	}
	p.clearErrorLocked()
	p.publishStatusLocked()
	p.mu.Unlock()
	p.startRecording()
}

func (p *VoicePlugin) stopRecordingAsync() {
	p.mu.Lock()
	session := p.detachRecordingLocked()
	p.trackFinishLocked(session)
	p.publishStatusLocked()
	p.mu.Unlock()
	if session != nil {
		go p.finishRecordingSession(session)
	}
}

func (p *VoicePlugin) detachRecordingLocked() *recordingSession {
	if p.stopTimer != nil {
		p.stopTimer.Stop()
		p.stopTimer = nil
	}
	p.stopAt = time.Time{}
	if p.recorder == nil && p.asrClient == nil && p.asrCancel == nil {
		p.recording, p.stopping, p.userStopped = false, false, false
		return nil
	}
	session := &recordingSession{
		sessionID:   p.sessionID,
		recorder:    p.recorder,
		asrClient:   p.asrClient,
		asrCancel:   p.asrCancel,
		audioDone:   p.audioDone,
		textOutput:  p.textOutput,
		userStopped: p.userStopped,
		startedAt:   p.startedAt,
	}
	p.sessionGen++
	activeRecorder.CompareAndSwap(p.recorder, nil)
	p.recorder, p.asrClient, p.asrCancel, p.audioDone = nil, nil, nil, nil
	p.startedAt = time.Time{}
	p.recording, p.stopping, p.userStopped = false, false, false
	p.holdReleased = false
	return session
}

func (p *VoicePlugin) trackFinishLocked(session *recordingSession) {
	if session == nil {
		return
	}
	p.pendingDone++
	if p.finishingSessions == nil {
		p.finishingSessions = make(map[uint64]struct{})
	}
	p.finishingSessions[session.sessionID] = struct{}{}
}

func (p *VoicePlugin) finishRecordingSession(session *recordingSession) {
	if session == nil {
		return
	}
	defer p.recordingSessionFinished(session.sessionID)
	pout("🎤 停止录音")
	if session.asrClient == nil && session.asrCancel != nil {
		session.asrCancel()
	}
	var remaining []byte
	if session.recorder != nil {
		p.logger.Debug("finish session: stopping recorder")
		remaining, _ = session.recorder.Stop()
		p.logger.Debug("finish session: recorder stopped", "remaining_bytes", len(remaining))
	}

	if session.asrClient != nil {
		audioDrained := true
		if session.audioDone != nil {
			p.logger.Debug("finish session: waiting audio stream drain")
			select {
			case <-session.audioDone:
				p.logger.Debug("finish session: audio stream drained")
			case <-time.After(audioDrainTimeout):
				audioDrained = false
				p.logger.Warn("finish session: audio stream drain timed out")
				if !p.sessionCanceled(session.sessionID) {
					p.publishError("停止录音超时: 等待音频发送结束超过 4s", session.sessionID)
				}
			}
		}

		finalSent := false
		if audioDrained {
			p.logger.Debug("finish session: sending final audio", "bytes", len(remaining))
			sendCtx, sendCancel := context.WithTimeout(context.Background(), audioSendTimeout)
			err := session.asrClient.SendAudio(sendCtx, remaining, true)
			sendCancel()
			if err != nil {
				p.logger.Warn("send final audio failed", "error", err)
				if !p.sessionCanceled(session.sessionID) {
					p.publishError("结束识别失败: "+shortError(err), session.sessionID)
				}
			} else {
				finalSent = true
			}
		}
		if finalSent {
			p.logger.Debug("finish session: waiting ASR final")
			select {
			case <-session.asrClient.Final():
				p.logger.Debug("finish session: ASR final received")
			case <-session.asrClient.Done():
				p.logger.Debug("finish session: ASR done")
			case <-time.After(15 * time.Second):
				if !p.sessionCanceled(session.sessionID) {
					pout("⚠️  识别超时")
					p.publishError("识别超时: 等待 ASR final 超过 15s", session.sessionID)
				}
			}
		}
		if text := session.asrClient.LastText(); text != "" && session.userStopped && p.claimSessionOutput(session.sessionID) {
			audioDuration := time.Duration(0)
			if !session.startedAt.IsZero() {
				audioDuration = time.Since(session.startedAt)
			}
			recordTUIStats(text, audioDuration)
			p.dispatchTextOutput(text, session.textOutput)
		}
		p.logger.Debug("finish session: closing ASR client")
		closeDone := make(chan error, 1)
		go func() { closeDone <- session.asrClient.Close() }()
		select {
		case err := <-closeDone:
			if err != nil {
				p.logger.Debug("close ASR client failed", "error", err)
			}
		case <-time.After(2 * time.Second):
			p.logger.Warn("close ASR client timed out")
		}
	}
	if session.asrCancel != nil {
		session.asrCancel()
	}
	p.logger.Debug("finish session: done")
}

func (p *VoicePlugin) dispatchTextOutput(text string, output string) {
	if output == config.OutputNone {
		// 只转写：结果已经进了日志、浮层和识别结果订阅，不碰输入框和剪贴板。
		p.logger.Debug("text output: disabled", "text_len", len(text))
		pout("\n\U0001f4dd 已转写（不输出）")
		return
	}
	beginOverlayOutput()
	go func() {
		detail := ""
		defer func() { finishOverlayOutput(text, detail) }()
		if output == config.OutputPaste {
			if err := autotype.Paste(text, p.logger); err != nil {
				pout("❌ 上屏失败: %v", err)
			} else {
				pout("📋 已复制到剪贴板")
				pout("✅ 已上屏")
				detail = "pasted"
			}
			return
		}

		p.logger.Debug("text output: writing clipboard", "text_len", len(text))
		if err := writeClipboard(text); err != nil {
			pout("❌ 复制到剪贴板失败: %v", err)
		} else {
			pout("📋 已复制到剪贴板")
			detail = "copied"
		}
	}()
}

func (p *VoicePlugin) recordingSessionFinished(sessionID uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.finishingSessions, sessionID)
	delete(p.canceledSessions, sessionID)
	if p.pendingDone > 0 {
		p.pendingDone--
	}
	p.publishStatusLocked()
}

func (p *VoicePlugin) sessionCanceled(sessionID uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.canceledSessions[sessionID]
	return ok
}

func (p *VoicePlugin) claimSessionOutput(sessionID uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, canceled := p.canceledSessions[sessionID]; canceled {
		return false
	}
	if p.outputSessions == nil {
		p.outputSessions = make(map[uint64]struct{})
	}
	if _, exists := p.outputSessions[sessionID]; exists {
		return false
	}
	p.outputSessions[sessionID] = struct{}{}
	return true
}

func (p *VoicePlugin) publishStatusLocked() {
	state, detail := "idle", "等待热键"
	stopAt := time.Time{}
	recording, stopping := p.recording, false

	switch {
	case p.recording && p.stopping:
		state, detail = "stopping_delayed", "等待停止延迟"
		stopping = true
		stopAt = p.stopAt
	case p.recording && p.asrClient == nil && p.asrCancel != nil:
		state, detail = "connecting", "录音中，正在连接 ASR"
	case p.recording:
		state, detail = "recording", "录音中"
	case p.pendingDone > 0:
		state, detail = "stopping", "正在停止并等待识别结果"
		stopping = true
	case p.lastError != "" && time.Now().Before(p.errorUntil):
		state, detail = "error", p.lastError
	}
	if state == "idle" && p.contState != "" {
		// Nothing is being dictated, so the status line shows what the
		// transcription service is doing instead of a hotkey that just waits.
		state, detail = p.contState, p.contDetail
		recording = state == "recording"
	}

	p.publishStatusSnapshotLocked(state, detail, recording, stopping, stopAt, p.sessionID)
	errorActive := state == "error" && time.Now().Before(p.errorUntil)
	wasFlowActive := p.flowActive
	p.flowActive = state != "idle"
	if state == "idle" && wasFlowActive {
		p.cleanupTransientHotkeysLocked()
		return
	}
	p.syncTransientHotkeysLocked(recording || stopping || p.pendingDone > 0 || errorActive, errorActive)
}

func (p *VoicePlugin) publishErrorLocked(detail string, sessionID uint64) {
	p.lastError = detail
	p.errorUntil = time.Now().Add(errorHoldDuration)
	if p.errorTimer != nil {
		p.errorTimer.Stop()
	}
	p.errorTimer = time.AfterFunc(errorHoldDuration, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.lastError != "" && time.Now().After(p.errorUntil) {
			p.clearErrorLocked()
			p.publishStatusLocked()
		}
	})
	p.publishStatusSnapshotLocked("error", detail, false, false, time.Time{}, sessionID)
	p.syncTransientHotkeysLocked(true, true)
}

func (p *VoicePlugin) publishError(detail string, sessionID uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publishErrorLocked(detail, sessionID)
}

func (p *VoicePlugin) publishStatusSnapshotLocked(state, detail string, recording, stopping bool, stopAt time.Time, sessionID uint64) {
	pendingDone := p.pendingDone
	setTUIStatus(func(s *TUIVoiceStatus) {
		s.State, s.Detail = state, detail
		if state == "idle" {
			idleDetail = detail
		}
		s.Recording, s.Stopping = recording, stopping
		s.StopAt = stopAt
		s.ErrorUntil = p.errorUntil
		s.SessionID = sessionID
		s.PendingFinishes = pendingDone
	})
	notifyOverlay()
}

func (p *VoicePlugin) clearErrorLocked() {
	if p.errorTimer != nil {
		p.errorTimer.Stop()
		p.errorTimer = nil
	}
	p.errorUntil = time.Time{}
	p.lastError = ""
}

func (p *VoicePlugin) cleanupTransientHotkeysLocked() {
	p.syncTransientHotkeysLocked(false, false)
}

func validateVoiceHotkey(combo hotkey.Combo) error {
	if combo.Mods&hotkey.ModFn != 0 && combo.Key.IsFunctionKey() {
		return fmt.Errorf("Fn 不能与功能键组合为语音热键 %s；请改用字母或其他非功能键", combo)
	}
	if combo.Key.IsTextKey() && combo.Mods&hotkey.ModFn == 0 {
		return fmt.Errorf("语音热键不支持普通字符键 %s；请使用适合作为全局快捷键的组合，如 Alt+Super、Alt+F8、F9、Ctrl+Alt+Tab", combo)
	}
	return nil
}

func (p *VoicePlugin) syncTransientHotkeysLocked(needCancel, needRetry bool) {
	if needCancel && !p.cancelHotkeyRegistered {
		if err := p.env.RegisterHotkey(cancelRecordingCombo, p.onCancelHotkey); err != nil {
			p.logger.Warn("register cancel hotkey failed", "combo", cancelRecordingCombo, "error", err)
		} else {
			p.cancelHotkeyRegistered = true
		}
	} else if !needCancel && p.cancelHotkeyRegistered {
		if err := p.env.UnregisterHotkey(cancelRecordingCombo); err != nil {
			p.logger.Warn("unregister cancel hotkey failed", "combo", cancelRecordingCombo, "error", err)
		}
		p.cancelHotkeyRegistered = false
	}

	if needRetry && !p.retryHotkeyRegistered {
		if err := p.env.RegisterHotkey(retryErrorCombo, p.onRetryHotkey); err != nil {
			p.logger.Warn("register retry hotkey failed", "combo", retryErrorCombo, "error", err)
		} else {
			p.retryHotkeyRegistered = true
		}
	} else if !needRetry && p.retryHotkeyRegistered {
		if err := p.env.UnregisterHotkey(retryErrorCombo); err != nil {
			p.logger.Warn("unregister retry hotkey failed", "combo", retryErrorCombo, "error", err)
		}
		p.retryHotkeyRegistered = false
	}
}

func shortError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "未知错误"
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len([]rune(msg)) <= 120 {
		return msg
	}
	return string([]rune(msg)[:120]) + "..."
}

func asrConnectErrorDetail(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "HTTP 403") {
		return "认证失败，请检查 App Key 或 Access Key。"
	}
	return shortError(err)
}

// streamAudio forwards recorder PCM to the active speech session.
func (p *VoicePlugin) streamAudio(ctx context.Context, rec *Recorder, client speech.StreamSession) {
	buf := make([]byte, 6400)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := rec.Read(buf)
		if n > 0 {
			sendCtx, sendCancel := context.WithTimeout(ctx, audioSendTimeout)
			sendErr := client.SendAudio(sendCtx, buf[:n], false)
			sendCancel()
			if sendErr != nil {
				if ctx.Err() == nil {
					p.logger.Error("send audio error", "error", sendErr)
				}
				return
			}
		}
		if err == io.EOF || (err != nil && ctx.Err() != nil) {
			return
		}
		if err != nil {
			p.logger.Error("read audio error", "error", err)
			return
		}
	}
}

func writeClipboard(text string) error {
	cb, err := clipboard.New()
	if err != nil {
		return err
	}
	return cb.Set(text)
}
