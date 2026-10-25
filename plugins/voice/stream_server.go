package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/c/just-talk-go/config"
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

// streamServiceEvent tells subscribers what the transcription service is
// doing, which is the only way to tell "listening, nobody is talking" from
// "not running" while the stream is quiet. It carries the service's own
// state — the same for everyone — never anything about other subscribers,
// and never where Just Talk puts dictated text: not a subscriber's business.
type streamServiceEvent struct {
	Type   string `json:"type"`
	State  string `json:"state"`
	Device string `json:"device,omitempty"`
	Engine string `json:"engine,omitempty"`
	Gate   bool   `json:"gate"`
}

// Service states.
const (
	// serviceListening means capture is open: speech will be transcribed.
	serviceListening = "listening"
	// serviceIdle means the service is off or capture is down.
	serviceIdle = "idle"
)

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

// streamListen splits stream_addr into something net.Listen understands.
// "unix:<path>" listens on a Unix socket, which names the instance by path
// and limits who may subscribe to the owner; anything else is host:port on
// loopback, which any local program (and a browser page) can reach.
// Windows gets no unix: addresses: a socket there ignores 0600, so the
// owner-only promise would be false.
func streamListen(addr string) (network, target string, err error) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		if runtime.GOOS == "windows" {
			return "", "", fmt.Errorf("stream_addr %q：Windows 不支持 unix: 地址，请用 127.0.0.1:<端口>", addr)
		}
		path = config.ExpandPath(strings.TrimSpace(path))
		if path == "" {
			return "", "", fmt.Errorf("stream_addr %q 缺少 socket 路径", addr)
		}
		return "unix", path, nil
	}
	if err := checkLoopbackAddr(addr); err != nil {
		return "", "", err
	}
	return "tcp", addr, nil
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
// connection carries broadcast frames (ASR results, identical for everyone)
// plus, on connect, one snapshot of the service's own state — the same
// snapshot anyone connecting at that moment would get. Never anything
// addressed to one subscriber, and never how many others are connected.
// Per-connection state stays on streamSubscriber. If per-subscriber
// filtering or commands are ever added, they belong on that struct and must
// be applied on that connection's write path alone.
type streamServer struct {
	logger *slog.Logger
	srv    *http.Server
	ln     net.Listener
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	subs   map[*streamSubscriber]struct{}
	closed bool
	// snapshot describes the service to a subscriber that just connected.
	snapshot func() any
	// display is what a subscriber should connect to, for the UIs and logs.
	display string
}

type streamSubscriber struct {
	frames chan []byte
	// kicked closes when the subscriber fell too far behind.
	kicked chan struct{}
}

func newStreamServer(addr string, logger *slog.Logger) (*streamServer, error) {
	network, target, err := streamListen(addr)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		if err := clearStaleSocket(target); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen(network, target)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	display := "ws://" + ln.Addr().String() + streamPath
	if network == "unix" {
		// Only the owner may subscribe: this is the whole point of the socket.
		if err := os.Chmod(target, 0o600); err != nil {
			ln.Close()
			return nil, fmt.Errorf("chmod %s: %w", target, err)
		}
		display = "unix:" + target
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &streamServer{
		logger:  logger,
		ln:      ln,
		ctx:     ctx,
		cancel:  cancel,
		subs:    make(map[*streamSubscriber]struct{}),
		display: display,
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

// URL is where a subscriber connects, for logs and the settings UIs.
func (s *streamServer) URL() string { return s.display }

// setSnapshot installs what a newly connected subscriber is told first.
func (s *streamServer) setSnapshot(fn func() any) {
	s.mu.Lock()
	s.snapshot = fn
	s.mu.Unlock()
}

// clearStaleSocket removes a socket file left behind by a crash. A socket
// something is still listening on is left alone, so a second instance fails
// loudly instead of stealing the address. Anything that is not a socket is
// never removed: a mistyped stream_addr must not cost the file it names.
func clearStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s 已存在且不是 socket，不会删除它；请给 stream_addr 换一个路径", path)
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("%s 上已经有实例在监听", path)
	}
	return os.Remove(path)
}

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
	// Queued before the subscriber is registered, so it arrives ahead of
	// anything broadcast from here on.
	s.mu.Lock()
	snapshot := s.snapshot
	s.mu.Unlock()
	if snapshot != nil {
		if frame, err := json.Marshal(snapshot()); err == nil {
			sub.frames <- frame
		}
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
// changes; an empty address (the switch is off) stops it. A bad address is
// logged and leaves the server off until the next config reload retries it.
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
	srv.setSnapshot(func() any { return p.serviceState() })
	p.stream = srv
	p.logger.Info("stream server listening", "url", srv.URL())
	pout("\U0001f4e1 识别结果订阅: %s", srv.URL())
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
	return srv.URL(), srv.subscriberCount()
}

// CheckStream reports whether [voice] stream_enabled and stream_addr can be
// saved together. The UIs call it so a bad address fails on save instead of
// silently leaving the server off. An address is checked even with the switch
// off, so turning it on later cannot fail on a typo made now.
func CheckStream(enabled bool, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		if enabled {
			return errors.New("打开识别结果订阅需要填写地址，如 " + config.DefaultStreamAddr)
		}
		return nil
	}
	_, _, err := streamListen(addr)
	return err
}
