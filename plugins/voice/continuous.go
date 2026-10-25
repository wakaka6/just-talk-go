package voice

import (
	"context"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/internal/asrcfg"
	"github.com/c/just-talk-go/internal/speech"
)

// modeContinuous transcribes without hotkeys: sessions start with the plugin
// and reconnect whenever they end. Results only go to the log, overlay and
// stream subscribers; nothing is ever pasted, typed or copied.
const modeContinuous = "continuous"

const (
	continuousBackoffMin = 500 * time.Millisecond
	continuousBackoffMax = 5 * time.Second
	// A session that ran this long resets the reconnect backoff.
	continuousStableSession = 10 * time.Second
	// Audio kept while ASR reconnects, about 5s: the macOS capture pipe
	// delivers 4096-byte (128ms) chunks.
	continuousBacklogChunks = 40
	continuousChunkBytes    = 6400
	continuousStopTimeout   = 5 * time.Second
	continuousCloseTimeout  = 2 * time.Second
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
	want := ctx != nil && vc.Enabled && p.mode == modeContinuous
	if want && p.contCancel != nil && reflect.DeepEqual(p.contCfg, vc) {
		p.mu.Unlock()
		return
	}
	oldCancel, oldDone := p.contCancel, p.contDone
	p.contCancel, p.contDone = nil, nil
	var start func()
	if want {
		loopCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		p.contCancel, p.contDone, p.contCfg = cancel, done, vc
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
	p.logger.Info("continuous: started", "device", vc.Device)
	pout("🎧 持续转写已启动")
	backoff := continuousBackoffMin
	for ctx.Err() == nil {
		var rec *Recorder
		if vc.Device != "" {
			rec = NewRecorderWithDevice(p.logger, vc.Device, vc.Gain)
		} else {
			rec = NewRecorder(p.logger, vc.Gain)
		}
		if err := rec.Start(); err != nil {
			p.logger.Error("continuous: recorder start failed", "error", err)
			p.publishContinuousStatus("error", "录音启动失败: "+shortError(err))
			pout("❌ 录音启动失败: %v", err)
			if !sleepContext(ctx, backoff) {
				break
			}
			backoff = nextContinuousBackoff(backoff)
			continue
		}
		backoff = continuousBackoffMin
		pout("🎤 开始持续录音 (后端: %s)", rec.Backend())
		activeRecorder.Store(rec)

		recCtx, recCancel := context.WithCancel(ctx)
		backlog := newAudioBacklog()
		pumpDone := make(chan struct{})
		go func() {
			defer close(pumpDone)
			defer recCancel()
			p.pumpAudio(rec, backlog)
		}()
		p.runContinuousSessions(recCtx, vc, backlog)
		recCancel()
		_, _ = rec.Stop()
		<-pumpDone
		activeRecorder.CompareAndSwap(rec, nil)
		if ctx.Err() == nil {
			pout("⚠️  录音中断，正在重新打开")
			if !sleepContext(ctx, backoff) {
				break
			}
			backoff = nextContinuousBackoff(backoff)
		}
	}
	p.publishContinuousStatus("idle", "持续转写已停止")
	pout("🎧 持续转写已停止")
	p.logger.Info("continuous: stopped")
}

// runContinuousSessions runs back-to-back ASR sessions over one recorder until
// ctx ends (shutdown or capture failure).
func (p *VoicePlugin) runContinuousSessions(ctx context.Context, vc config.VoiceConfig, backlog *audioBacklog) {
	backoff := continuousBackoffMin
	for ctx.Err() == nil {
		startedAt := time.Now()
		connected, err := p.runContinuousSession(ctx, vc, backlog)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errContinuousNeedsStreaming) {
			p.logger.Error("continuous: engine is not streaming", "engine", vc.Engine)
			p.publishContinuousStatus("error", err.Error())
			pout("❌ %v", err)
			<-ctx.Done()
			return
		}
		if connected && time.Since(startedAt) >= continuousStableSession {
			backoff = continuousBackoffMin
		}
		p.logger.Warn("continuous: session ended, reconnecting", "error", err, "connected", connected, "backoff", backoff)
		if connected {
			p.publishContinuousStatus("connecting", "持续转写：会话结束，正在重连")
			pout("⚠️  ASR 会话结束 (%v)，%s 后重连", err, backoff)
		} else {
			p.publishContinuousStatus("error", "ASR 连接失败: "+asrConnectErrorDetail(err))
			pout("❌ ASR 连接失败: %v，%s 后重试", err, backoff)
		}
		if !sleepContext(ctx, backoff) {
			return
		}
		backoff = nextContinuousBackoff(backoff)
	}
}

