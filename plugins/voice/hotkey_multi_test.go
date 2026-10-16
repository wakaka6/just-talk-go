package voice

import (
	"io"
	"log/slog"
	"testing"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/engine"
	"github.com/c/just-talk-go/hotkey"
)

// hotkeyEnv records which combos are currently registered.
type hotkeyEnv struct {
	registered map[hotkey.Combo]int // combo -> number of Register calls
	active     map[hotkey.Combo]bool
}

func newHotkeyEnv() *hotkeyEnv {
	return &hotkeyEnv{registered: map[hotkey.Combo]int{}, active: map[hotkey.Combo]bool{}}
}

func (e *hotkeyEnv) RegisterHotkey(c hotkey.Combo, h func(hotkey.Event)) error {
	return e.RegisterHotkeyWithOptions(c, hotkey.RegisterOptions{}, h)
}

func (e *hotkeyEnv) RegisterHotkeyWithOptions(c hotkey.Combo, _ hotkey.RegisterOptions, _ func(hotkey.Event)) error {
	e.registered[c]++
	e.active[c] = true
	return nil
}

func (e *hotkeyEnv) UnregisterHotkey(c hotkey.Combo) error {
	delete(e.active, c)
	return nil
}

func (e *hotkeyEnv) Logger() *slog.Logger   { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (e *hotkeyEnv) Config() *config.Config { return config.Default() }
func (e *hotkeyEnv) Engine() *engine.Engine { return nil }

var (
	comboFn  = hotkey.Combo{Mods: hotkey.ModFn}
	comboF13 = hotkey.Combo{Key: hotkey.KeyF13}
)

func TestReloadRegistersEveryPushToTalkHotkey(t *testing.T) {
	env := newHotkeyEnv()
	p := NewVoicePlugin()
	p.env, p.logger = env, env.Logger()
	cfg := config.Default()
	cfg.Voice.Mode = "hold"
	cfg.Voice.PushToTalk = "Fn, F13"
	if err := p.registerFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if !env.active[comboFn] || !env.active[comboF13] || len(env.active) != 2 {
		t.Fatalf("active hotkeys = %v, want Fn and F13", env.active)
	}

	next := *cfg
	next.Voice.PushToTalk = "F13"
	if err := p.registerFromConfig(&next); err != nil {
		t.Fatal(err)
	}
	if env.active[comboFn] || !env.active[comboF13] {
		t.Fatalf("active hotkeys = %v, want only F13", env.active)
	}
	if env.registered[comboF13] != 1 {
		t.Fatalf("F13 registered %d times; an unchanged hotkey must stay registered across reloads", env.registered[comboF13])
	}
}

func TestHoldModeStopsOnlyAfterAllHotkeysReleased(t *testing.T) {
	SetOutput(io.Discard)
	p := NewVoicePlugin()
	env := newHotkeyEnv()
	p.env, p.logger = env, env.Logger()
	p.mode, p.recording, p.stopDelayMs = "hold", true, 60_000
	t.Cleanup(func() {
		p.mu.Lock()
		p.cancelStopDelayOnlyLocked()
		p.mu.Unlock()
	})

	p.handleHotkey(hotkey.Event{Combo: comboFn, Type: hotkey.KeyDown})
	p.handleHotkey(hotkey.Event{Combo: comboF13, Type: hotkey.KeyDown})
	p.handleHotkey(hotkey.Event{Combo: comboFn, Type: hotkey.KeyUp})
	p.mu.Lock()
	stopping := p.stopping
	p.mu.Unlock()
	if stopping {
		t.Fatal("releasing one hotkey stopped recording while another was still held")
	}

	p.handleHotkey(hotkey.Event{Combo: comboF13, Type: hotkey.KeyUp})
	p.mu.Lock()
	stopping = p.stopping
	p.mu.Unlock()
	if !stopping {
		t.Fatal("releasing the last held hotkey did not stop recording")
	}
}
