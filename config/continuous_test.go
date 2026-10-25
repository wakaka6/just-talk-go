package config

import (
	"os"
	"path/filepath"
	"testing"
)

// An old config said mode = "continuous", which meant: no hotkey, no text
// output, and the device is the external source. Migrating it must keep all
// three, above all the missing hotkey — otherwise the migration invents a
// dictation that types into whatever window happens to be focused.
func TestContinuousModeMigratesToItsOwnSection(t *testing.T) {
	cfg := loadFrom(t, "[voice]\nmode = \"continuous\"\ndevice = \"BlackHole 2ch\"\n")
	vc := cfg.Voice
	if !vc.Continuous.Enabled {
		t.Fatal("continuous transcription is off after migrating from mode = continuous")
	}
	if vc.Continuous.Device != "BlackHole 2ch" {
		t.Fatalf("continuous device = %q, want the device the old config recorded from", vc.Continuous.Device)
	}
	if vc.Device != "" {
		t.Fatalf("dictation device = %q, want empty: that device was the external source", vc.Device)
	}
	if vc.PushToTalk != "" {
		t.Fatalf("push_to_talk = %q, want none: the old config registered no hotkey", vc.PushToTalk)
	}
	if !ValidMode(vc.Mode) {
		t.Fatalf("mode = %q is not a dictation mode", vc.Mode)
	}
	if !vc.Continuous.SilenceGate {
		t.Fatal("silence gate is off by default")
	}
}

// A -config instance must write its own file. Saving to whatever FindConfig
// turns up overwrites the config of a different instance.
func TestSaveWritesTheGivenFileNotTheDiscoveredOne(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.toml", []byte("[voice]\nlanguage = \"zh-CN\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "interview.toml")

	cfg := Default()
	cfg.Voice.Language = "en-US"
	if err := Save(cfg, mine); err != nil {
		t.Fatal(err)
	}

	saved, err := Load(mine)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Voice.Language != "en-US" {
		t.Fatalf("saved language = %q, want the edit in the file that was passed in", saved.Voice.Language)
	}
	other, err := Load("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if other.Voice.Language != "zh-CN" {
		t.Fatalf("./config.toml language = %q: saving overwrote another instance's config", other.Voice.Language)
	}
}
