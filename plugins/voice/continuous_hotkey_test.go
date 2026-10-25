package voice

import (
	"testing"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
)

func continuousPlugin(t *testing.T, cfg *config.Config) (*VoicePlugin, *hotkeyEnv) {
	t.Helper()
	env := newHotkeyEnv()
	p := NewVoicePlugin()
	p.env, p.logger = env, env.Logger()
	if err := p.registerFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return p, env
}

// Both hotkeys are registered at once: the service is not a dictation mode,
// so turning it on must not cost the user their dictation key.
func TestContinuousHotkeyRegistersAlongsideDictation(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.PushToTalk = "Fn"
	cfg.Voice.Continuous.Hotkey = "F13"
	_, env := continuousPlugin(t, cfg)
	if !env.active[comboFn] || !env.active[comboF13] || len(env.active) != 2 {
		t.Fatalf("active hotkeys = %v, want both Fn and F13", env.active)
	}
}

func TestContinuousHotkeyTogglesTheService(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.PushToTalk = "Fn"
	cfg.Voice.Continuous.Hotkey = "F13"
	p, _ := continuousPlugin(t, cfg)
	if p.contEnabled {
		t.Fatal("the service is on without being asked for")
	}

	down := hotkey.Event{Combo: comboF13, Type: hotkey.KeyDown}
	p.onContinuousHotkey(down)
	if !p.contEnabled {
		t.Fatal("pressing the hotkey did not turn the service on")
	}
	// A key bounds the service, not a sentence: releasing changes nothing.
	p.onContinuousHotkey(hotkey.Event{Combo: comboF13, Type: hotkey.KeyUp})
	if !p.contEnabled {
		t.Fatal("releasing the hotkey turned the service off")
	}
	p.onContinuousHotkey(down)
	if p.contEnabled {
		t.Fatal("pressing the hotkey again did not turn the service off")
	}
}

// The tray panel saves the setting and switches the service. After the
// hotkey turned it on over a saved "off", saving "off" changes nothing in
// the config, and the panel's off must still stop it.
func TestPanelSwitchStopsWhatTheHotkeyStarted(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.PushToTalk = "Fn"
	cfg.Voice.Continuous.Hotkey = "F13"
	p, _ := continuousPlugin(t, cfg)
	liveVoice.Store(p)
	t.Cleanup(func() { liveVoice.Store(nil) })

	p.onContinuousHotkey(hotkey.Event{Combo: comboF13, Type: hotkey.KeyDown})
	if !ContinuousOn() {
		t.Fatal("the panel does not see what the hotkey switched on")
	}
	same := *cfg
	if err := p.registerFromConfig(&same); err != nil {
		t.Fatal(err)
	}
	SetContinuous(false)
	if ContinuousOn() {
		t.Fatal("the panel turned it off, but it is still on")
	}
}

// Editing the setting is an explicit instruction and outranks an earlier
// toggle; a reload that does not touch it must leave the toggle alone.
func TestConfigReloadOverridesTheToggleOnlyWhenItChanges(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Continuous.Hotkey = "F13"
	p, _ := continuousPlugin(t, cfg)
	p.onContinuousHotkey(hotkey.Event{Combo: comboF13, Type: hotkey.KeyDown})

	unrelated := *cfg
	unrelated.Voice.Language = "en-US"
	if err := p.registerFromConfig(&unrelated); err != nil {
		t.Fatal(err)
	}
	if !p.contEnabled {
		t.Fatal("an unrelated config reload undid the hotkey toggle")
	}

	off := *cfg
	off.Voice.Continuous.Enabled = true
	if err := p.registerFromConfig(&off); err != nil {
		t.Fatal(err)
	}
	if !p.contEnabled {
		t.Fatal("enabling it in the config should keep it on")
	}
	back := *cfg
	back.Voice.Continuous.Enabled = false
	if err := p.registerFromConfig(&back); err != nil {
		t.Fatal(err)
	}
	if p.contEnabled {
		t.Fatal("turning it off in the config did not take effect")
	}
}

// One combo cannot mean two things.
func TestContinuousHotkeyCannotShadowDictation(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.PushToTalk = "Fn, F13"
	cfg.Voice.Continuous.Hotkey = "F13"
	env := newHotkeyEnv()
	p := NewVoicePlugin()
	p.env, p.logger = env, env.Logger()
	if err := p.registerFromConfig(cfg); err == nil {
		t.Fatal("a hotkey bound to both dictation and the service was accepted")
	}
}
