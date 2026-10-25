// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package voice

import (
	"context"
	"log/slog"
	"time"
)

const (
	// Capture is 16 kHz mono s16le everywhere, so 32000 bytes is one second.
	gateBytesPerSecond = 32000
	// Read buffer size. The macOS capture pipe hands back 4096-byte pieces,
	// so a chunk covers 128–200 ms and its length has to be measured, not
	// assumed.
	gateChunkBytes = 6400
	// Queue between capture and the ASR sender, about 5 s: enough to cover a
	// WebSocket dial without losing the start of a sentence.
	gateQueueChunks = 40
	// Audio kept while the gate is shut, so the syllable that tripped the
	// level check is not the first thing the engine hears.
	gatePrerollBytes = 19200 // 600 ms
	// Hysteresis: one chunk at onset level opens the gate, and it takes a
	// quieter chunk to start the countdown that closes it again.
	gateOnsetLevel   = 0.15 // about -44 dBFS
	gateReleaseLevel = 0.08 // about -47 dBFS
	// Silence this long ends the utterance. An engine's own end-of-speech
	// timeout is far longer, so the gate always gets there first.
	gateHangover = 1200 * time.Millisecond
)

// speechGate cuts the continuous audio stream into utterances.
//
// In hold and toggle modes the user's finger does this job: it opens a session
// when they start talking and closes it when they stop, so silence never
// reaches the engine. Continuous mode has no finger. Without a gate it streams
// silence into a session the engine eventually closes by itself, which costs a
// reconnect and re-sends audio that was already recognised — the same sentence
// comes back twice. The gate plays the finger's part: speech opens a session,
// and silence closes it before the engine does.
//
// A disabled gate forwards every chunk, which is the behaviour from before it
// existed.
type speechGate struct {
	enabled bool
	logger  *slog.Logger

	chunks chan gateChunk
	free   chan []byte
	// opened signals that an utterance started. Capacity 1 so a session loop
	// still tearing down the previous utterance does not miss the next one.
	opened chan struct{}

	// Below is owned by the single pump goroutine.
	open       bool
	silentFor  time.Duration
	preroll    [][]byte
	prerollLen int
}

// gateChunk carries captured audio. last marks the final packet of an
// utterance, which asks the engine to flush its last result.
type gateChunk struct {
	data []byte
	last bool
}

func newSpeechGate(enabled bool, logger *slog.Logger) *speechGate {
	return &speechGate{
		enabled: enabled,
		logger:  logger,
		chunks:  make(chan gateChunk, gateQueueChunks),
		free:    make(chan []byte, gateQueueChunks+2),
		opened:  make(chan struct{}, 1),
	}
}

// acquire hands out a buffer for the next read.
func (g *speechGate) acquire() []byte {
	select {
	case chunk := <-g.free:
		return chunk[:gateChunkBytes]
	default:
		return make([]byte, gateChunkBytes)
	}
}

func (g *speechGate) release(chunk []byte) {
	if chunk == nil {
		return
	}
	select {
	case g.free <- chunk:
	default:
	}
}

// push hands one captured chunk to the gate; only the pump goroutine calls it.
func (g *speechGate) push(chunk []byte) {
	if !g.enabled {
		g.send(gateChunk{data: chunk})
		return
	}
	level := pcmLevel(chunk)
	if g.open {
		if level >= gateReleaseLevel {
			g.silentFor = 0
			g.send(gateChunk{data: chunk})
			return
		}
		g.silentFor += chunkDuration(len(chunk))
		if g.silentFor < gateHangover {
			g.send(gateChunk{data: chunk})
			return
		}
		g.open, g.silentFor = false, 0
		g.logger.Debug("gate: utterance ended", "level", level)
		g.send(gateChunk{data: chunk, last: true})
		return
	}
	if level < gateOnsetLevel {
		g.keepPreroll(chunk)
		return
	}
	g.open, g.silentFor = true, 0
	g.logger.Debug("gate: speech started", "level", level)
	// Anything still queued belongs to an utterance that already ended;
	// sending it again would have the engine recognise it twice.
	g.discard()
	g.flushPreroll()
	g.send(gateChunk{data: chunk})
	select {
	case g.opened <- struct{}{}:
	default:
	}
}

// keepPreroll remembers the newest audio while the gate is shut, within the
// preroll budget. The newest chunk is always kept, however long it is.
func (g *speechGate) keepPreroll(chunk []byte) {
	g.preroll = append(g.preroll, chunk)
	g.prerollLen += len(chunk)
	for g.prerollLen > gatePrerollBytes && len(g.preroll) > 1 {
		g.prerollLen -= len(g.preroll[0])
		g.release(g.preroll[0])
		g.preroll = append(g.preroll[:0], g.preroll[1:]...)
	}
}

func (g *speechGate) flushPreroll() {
	for _, chunk := range g.preroll {
		g.send(gateChunk{data: chunk})
	}
	g.preroll, g.prerollLen = g.preroll[:0], 0
}

// discard drops queued audio and recycles its buffers.
func (g *speechGate) discard() {
	for {
		select {
		case c := <-g.chunks:
			g.release(c.data)
		default:
			return
		}
	}
}

// send queues a chunk, dropping the oldest one when the queue is full so the
// newest audio always gets through.
func (g *speechGate) send(c gateChunk) {
	select {
	case g.chunks <- c:
		return
	default:
	}
	select {
	case old := <-g.chunks:
		g.release(old.data)
	default:
	}
	select {
	case g.chunks <- c:
	default:
		g.release(c.data)
	}
}

// waitOpen blocks until an utterance starts and reports false if ctx ended
// first. A disabled gate has no utterance boundaries, so a session may open
// right away.
func (g *speechGate) waitOpen(ctx context.Context) bool {
	if !g.enabled {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-g.opened:
		return true
	}
}

// chunkDuration is how much time n bytes of captured audio cover.
func chunkDuration(n int) time.Duration {
	return time.Duration(n) * time.Second / gateBytesPerSecond
}
