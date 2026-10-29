package voice

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/internal/asrcfg"
	"github.com/c/just-talk-go/internal/speech"
)

// Continuous transcription runs alongside dictation, not instead of it: the
// speech gate opens a session when someone starts talking into the external
// source and closes it when they stop, while the hotkeys keep dictating into
// whatever the user is typing in.
//
// The audio here is an external source — a meeting, system output, a room —
// not the user talking to their own machine. Nobody asked for this text, so
// there is no key to press and no destination to paste into: results go to
// the log, the overlay and stream subscribers. That is what this path is for,
// not a limitation of it, which is also why it has its own recording device.
const (
	continuousBackoffMin = 500 * time.Millisecond
	continuousBackoffMax = 5 * time.Second
	// A session that ran this long resets the reconnect backoff.
	continuousStableSession = 10 * time.Second
	continuousStopTimeout   = 5 * time.Second
	continuousCloseTimeout  = 2 * time.Second
	// How long the engine gets to return its last result after the final
	// audio packet of an utterance.
	continuousFlushTimeout = 2 * time.Second
)

var (
	errContinuousNeedsStreaming = errors.New("持续转写需要流式识别引擎")
	errSessionEndedByServer     = errors.New("服务端结束会话")
	errSessionClosed            = errors.New("ASR 连接已关闭")
	errAudioSendStopped         = errors.New("音频发送中断")
)

// syncContinuous starts, restarts or stops the continuous loop to match the
// current config and run state. A replaced loop releases its recorder before
// the new one starts.
func (p *VoicePlugin) syncContinuous() {
	p.mu.Lock()
	ctx := p.runCtx
	vc := p.cfg.Voice
	want := ctx != nil && vc.Enabled && p.contEnabled
	// The subscription has no switch of its own and does not follow the
	// service either: the switch decides whether a device is captured, and
	// subscribers stay connected across it, told by service frames whether
	// anything is being heard. It listens for as long as Just Talk runs.
	streamAddr := ""
	if ctx != nil {
		streamAddr = vc.StreamAddr
		if strings.TrimSpace(streamAddr) == "" {
			streamAddr = config.DefaultStreamAddr
		}
	}
	p.syncStreamServerLocked(streamAddr)
	if want && p.contCancel != nil && reflect.DeepEqual(p.contCfg, continuousKey(vc)) {
		p.mu.Unlock()
		return
	}
	oldCancel, oldDone := p.contCancel, p.contDone
	p.contCancel, p.contDone = nil, nil
	var start func()
	if want {
		loopCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		p.contCancel, p.contDone, p.contCfg = cancel, done, continuousKey(vc)
		start = func() {
			go func() {
				defer close(done)
				p.runContinuous(loopCtx, vc)
			}()
		}
	}
	p.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
		select {
		case <-oldDone:
		case <-time.After(continuousStopTimeout):
			p.logger.Warn("continuous: previous loop did not stop in time")
		}
	}
	if start != nil {
		start()
	}
}

// runContinuous keeps one recorder open across ASR reconnects until ctx is
// cancelled; the recorder is reopened only if capture itself fails.
func (p *VoicePlugin) runContinuous(ctx context.Context, vc config.VoiceConfig) {
	p.logger.Info("continuous: started", "device", vc.Continuous.Device)
	pout("\U0001f3a7 持续转写已启动")
	backoff := continuousBackoffMin
	for ctx.Err() == nil {
		var rec *Recorder
		if vc.Continuous.Device != "" {
			rec = NewRecorderWithDevice(p.logger, vc.Continuous.Device, vc.Gain)
		} else {
			rec = NewRecorder(p.logger, vc.Gain)
		}
		if err := rec.Start(); err != nil {
			p.logger.Error("continuous: recorder start failed", "error", err)
			pout("❌ 录音启动失败: %v", err)
			if !sleepContext(ctx, backoff) {
				break
			}
			backoff = nextContinuousBackoff(backoff)
			continue
		}
		backoff = continuousBackoffMin
		pout("\U0001f3a4 开始持续录音 (后端: %s)", rec.Backend())
		continuousRecorder.Store(rec)
		p.publishServiceState()

		recCtx, recCancel := context.WithCancel(ctx)
		gate := newSpeechGate(vc.Continuous.SilenceGate, p.logger)
		pumpDone := make(chan struct{})
		go func() {
			defer close(pumpDone)
			defer recCancel()
			p.pumpAudio(rec, gate)
		}()
		p.runContinuousSessions(recCtx, vc, gate)
		recCancel()
		_, _ = rec.Stop()
		<-pumpDone
		continuousRecorder.CompareAndSwap(rec, nil)
		p.publishServiceState()
		if ctx.Err() == nil {
			pout("⚠️  录音中断，正在重新打开")
			if !sleepContext(ctx, backoff) {
				break
			}
			backoff = nextContinuousBackoff(backoff)
		}
	}
	pout("🎧 持续转写已停止")
	p.logger.Info("continuous: stopped")
}

