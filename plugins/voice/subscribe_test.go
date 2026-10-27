package voice

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func subscribeServer(t *testing.T) *streamServer {
	t.Helper()
	srv, err := newStreamServer("127.0.0.1:0", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	srv.setSnapshot(func() any { return streamServiceEvent{Type: "service", State: serviceListening, Gate: true} })
	return srv
}

// One frame per line on stdout, a ready marker and an exit reason on stderr:
// that is the contract other local programs subscribe with.
func TestSubscribePrintsFramesAndStopsAtTheLimit(t *testing.T) {
	srv := subscribeServer(t)

	var out, errOut bytes.Buffer
	frames, reason, err := Subscribe(context.Background(), srv.Addr(), SubscribeOptions{Out: &out, Err: &errOut, MaxEvents: 1})
	if err != nil {
		t.Fatal(err)
	}
	if frames != 1 || reason != "limit" {
		t.Fatalf("frames=%d reason=%s, want 1 limit", frames, reason)
	}
	if line := strings.TrimSpace(out.String()); !strings.Contains(line, `"type":"service"`) || strings.Contains(line, "\n") {
		t.Fatalf("stdout = %q, want one NDJSON service frame", line)
	}
	if !strings.Contains(errOut.String(), "[subscribe] ready addr=ws://") ||
		!strings.Contains(errOut.String(), "(reason: limit)") {
		t.Fatalf("stderr = %q, want a ready marker and an exit line", errOut.String())
	}
}

// The timeout bound ends the run without dropping anything already received.
func TestSubscribeStopsAtTheTimeout(t *testing.T) {
	srv := subscribeServer(t)
	var out bytes.Buffer
	frames, reason, err := Subscribe(context.Background(), srv.Addr(), SubscribeOptions{Out: &out, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if reason != "timeout" {
		t.Fatalf("reason = %s, want timeout", reason)
	}
	// The service frame sent on connect is the one frame that arrives.
	if frames != 1 {
		t.Fatalf("frames = %d, want the connect snapshot", frames)
	}
}

// The service stopping closes the connection; that is a normal end, not an
// error.
func TestSubscribeEndsWhenTheServiceCloses(t *testing.T) {
	srv := subscribeServer(t)
	type result struct {
		frames int
		reason string
		err    error
	}
	done := make(chan result, 1)
	var out bytes.Buffer
	go func() {
		f, r, e := Subscribe(context.Background(), srv.Addr(), SubscribeOptions{Out: &out})
		done <- result{f, r, e}
	}()
	waitForSubscribers(t, srv, 1)
	srv.Close()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.reason != "closed" {
			t.Fatalf("reason = %s, want closed", res.reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the service did not end the subscription")
	}
}

// Nothing listening is the one real error: the service only runs while
// continuous transcription does.
func TestSubscribeFailsWhenNothingIsListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	var out bytes.Buffer
	_, _, err = Subscribe(context.Background(), addr, SubscribeOptions{Out: &out})
	if err == nil {
		t.Fatal("connecting to a dead address succeeded")
	}
	if !strings.Contains(err.Error(), "持续转写") && !strings.Contains(err.Error(), addr) {
		t.Fatalf("error = %v, want the address and what to do about it", err)
	}
}

// A unix: address names the instance and lets only its owner subscribe.
func TestSubscribeOverUnixSocket(t *testing.T) {
	skipUnixOnWindows(t)
	path := filepath.Join(shortTempDir(t), "asr.sock")
	srv, err := newStreamServer("unix:"+path, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.setSnapshot(func() any { return streamServiceEvent{Type: "service", State: serviceListening, Gate: true} })

	var out bytes.Buffer
	_, _, err = Subscribe(context.Background(), "unix:"+path, SubscribeOptions{Out: &out, Err: &bytes.Buffer{}, MaxEvents: 1})
	if err != nil {
		t.Fatal(err)
	}
	if line := strings.TrimSpace(out.String()); !strings.Contains(line, `"type":"service"`) {
		t.Fatalf("stdout = %q, want the service frame", line)
	}
}
