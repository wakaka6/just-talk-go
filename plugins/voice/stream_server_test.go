package voice

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
