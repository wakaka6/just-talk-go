package voice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// SubscribeOptions bounds one subscription run and receives the protocol's
// stderr lines. Out is where frames go; Err defaults to io.Discard.
type SubscribeOptions struct {
	Out io.Writer
	Err io.Writer
	// MaxEvents ends the run after that many frames; 0 is unlimited.
	MaxEvents int
	// Timeout ends the run after that long; 0 is unlimited. Whichever bound
	// fires first wins.
	Timeout time.Duration
}

// Exit reasons, reported on the last stderr line and in the return. They all
// exit 0: the run did what was asked. A dial failure is the only error.
const (
	subscribeReasonLimit   = "limit"
	subscribeReasonTimeout = "timeout"
	subscribeReasonSignal  = "signal"
	subscribeReasonClosed  = "closed"
)

// Subscribe connects to the recognition-result subscription and writes every
// frame as NDJSON to Out until a bound fires or the service closes the
// connection. It is the contract for other local programs — agents above all
// — that consume Just Talk as a subprocess: a ready marker on connect and an
// exit line with the reason go to Err, exactly the way lark-cli event consume
// and omp's RPC mode do it. err is non-nil only when connecting fails.
func Subscribe(ctx context.Context, addr string, opts SubscribeOptions) (frames int, reason string, err error) {
	errw := opts.Err
	if errw == nil {
		errw = io.Discard
	}
	if opts.Out == nil {
		return 0, "", errors.New("subscribe: no output writer")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	network, target, err := streamListen(addr)
	if err != nil {
		return 0, "", err
	}
	url := "ws://" + target + streamPath
	display := url
	var dialOpts *websocket.DialOptions
	if network == "unix" {
		display = "unix:" + target
		url = "ws://subscribe" + streamPath // host is ignored; the URL must be well-formed
		client := &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", target)
			},
		}}
		dialOpts = &websocket.DialOptions{HTTPClient: client}
	}

	conn, _, err := websocket.Dial(ctx, url, dialOpts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, subscribeReasonFor(ctxErr), nil
		}
		return 0, "", fmt.Errorf("连接 %s 失败: %w；Just Talk 没在运行，或地址与配置的 stream_addr 不一致", display, err)
	}
	defer conn.CloseNow()
	fmt.Fprintf(errw, "[subscribe] ready addr=%s\n", display)

	start := time.Now()
	defer func() {
		fmt.Fprintf(errw, "[subscribe] exited — received %d frame(s) in %s (reason: %s)\n",
			frames, time.Since(start).Round(time.Millisecond), reason)
	}()
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return frames, subscribeReasonFor(ctxErr), nil
		}
		_, r, err := conn.Reader(ctx)
		if err != nil {
			// A reader error is the service closing the connection — the
			// transcription stopped — unless our own bound ended the run.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return frames, subscribeReasonFor(ctxErr), nil
			}
			return frames, subscribeReasonClosed, nil
		}
		data, err := io.ReadAll(r)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return frames, subscribeReasonFor(ctxErr), nil
			}
			return frames, subscribeReasonClosed, nil
		}
		if _, err := opts.Out.Write(append(data, '\n')); err != nil {
			return frames, subscribeReasonClosed, nil
		}
		frames++
		if opts.MaxEvents > 0 && frames >= opts.MaxEvents {
			return frames, subscribeReasonLimit, nil
		}
	}
}

// subscribeReasonFor maps the run's context error to the exit reason: a
// deadline is the timeout, a cancellation is the caller stopping the run.
func subscribeReasonFor(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return subscribeReasonTimeout
	}
	return subscribeReasonSignal
}
