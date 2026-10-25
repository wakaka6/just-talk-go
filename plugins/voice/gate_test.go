// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package voice

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

// pcmChunk fills one full-size chunk with a constant amplitude, so its RMS —
// and therefore pcmLevel — is that amplitude. 0 is digital silence.
func pcmChunk(amp int16) []byte {
	b := make([]byte, gateChunkBytes)
	for i := 0; i+1 < len(b); i += 2 {
		binary.LittleEndian.PutUint16(b[i:], uint16(amp))
	}
	return b
}

func firstSample(chunk []byte) int16 {
	return int16(binary.LittleEndian.Uint16(chunk))
}

// drainGate collects everything queued without blocking.
func drainGate(g *speechGate) []gateChunk {
	var out []gateChunk
	for {
		select {
		case c := <-g.chunks:
			out = append(out, c)
		default:
			return out
		}
	}
}

func TestGateOpensOnSpeechAndKeepsPreroll(t *testing.T) {
	g := newSpeechGate(true, discardLogger())
	// Silence is not forwarded, only remembered within the preroll budget.
	for range 5 {
		g.push(pcmChunk(0))
	}
	if queued := drainGate(g); len(queued) != 0 {
		t.Fatalf("silence forwarded %d chunks, want 0", len(queued))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	g.push(pcmChunk(1036)) // about -30 dBFS
	if !g.waitOpen(ctx) {
		t.Fatal("gate did not report speech")
	}
	queued := drainGate(g)
	want := gatePrerollBytes/gateChunkBytes + 1
	if len(queued) != want {
		t.Fatalf("queued %d chunks, want %d preroll + speech", len(queued), want)
	}
	for i, c := range queued[:len(queued)-1] {
		if c.last || firstSample(c.data) != 0 {
			t.Fatalf("preroll chunk %d = %+v, want silence and not last", i, firstSample(c.data))
		}
	}
	if final := queued[len(queued)-1]; final.last || firstSample(final.data) != 1036 {
		t.Fatalf("speech chunk sample = %d, last = %v", firstSample(final.data), final.last)
	}
}

func TestGateEndsUtteranceAfterHangover(t *testing.T) {
	g := newSpeechGate(true, discardLogger())
	g.push(pcmChunk(1036))
	silentChunks := int(gateHangover / chunkDuration(gateChunkBytes))
	for range silentChunks {
		g.push(pcmChunk(0))
	}
	queued := drainGate(g)
	if len(queued) != silentChunks+1 {
		t.Fatalf("queued %d chunks, want %d", len(queued), silentChunks+1)
	}
	// Silence inside the hangover still goes to the engine, so the tail of a
	// sentence is never cut; only the last chunk carries the flush flag.
	for i, c := range queued[:len(queued)-1] {
		if c.last {
			t.Fatalf("chunk %d marked last too early", i)
		}
	}
	if !queued[len(queued)-1].last {
		t.Fatal("utterance did not end with a final packet")
	}
	// The gate is shut again: more silence is only remembered.
	g.push(pcmChunk(0))
	if queued := drainGate(g); len(queued) != 0 {
		t.Fatalf("gate still forwarding after the utterance: %d chunks", len(queued))
	}
}

// Audio left over from a finished utterance must never reach the next session:
// the engine already recognised it, so re-sending it duplicates the sentence.
func TestGateDropsLeftoverAudioOnNextUtterance(t *testing.T) {
	g := newSpeechGate(true, discardLogger())
	g.push(pcmChunk(1036))
	for range int(gateHangover / chunkDuration(gateChunkBytes)) {
		g.push(pcmChunk(0))
	}
	// Nothing drained: this is a session that died before sending.
	if len(g.chunks) == 0 {
		t.Fatal("expected leftover audio to be queued")
	}

	g.push(pcmChunk(2000))
	queued := drainGate(g)
	if len(queued) != 1 {
		t.Fatalf("queued %d chunks, want only the new utterance", len(queued))
	}
	if got := firstSample(queued[0].data); got != 2000 {
		t.Fatalf("queued sample = %d, want the new utterance (2000)", got)
	}
}

func TestGateDisabledForwardsEverything(t *testing.T) {
	g := newSpeechGate(false, discardLogger())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !g.waitOpen(ctx) {
		t.Fatal("a disabled gate must let a session start right away")
	}
	for range 3 {
		g.push(pcmChunk(0))
	}
	queued := drainGate(g)
	if len(queued) != 3 {
		t.Fatalf("queued %d chunks, want 3", len(queued))
	}
	for i, c := range queued {
		if c.last {
			t.Fatalf("chunk %d marked last: a disabled gate has no utterance boundaries", i)
		}
	}
}