// runContinuousSessions runs one ASR session per utterance over one recorder
// until ctx ends (shutdown or capture failure). Backoff is for connecting:
// an utterance ending is the normal outcome, not a failure to recover from.
func (p *VoicePlugin) runContinuousSessions(ctx context.Context, vc config.VoiceConfig, gate *speechGate) {
	backoff := continuousBackoffMin
	for ctx.Err() == nil {
		if !gate.waitOpen(ctx) {
			return
		}
		startedAt := time.Now()
		connected, err := p.runContinuousSession(ctx, vc, gate)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errContinuousNeedsStreaming) {
			p.logger.Error("continuous: engine is not streaming", "engine", vc.Engine)
			pout("❌ %v", err)
			<-ctx.Done()
			return
		}
		if connected && gate.enabled {
			backoff = continuousBackoffMin
			p.logger.Debug("continuous: utterance ended", "reason", err)
			continue
		}
		if connected && time.Since(startedAt) >= continuousStableSession {
			backoff = continuousBackoffMin
		}
		p.logger.Warn("continuous: session ended, reconnecting", "error", err, "connected", connected, "backoff", backoff)
		if connected {
			pout("⚠️  ASR 会话结束 (%v)，%s 后重连", err, backoff)
		} else {
			pout("❌ ASR 连接失败: %v，%s 后重试", err, backoff)
		}
		if !sleepContext(ctx, backoff) {
			return
		}
		backoff = nextContinuousBackoff(backoff)
	}
}

// runContinuousSession connects one ASR session, streams one utterance into it
// and consumes results until the session ends. It reports whether the session
// connected and why it ended.
func (p *VoicePlugin) runContinuousSession(ctx context.Context, vc config.VoiceConfig, gate *speechGate) (bool, error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p.mu.Lock()
	p.contSessionID++
	sessionID := p.contSessionID
	p.mu.Unlock()

	eng, err := asrcfg.Build(vc)
	if err != nil {
		return false, err
	}
	streaming, ok := eng.(speech.StreamingEngine)
	if !ok {
		return false, errContinuousNeedsStreaming
	}
	client, err := streaming.NewStream(sctx, speech.StreamParams{Lang: vc.Language, Hotwords: p.hotwords.load(p.logger, vc)})
	if err != nil {
		return false, err
	}
	if err := client.Connect(sctx); err != nil {
		return false, err
	}
	defer p.closeStreamSession(client)
	pout("✅ ASR 已连接 (持续转写)")
	p.publishSessionStart(sessionID)
	// Hotwords only reach the engine when a session opens. With the gate on
	// that is once per utterance, so the file is already reread every time;
	// only a long-lived session needs watching.
	if !gate.enabled {
		go p.hotwords.watch(sctx, vc, func() {
			p.logger.Info("hotwords file changed, restarting session")
			pout("\U0001f501 热词已更新，重连以生效")
			cancel()
		})
	}

	sendDone := make(chan struct{})
	utteranceEnded := false
	go func() {
		defer close(sendDone)
		defer cancel()
		utteranceEnded = p.streamGated(sctx, client, gate)
		if !utteranceEnded {
			return
		}
		// The final packet is out: give the engine a moment to answer with
		// its last result before the session is torn down.
		select {
		case <-sctx.Done():
		case <-time.After(continuousFlushTimeout):
		}
	}()
	err = p.consumeContinuousResults(sctx, sessionID, client)
	cancel()
	<-sendDone
	reason := sessionEndError
	switch {
	case errors.Is(err, errSessionEndedByServer):
		reason = sessionEndLast
	case utteranceEnded, errors.Is(err, errSessionClosed), ctx.Err() != nil:
		reason = sessionEndClosed
	}
	p.publishSessionEnd(sessionID, reason)
	return true, err
}

