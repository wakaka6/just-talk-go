package engine

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c/just-talk-go/config"
)

// reloadSpy is a plugin that only reports hot reloads.
type reloadSpy struct {
	basePlugin
	reloaded chan *config.Config
}

func (p *reloadSpy) Name() string                    { return "reload-spy" }
func (p *reloadSpy) Version() string                 { return "test" }
func (p *reloadSpy) Init(PluginEnv) error            { return nil }
func (p *reloadSpy) Start(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (p *reloadSpy) Stop() error                     { return nil }

func (p *reloadSpy) OnConfigReload(cfg *config.Config) error {
	select {
	case p.reloaded <- cfg:
	default:
	}
	return nil
}

// watchedConfig starts a watcher over a config file and returns the spy.
func watchedConfig(t *testing.T) (*reloadSpy, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[voice]\nlanguage = \"zh-CN\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	eng := New(nil, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spy := &reloadSpy{reloaded: make(chan *config.Config, 1)}
	if err := eng.LoadPlugin(spy); err != nil {
		t.Fatal(err)
	}
	// The engine is never started (no hotkey provider), so Stop would reach a
	// nil registry; the watcher goroutine ends with the test binary.
	eng.WatchConfig(path)
	return spy, path
}

func (p *reloadSpy) await(t *testing.T) *config.Config {
	t.Helper()
	select {
	case cfg := <-p.reloaded:
		return cfg
	case <-time.After(10 * time.Second):
		t.Fatal("config change did not reach the plugin")
		return nil
	}
}

func TestWatchConfigReloadsOnInPlaceWrite(t *testing.T) {
	spy, path := watchedConfig(t)
	if err := os.WriteFile(path, []byte("[voice]\nlanguage = \"en-US\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := spy.await(t).Voice.Language; got != "en-US" {
		t.Fatalf("language = %q after an in-place save", got)
	}
}

// Editors and scripts save atomically; that is a rename, not a write.
func TestWatchConfigReloadsOnAtomicSave(t *testing.T) {
	spy, path := watchedConfig(t)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("[voice]\nlanguage = \"ja-JP\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if got := spy.await(t).Voice.Language; got != "ja-JP" {
		t.Fatalf("language = %q after an atomic save", got)
	}
}