// runContinuousSession connects one ASR session, streams the backlog into it
// and consumes results until the session ends. It reports whether the session
// connected and why it ended.
func (p *VoicePlugin) runContinuousSession(ctx context.Context, vc config.VoiceConfig, backlog *audioBacklog) (bool, error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p.mu.Lock()
	p.sessionID++
	sessionID := p.sessionID
	p.mu.Unlock()
	resetOverlaySession(sessionID)
	p.publishContinuousStatus("connecting", "持续转写：正在连接 ASR")

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
	p.publishContinuousStatus("recording", "持续转写中")
	p.publishSessionStart(sessionID)
	// Hotwords only reach the engine when a session opens; a continuous
	// session can outlive many edits of the word list.
	go p.hotwords.watch(sctx, vc, func() {
		p.logger.Info("hotwords file changed, restarting session")
		pout("\U0001f501 热词已更新，重连以生效")
		cancel()
	})

	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		defer cancel()
		p.streamBacklog(sctx, client, backlog)
	}()
	err = p.consumeContinuousResults(sctx, sessionID, client)
	cancel()
	<-sendDone
	reason := sessionEndError
	switch {
	case errors.Is(err, errSessionEndedByServer):
		reason = sessionEndLast
	case errors.Is(err, errSessionClosed), ctx.Err() != nil:
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
			if result.Text != "" {
				setOverlayPartial(sessionID, result.Text)
			}
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

// streamBacklog sends captured audio to client until the session ends.
func (p *VoicePlugin) streamBacklog(ctx context.Context, client speech.StreamSession, backlog *audioBacklog) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Done():
			return
		case chunk := <-backlog.chunks:
			sendCtx, sendCancel := context.WithTimeout(ctx, audioSendTimeout)
			err := client.SendAudio(sendCtx, chunk, false)
			sendCancel()
			backlog.release(chunk)
			if err != nil {
				if ctx.Err() == nil {
					p.logger.Warn("continuous: send audio failed", "error", err)
				}
				return
			}
		}
	}
}

// pumpAudio drains the recorder into backlog until capture stops, so the
// capture pipe never fills while ASR reconnects.
func (p *VoicePlugin) pumpAudio(rec *Recorder, backlog *audioBacklog) {
	for {
		chunk := backlog.acquire()
		n, err := rec.Read(chunk)
		if n > 0 {
			backlog.push(chunk[:n])
		} else {
			backlog.release(chunk)
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

func (p *VoicePlugin) publishContinuousStatus(state, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Status only: the hotkey state machine (and its Esc/R hotkeys) stays idle.
	p.publishStatusSnapshotLocked(state, detail, state == "recording", false, time.Time{}, p.sessionID)
}

// audioBacklog is a bounded queue of PCM chunks with recycled buffers. When
// full, the oldest chunk is dropped so the newest audio always gets through.
type audioBacklog struct {
	chunks chan []byte
	free   chan []byte
}

func newAudioBacklog() *audioBacklog {
	return &audioBacklog{
		chunks: make(chan []byte, continuousBacklogChunks),
		free:   make(chan []byte, continuousBacklogChunks+2),
	}
}

func (b *audioBacklog) acquire() []byte {
	select {
	case chunk := <-b.free:
		return chunk[:continuousChunkBytes]
	default:
		return make([]byte, continuousChunkBytes)
	}
}

func (b *audioBacklog) release(chunk []byte) {
	select {
	case b.free <- chunk:
	default:
	}
}

// push is called only by the single pump goroutine.
func (b *audioBacklog) push(chunk []byte) {
	select {
	case b.chunks <- chunk:
		return
	default:
	}
	select {
	case old := <-b.chunks:
		b.release(old)
	default:
	}
	select {
	case b.chunks <- chunk:
	default:
		b.release(chunk)
	}
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