// consumeContinuousResults publishes and logs results until the session ends.
// It never dispatches text output.
func (p *VoicePlugin) consumeContinuousResults(ctx context.Context, sessionID uint64, client speech.StreamSession) error {
	results := client.Results()
	printed := 0
	for {
		select {
		case <-ctx.Done():
			return errAudioSendStopped
		case result, ok := <-results:
			if !ok {
				return errSessionClosed
			}
			if result.Error != nil {
				return result.Error
			}
			p.publishASRResult(sessionID, client, result)
			printed = printUtterances(result.Utterances, printed)
			if result.Last {
				return errSessionEndedByServer
			}
		}
	}
}

// printUtterances logs each newly definite utterance once and the trailing
// partial one in place; it returns how many utterances are now logged.
func printUtterances(utterances []speech.Utterance, printed int) int {
	printed = min(printed, len(utterances))
	for i := printed; i < len(utterances); i++ {
		u := utterances[i]
		if !u.Definite {
			if u.Text != "" {
				pout("\r🎤 %s", u.Text)
			}
			break
		}
		pout("\n🎤 %s", u.Text)
		printed = i + 1
	}
	return printed
}

// streamGated sends one utterance to client and reports whether it sent the
// final packet, which asks the engine to flush its last result. False means
// capture stopped, the send failed or the session died first.
func (p *VoicePlugin) streamGated(ctx context.Context, client speech.StreamSession, gate *speechGate) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-client.Done():
			return false
		case c := <-gate.chunks:
			sendCtx, sendCancel := context.WithTimeout(ctx, audioSendTimeout)
			err := client.SendAudio(sendCtx, c.data, c.last)
			sendCancel()
			gate.release(c.data)
			if err != nil {
				if ctx.Err() == nil {
					p.logger.Warn("continuous: send audio failed", "error", err)
				}
				return false
			}
			if c.last {
				return true
			}
		}
	}
}

// pumpAudio drains the recorder into the gate until capture stops, so the
// capture pipe never fills while the gate is shut or ASR reconnects.
func (p *VoicePlugin) pumpAudio(rec *Recorder, gate *speechGate) {
	for {
		chunk := gate.acquire()
		n, err := rec.Read(chunk)
		if n > 0 {
			gate.push(chunk[:n])
		} else {
			gate.release(chunk)
		}
		if err != nil {
			if err != io.EOF {
				p.logger.Error("continuous: read audio failed", "error", err)
			}
			return
		}
	}
}

func (p *VoicePlugin) closeStreamSession(client speech.StreamSession) {
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			p.logger.Debug("continuous: close ASR client failed", "error", err)
		}
	case <-time.After(continuousCloseTimeout):
		p.logger.Warn("continuous: close ASR client timed out")
	}
}

// serviceState describes the transcription service for subscribers: while
// nobody is talking the stream is silent, so this is the only way to tell
// "listening" from "not running".
func (p *VoicePlugin) serviceState() streamServiceEvent {
	p.mu.Lock()
	var vc config.VoiceConfig
	if p.cfg != nil {
		vc = p.cfg.Voice
	}
	p.mu.Unlock()
	ev := streamServiceEvent{
		Type:   "service",
		State:  serviceIdle,
		Device: vc.Continuous.Device,
		Engine: asrcfg.EngineType(vc),
		Gate:   vc.Continuous.SilenceGate,
	}
	if continuousRecorder.Load() != nil {
		ev.State = serviceListening
	}
	return ev
}

func (p *VoicePlugin) publishServiceState() { p.streamBroadcast(p.serviceState()) }

// continuousKey drops the settings that belong to dictation, so editing a
// hotkey or the paste destination does not interrupt a transcription that is
// running alongside it.
func continuousKey(vc config.VoiceConfig) config.VoiceConfig {
	vc.Mode, vc.PushToTalk, vc.TextOutput, vc.Device = "", "", "", ""
	vc.StopDelayMs = 0
	// Switching the service on or off is handled by want; rebinding its
	// hotkey must not interrupt a transcription either.
	vc.Continuous.Enabled, vc.Continuous.Hotkey = false, ""
	// The subscription server is synced on its own; rebinding its address
	// must not interrupt a transcription.
	vc.StreamAddr = ""
	return vc
}

func nextContinuousBackoff(d time.Duration) time.Duration {
	return min(d*2, continuousBackoffMax)
}

// sleepContext waits d and reports false if ctx ended first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
