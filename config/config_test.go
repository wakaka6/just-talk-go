package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/c/just-talk-go/hotkey"
)

func TestLoadOverlayPosition(t *testing.T) {
	for _, test := range []struct {
		content string
		want    string
	}{
		{content: "[overlay]\nenabled = true\n", want: DefaultOverlayPosition()},
		{content: "[overlay]\nposition = \"bottom-center\"\n", want: "bottom-center"},
		{content: "[overlay]\nposition = \"notch\"\n", want: "notch"},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Overlay.Position != test.want {
			t.Fatalf("Load(%q).Overlay.Position = %q, want %q", test.content, cfg.Overlay.Position, test.want)
		}
	}
}

func TestParseHotkeyFnModifier(t *testing.T) {
	for _, input := range []string{"Fn", "Function"} {
		combo, err := ParseHotkey(input)
		if err != nil {
			t.Fatalf("ParseHotkey(%q): %v", input, err)
		}
		want := hotkey.Combo{Mods: hotkey.ModFn, Key: hotkey.KeyNone}
		if combo != want {
			t.Fatalf("ParseHotkey(%q) = %v, want %v", input, combo, want)
		}
	}
	for _, test := range []struct {
		input string
		key   hotkey.KeyCode
	}{
		{input: "Fn+F5", key: hotkey.KeyF5},
		{input: "Fn+A", key: hotkey.KeyA},
	} {
		combo, err := ParseHotkey(test.input)
		if err != nil {
			t.Fatalf("ParseHotkey(%q): %v", test.input, err)
		}
		want := hotkey.Combo{Mods: hotkey.ModFn, Key: test.key}
		if combo != want {
			t.Fatalf("ParseHotkey(%q) = %v, want %v", test.input, combo, want)
		}
	}
}

func TestParseHotkeyList(t *testing.T) {
	fn := hotkey.Combo{Mods: hotkey.ModFn}
	f13 := hotkey.Combo{Key: hotkey.KeyF13}
	fnComma := hotkey.Combo{Mods: hotkey.ModFn, Key: hotkey.KeyComma}
	for _, test := range []struct {
		input string
		want  []hotkey.Combo
	}{
		{input: "Fn", want: []hotkey.Combo{fn}},
		{input: "Fn, F13", want: []hotkey.Combo{fn, f13}},
		{input: "Fn，F13", want: []hotkey.Combo{fn, f13}},
		{input: "Fn, F13, fn", want: []hotkey.Combo{fn, f13}},
		{input: "Fn+,", want: []hotkey.Combo{fnComma}},
		{input: "Fn+, , F13", want: []hotkey.Combo{fnComma, f13}},
		{input: " Fn , ,F13, ", want: []hotkey.Combo{fn, f13}},
		{input: "Fn, Menu", want: []hotkey.Combo{fn, {Key: hotkey.KeyMenu}}},
		{input: "Apps, application, menu", want: []hotkey.Combo{{Key: hotkey.KeyMenu}}},
	} {
		got, err := ParseHotkeyList(test.input)
		if err != nil {
			t.Fatalf("ParseHotkeyList(%q): %v", test.input, err)
		}
		if !slices.Equal(got, test.want) {
			t.Fatalf("ParseHotkeyList(%q) = %v, want %v", test.input, got, test.want)
		}
	}
	for _, input := range []string{"", " , ", "Fn, Bogus"} {
		if _, err := ParseHotkeyList(input); err == nil {
			t.Fatalf("ParseHotkeyList(%q) succeeded, want error", input)
		}
	}
}
