// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package voice

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/c/just-talk-go/config"
)

// hotwordsLoader reads [voice] hotwords_file at the start of every session, so
// another program can keep the word list in step with what is being said. It
// logs only when the outcome changes: continuous mode opens a new session
// every few seconds and would otherwise flood the log.
type hotwordsLoader struct {
	mu   sync.Mutex
	last string
}

// load merges the configured hotwords with the ones in hotwords_file. An
// unreadable file is reported and skipped; recognition still runs with the
// configured list.
func (l *hotwordsLoader) load(logger *slog.Logger, vc config.VoiceConfig) []string {
	words := slices.Clone(vc.Hotwords)
	path := strings.TrimSpace(vc.HotwordsFile)
	if path == "" {
		l.forget()
		return dedupeWords(words)
	}
	full := config.ExpandPath(path)
	data, err := os.ReadFile(full)
	if err != nil {
		if l.changed("err:" + err.Error()) {
			logger.Error("hotwords file unreadable", "path", full, "error", err)
			pout("❌ 热词文件读不到: %v", err)
		}
		return dedupeWords(words)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			words = append(words, line)
		}
	}
	words = dedupeWords(words)
	if l.changed(fmt.Sprintf("ok:%s:%d", full, len(words))) {
		logger.Info("hotwords loaded", "path", full, "words", len(words))
	}
	return words
}

// changed reports whether state differs from the last reported one.
func (l *hotwordsLoader) changed(state string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == state {
		return false
	}
	l.last = state
	return true
}

func (l *hotwordsLoader) forget() { l.changed("") }

func dedupeWords(words []string) []string {
	out := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))
	for _, w := range words {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}
