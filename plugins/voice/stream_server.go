package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/c/just-talk-go/internal/speech"
)

const (
	// streamPath is where subscribers connect: ws://<stream_addr>/asr.
	streamPath = "/asr"
	// A subscriber this many frames behind is disconnected so it can never
	// stall the ASR loop or other subscribers.
	streamSubscriberBuffer = 256
	streamWriteTimeout     = 2 * time.Second
)

// Session end reasons sent in {"type":"session","state":"end"} frames.
const (
	sessionEndLast   = "last"
	sessionEndError  = "error"
	sessionEndClosed = "closed"
)

type streamSessionEvent struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Session string `json:"session"`
	Reason  string `json:"reason,omitempty"`
}

type streamASREvent struct {
	Type       string             `json:"type"`
	Session    string             `json:"session"`
	Text       string             `json:"text"`
	Utterances []speech.Utterance `json:"utterances"`
	Final      bool               `json:"final"`
}

func newSessionStartEvent(session string) streamSessionEvent {
	return streamSessionEvent{Type: "session", State: "start", Session: session}
}

func newSessionEndEvent(session, reason string) streamSessionEvent {
	return streamSessionEvent{Type: "session", State: "end", Session: session, Reason: reason}
}

func newASREvent(session, text string, utterances []speech.Utterance, final bool) streamASREvent {
	if utterances == nil {
		// Subscribers always get an array, never null.
		utterances = []speech.Utterance{}
	}
	return streamASREvent{Type: "asr", Session: session, Text: text, Utterances: utterances, Final: final}
}

// checkLoopbackAddr rejects listen addresses that are not loopback-only:
// transcripts must never be reachable from the network.
func checkLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("stream_addr %q: %w", addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("stream_addr %q 不是回环地址；只允许 127.0.0.1、::1 或 localhost", addr)
}

// streamServer serves ASR events to any number of local WebSocket
// subscribers. It outlives sessions; broadcast never blocks.
//
// Subscribers are invisible to each other, and that must stay true: a
// connection only ever carries frames from broadcast (ASR results, identical
// for everyone), never anything addressed to one subscriber, nor how many
// others are connected. Per-connection state stays on streamSubscriber. If
// per-subscriber filtering or commands are ever added, they belong on that
// struct and must be applied on that connection's write path alone.
type streamServer struct {
	logger *slog.Logger
	srv    *http.Server
	ln     net.Listener
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	subs   map[*streamSubscriber]struct{}
	closed bool
}

type streamSubscriber struct {
	frames chan []byte
	// kicked closes when the subscriber fell too far behind.
	kicked chan struct{}
}

func newStreamServer(addr string, logger *slog.Logger) (*streamServer, error) {
	if err := checkLoopbackAddr(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &streamServer{
		logger: logger,
		ln:     ln,
		ctx:    ctx,
		cancel: cancel,
		subs:   make(map[*streamSubscriber]struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(streamPath, s.handle)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("stream server stopped", "error", err)
		}
	}()
	return s, nil
}

// Addr returns the bound address (useful when addr used port 0).
func (s *streamServer) Addr() string { return s.ln.Addr().String() }

// Close stops listening and disconnects every subscriber.
func (s *streamServer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	_ = s.srv.Close()
}

func (s *streamServer) handle(w http.ResponseWriter, r *http.Request) {
	// Default options reject cross-origin browser pages; local tools send no
	// Origin header and are accepted.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Debug("stream: accept failed", "error", err)
		return
	}
	defer conn.CloseNow()
	// Subscribers only listen; CloseRead notices the peer going away.
	ctx := conn.CloseRead(s.ctx)
	sub := &streamSubscriber{
		frames: make(chan []byte, streamSubscriberBuffer),
		kicked: make(chan struct{}),
	}
	if !s.add(sub) {
		return
	}
	defer s.remove(sub)
	s.logger.Info("stream: subscriber connected", "remote", r.RemoteAddr)
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusGoingAway, "")
			s.logger.Info("stream: subscriber disconnected", "remote", r.RemoteAddr)
			return
		case <-sub.kicked:
			_ = conn.Close(websocket.StatusPolicyViolation, "subscriber too slow")
			s.logger.Warn("stream: slow subscriber disconnected", "remote", r.RemoteAddr)
			return
		case frame := <-sub.frames:
			wctx, cancel := context.WithTimeout(ctx, streamWriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				s.logger.Debug("stream: write failed", "remote", r.RemoteAddr, "error", err)
				return
			}
		}
	}
}

