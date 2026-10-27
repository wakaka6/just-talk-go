package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/plugins/voice"
)

// runSubscribe implements `just-talk subscribe`: consume the recognition-result
// subscription as a subprocess, the way agents consume other local
// applications. NDJSON frames on stdout, ready marker and exit reason on
// stderr, exit 0 for every bounded or normal end and 1 for a connect failure.
func runSubscribe(args []string) int {
	fs := flag.NewFlagSet("subscribe", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	addr := fs.String("addr", "", "subscription address (default: the config's stream_addr)")
	timeout := fs.Duration("timeout", 0, "exit after this long, e.g. 30s or 10m; 0 = unlimited")
	maxEvents := fs.Int("max-events", 0, "exit after this many frames; 0 = unlimited")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		return 1
	}
	target := strings.TrimSpace(*addr)
	if target == "" {
		target = cfg.Voice.StreamAddr
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, _, err = voice.Subscribe(ctx, target, voice.SubscribeOptions{
		Out:       os.Stdout,
		Err:       os.Stderr,
		MaxEvents: *maxEvents,
		Timeout:   *timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[subscribe] error: %v\n", err)
		return 1
	}
	return 0
}
