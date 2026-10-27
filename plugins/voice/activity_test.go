// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package voice

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A restart brings back the finished lines; a streaming partial only meant
// something while its session ran.
func TestActivitySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.activity.jsonl")
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	logs := []LogEntry{
		{Time: at, Text: "🎤 最终: 把 <skill> 装上 & 删掉"},
		{Time: at.Add(time.Second), Text: "\r🎤 正在说"},
		{Time: at.Add(2 * time.Second), Text: "📋 已复制"},
	}
	if err := writeActivity(path, persistableLogs(logs)); err != nil {
		t.Fatal(err)
	}
	got := loadActivity(path)
	want := []LogEntry{logs[0], logs[2]}
	if !slices.EqualFunc(got, want, func(a, b LogEntry) bool { return a.Text == b.Text && a.Time.Equal(b.Time) }) {
		t.Fatalf("loaded %q, want %q", got, want)
	}
}

// Instances started with different -config files must not share a log.
func TestActivityPathPerConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := ActivityPath("/home/me/.config/just-talk/config.toml")
	b := ActivityPath("/home/me/.config/just-talk/meeting.toml")
	if a == b {
		t.Fatalf("both instances log to %s", a)
	}
	if filepath.Base(a) != "config.activity.jsonl" || filepath.Base(b) != "meeting.activity.jsonl" {
		t.Fatalf("paths %s, %s", a, b)
	}
	if got := filepath.Base(ActivityPath("")); got != "config.activity.jsonl" {
		t.Fatalf("no config file: %s", got)
	}
}