func (s *streamServer) add(sub *streamSubscriber) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.subs[sub] = struct{}{}
	return true
}

func (s *streamServer) remove(sub *streamSubscriber) {
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

func (s *streamServer) subscriberCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// broadcast encodes v once and queues it for every subscriber without
// blocking; a subscriber whose buffer is full is disconnected.
func (s *streamServer) broadcast(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) == 0 {
		return
	}
	frame, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("stream: encode event failed", "error", err)
		return
	}
	for sub := range s.subs {
		select {
		case sub.frames <- frame:
		default:
			delete(s.subs, sub)
			close(sub.kicked)
			s.logger.Debug("stream: subscriber buffer full, disconnecting")
		}
	}
}

func (p *VoicePlugin) streamSessionKey(sessionID uint64) string {
	return fmt.Sprintf("%s-%d", p.streamPrefix, sessionID)
}

func (p *VoicePlugin) streamBroadcast(v any) {
	p.mu.Lock()
	hub := p.stream
	p.mu.Unlock()
	if hub != nil {
		hub.broadcast(v)
	}
}

func (p *VoicePlugin) publishSessionStart(sessionID uint64) {
	p.streamBroadcast(newSessionStartEvent(p.streamSessionKey(sessionID)))
}

func (p *VoicePlugin) publishSessionEnd(sessionID uint64, reason string) {
	p.streamBroadcast(newSessionEndEvent(p.streamSessionKey(sessionID), reason))
}

// publishASRResult sends one ASR result frame to stream subscribers.
func (p *VoicePlugin) publishASRResult(sessionID uint64, client speech.StreamSession, result speech.StreamResult) {
	p.mu.Lock()
	hub := p.stream
	p.mu.Unlock()
	if hub == nil {
		return
	}
	text := result.Text
	if text == "" {
		text = client.LastText()
	}
	hub.broadcast(newASREvent(p.streamSessionKey(sessionID), text, result.Utterances, result.Last))
}

// syncStreamServerLocked (re)starts the subscription server when the address
// changes; an empty address stops it. A bad address is logged and leaves the
// server off until the next config reload retries it.
func (p *VoicePlugin) syncStreamServerLocked(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == p.streamAddr && (p.stream != nil || addr == "") {
		return
	}
	if p.stream != nil {
		p.stream.Close()
		p.stream = nil
		publishStreamStatus(nil)
		p.logger.Info("stream server stopped", "addr", p.streamAddr)
	}
	p.streamAddr = addr
	if addr == "" {
		return
	}
	srv, err := newStreamServer(addr, p.logger)
	if err != nil {
		p.logger.Error("stream server not started", "addr", addr, "error", err)
		pout("❌ 识别结果订阅服务未启动: %v", err)
		return
	}
	p.stream = srv
	p.logger.Info("stream server listening", "url", "ws://"+srv.Addr()+streamPath)
	pout("\U0001f4e1 识别结果订阅: ws://%s%s", srv.Addr(), streamPath)
	publishStreamStatus(srv)
}

// liveStream is the running subscription server, for the settings UIs. The
// engine loads one voice plugin, so a package-level handle is enough.
var (
	liveStreamMu sync.Mutex
	liveStream   *streamServer
)

func publishStreamStatus(srv *streamServer) {
	liveStreamMu.Lock()
	liveStream = srv
	liveStreamMu.Unlock()
}

// StreamStatus reports the subscription server: the URL subscribers connect
// to (empty when it is off) and how many are connected right now.
func StreamStatus() (url string, subscribers int) {
	liveStreamMu.Lock()
	srv := liveStream
	liveStreamMu.Unlock()
	if srv == nil {
		return "", 0
	}
	return "ws://" + srv.Addr() + streamPath, srv.subscriberCount()
}

// CheckStreamAddr reports whether addr can be used for [voice] stream_addr.
// The UIs call it so a bad address fails on save instead of silently
// leaving the server off.
func CheckStreamAddr(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return checkLoopbackAddr(strings.TrimSpace(addr))
}
