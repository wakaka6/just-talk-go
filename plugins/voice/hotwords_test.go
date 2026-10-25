package voice

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/c/just-talk-go/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestHotwordsMergesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hotwords.txt")
	content := "# 注释\n功能点耗时率\n\n  德尔菲  \n功能点耗时率\nEPG\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var l hotwordsLoader
	got := l.load(quietLogger(), config.VoiceConfig{
		Hotwords:     []string{"裁剪指南", "德尔菲"},
		HotwordsFile: path,
	})
	want := []string{"裁剪指南", "德尔菲", "功能点耗时率", "EPG"}
	if !slices.Equal(got, want) {
		t.Fatalf("hotwords = %q, want %q", got, want)
	}
}

// A missing file must not take the configured hotwords down with it.
func TestHotwordsKeepsListWhenFileMissing(t *testing.T) {
	var l hotwordsLoader
	got := l.load(quietLogger(), config.VoiceConfig{
		Hotwords:     []string{"裁剪指南"},
		HotwordsFile: filepath.Join(t.TempDir(), "nope.txt"),
	})
	if !slices.Equal(got, []string{"裁剪指南"}) {
		t.Fatalf("hotwords = %q, want the configured list", got)
	}
}

// Continuous mode reopens a session every few seconds; only a change is worth
// a log line.
func TestHotwordsReportsOnlyOnChange(t *testing.T) {
	var l hotwordsLoader
	if !l.changed("ok:1") {
		t.Fatal("first outcome should be reported")
	}
	if l.changed("ok:1") {
		t.Fatal("unchanged outcome reported twice")
	}
	if !l.changed("err:boom") {
		t.Fatal("a new outcome should be reported")
	}
}
