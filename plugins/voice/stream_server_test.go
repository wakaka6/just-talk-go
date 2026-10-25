package voice

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/c/just-talk-go/internal/speech"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestStreamEventEncoding(t *testing.T) {
	cases := []struct {
		event any
		want  string
	}{
		{newSessionStartEvent("jt-1"), `{"type":"session","state":"start","session":"jt-1"}`},
		{newSessionEndEvent("jt-1", sessionEndError), `{"type":"session","state":"end","session":"jt-1","reason":"error"}`},
		{
			newASREvent("jt-1", "你好。我们", []speech.Utterance{{Text: "你好。", Definite: true}, {Text: "我们"}}, false),
			`{"type":"asr","session":"jt-1","text":"你好。我们","utterances":[{"text":"你好。","definite":true},{"text":"我们","definite":false}],"final":false}`,
		},
		{newASREvent("jt-2", "", nil, true), `{"type":"asr","session":"jt-2","text":"","utterances":[],"final":true}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.event)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("encoded %s\nwant    %s", got, c.want)
		}
	}
}

func TestCheckLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7590", "[::1]:7590", "localhost:7590", "127.0.0.2:1"} {
		if err := checkLoopbackAddr(addr); err != nil {
			t.Errorf("checkLoopbackAddr(%q) = %v", addr, err)
		}
	}
	for _, addr := range []string{":7590", "0.0.0.0:7590", "[::]:7590", "192.168.1.2:7590", "example.com:7590", "127.0.0.1"} {
		if err := checkLoopbackAddr(addr); err == nil {
			t.Errorf("checkLoopbackAddr(%q) accepted a non-loopback address", addr)
		}
	}
	if _, err := newStreamServer("0.0.0.0:0", discardLogger()); err == nil {
		t.Fatal("newStreamServer listened on a non-loopback address")
	}
}

func TestStreamServerBroadcastsToAllSubscribers(t *testing.T) {
	srv, err := newStreamServer("127.0.0.1:0", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var conns []*websocket.Conn
	for range 2 {
		conn, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+streamPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		conns = append(conns, conn)
	}
	waitForSubscribers(t, srv, 2)

	srv.broadcast(newSessionStartEvent("jt-7"))
	srv.broadcast(newASREvent("jt-7", "天气", []speech.Utterance{{Text: "天气", Definite: true}}, false))
	for i, conn := range conns {
		var start streamSessionEvent
		readJSON(t, ctx, conn, &start)
		if start != newSessionStartEvent("jt-7") {
			t.Fatalf("subscriber %d session frame = %+v", i, start)
		}
		var asr streamASREvent
		readJSON(t, ctx, conn, &asr)
		if asr.Type != "asr" || asr.Text != "天气" || len(asr.Utterances) != 1 || !asr.Utterances[0].Definite {
			t.Fatalf("subscriber %d asr frame = %+v", i, asr)
		}
	}
}

// A subscriber must not be able to tell that others come and go: the stream
// carries ASR results only, never anything about the subscriber set.
func TestStreamServerSubscribersAreInvisibleToEachOther(t *testing.T) {
	srv, err := newStreamServer("127.0.0.1:0", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func() *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+streamPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}

	first := dial()
	defer first.CloseNow()
	waitForSubscribers(t, srv, 1)
	srv.broadcast(newSessionStartEvent("jt-1"))

	second := dial()
	waitForSubscribers(t, srv, 2)
	srv.broadcast(newASREvent("jt-1", "一", nil, false))
	second.CloseNow()
	waitForSubscribers(t, srv, 1)
	srv.broadcast(newASREvent("jt-1", "二", nil, true))

	// The joining and the leaving of the second subscriber inserted nothing:
	// the next frames are exactly what was broadcast.
	var start streamSessionEvent
	readJSON(t, ctx, first, &start)
	if start != newSessionStartEvent("jt-1") {
		t.Fatalf("first frame = %+v", start)
	}
	for _, want := range []string{"一", "二"} {
		var asr streamASREvent
		readJSON(t, ctx, first, &asr)
		if asr.Type != "asr" || asr.Text != want {
			t.Fatalf("frame = %+v, want asr %q", asr, want)
		}
	}
}

// While nobody is talking the stream is silent, so a subscriber that just
// connected is told what the service is doing before anything else.
func TestSubscriberIsToldTheServiceStateFirst(t *testing.T) {
	srv, err := newStreamServer("127.0.0.1:0", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.setSnapshot(func() any {
		return streamServiceEvent{Type: "service", State: serviceListening, Device: "BlackHole 2ch", Gate: true}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+streamPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitForSubscribers(t, srv, 1)
	srv.broadcast(newSessionStartEvent("jt-1"))

	var got streamServiceEvent
	readJSON(t, ctx, conn, &got)
	if got.Type != "service" || got.State != serviceListening || got.Device != "BlackHole 2ch" || !got.Gate {
		t.Fatalf("first frame = %+v, want the service snapshot", got)
	}
	// And it really is first: the broadcast comes after it.
	var next streamSessionEvent
	readJSON(t, ctx, conn, &next)
	if next != newSessionStartEvent("jt-1") {
		t.Fatalf("second frame = %+v, want the broadcast", next)
	}
}

// A Unix socket names the instance by path and lets only its owner listen.
func TestStreamServerOnUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asr.sock")
	srv, err := newStreamServer("unix:"+path, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if want := "unix:" + path; srv.URL() != want {
		t.Fatalf("URL = %q, want %q", srv.URL(), want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode = %o, want 600: anyone else could listen in", perm)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
	conn, _, err := websocket.Dial(ctx, "ws://socket"+streamPath, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	waitForSubscribers(t, srv, 1)
	srv.broadcast(newSessionStartEvent("jt-1"))
	var got streamSessionEvent
	readJSON(t, ctx, conn, &got)
	if got != newSessionStartEvent("jt-1") {
		t.Fatalf("frame over the socket = %+v", got)
	}
}

// A mistyped stream_addr must never cost the user the file it names.
func TestStreamServerKeepsAFileThatIsNotASocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "notes.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if srv, err := newStreamServer("unix:"+path, discardLogger()); err == nil {
		srv.Close()
		t.Fatal("listened on a path that holds a regular file")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep me" {
		t.Fatalf("the file was touched: %q, %v", data, err)
	}
}

// A socket left behind by a crash nobody listens on is cleared, not an error.
func TestStreamServerReplacesAStaleSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "asr.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	srv, err := newStreamServer("unix:"+path, discardLogger())
	if err != nil {
		t.Fatalf("a stale socket blocked the server: %v", err)
	}
	srv.Close()
}

// shortTempDir keeps socket paths under the ~104-byte sun_path limit, which
// t.TempDir() overruns on macOS for longer test names.
func shortTempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "jt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCheckStreamAcceptsBothKinds(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7590", "localhost:0", "unix:/tmp/jt.sock", "unix:~/jt.sock"} {
		if err := CheckStream(true, addr); err != nil {
			t.Fatalf("CheckStream(%q) = %v, want accepted", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:7590", "192.168.1.5:7590", "unix:", "7590"} {
		// Off does not excuse a bad address: switching on later must not fail.
		if err := CheckStream(false, addr); err == nil {
			t.Fatalf("CheckStream(%q) was accepted", addr)
		}
	}
	if err := CheckStream(true, ""); err == nil {
		t.Fatal("switched on with no address was accepted")
	}
	if err := CheckStream(false, ""); err != nil {
		t.Fatalf("switched off with no address = %v, want accepted", err)
	}
}

func TestStreamServerSlowSubscriberDoesNotBlock(t *testing.T) {
	srv, err := newStreamServer("127.0.0.1:0", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	// Never drained: stands in for a subscriber whose socket is stuck.
	slow := &streamSubscriber{frames: make(chan []byte, 1), kicked: make(chan struct{})}
	fast := &streamSubscriber{frames: make(chan []byte, 16), kicked: make(chan struct{})}
	srv.add(slow)
	srv.add(fast)

	start := time.Now()
	for range 8 {
		srv.broadcast(newSessionStartEvent("jt-8"))
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("broadcast blocked for %s", elapsed)
	}
	select {
	case <-slow.kicked:
	default:
		t.Fatal("slow subscriber was not disconnected")
	}
	if len(fast.frames) != 8 {
		t.Fatalf("fast subscriber got %d frames, want 8", len(fast.frames))
	}
	if n := srv.subscriberCount(); n != 1 {
		t.Fatalf("subscriberCount = %d, want 1", n)
	}
}

func waitForSubscribers(t *testing.T, srv *streamServer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for srv.subscriberCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d subscribers registered", srv.subscriberCount(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}
