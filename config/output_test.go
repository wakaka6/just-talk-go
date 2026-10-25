package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFrom(t *testing.T, content string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadMigratesAutoSubmit(t *testing.T) {
	for _, test := range []struct {
		name, content, want string
	}{
		{"empty config", "[voice]\n", OutputPaste},
		{"legacy paste", "[voice]\nauto_submit = true\n", OutputPaste},
		{"legacy clipboard", "[voice]\nauto_submit = false\n", OutputClipboard},
		{"explicit none", "[voice]\ntext_output = \"none\"\n", OutputNone},
		// A stale auto_submit must not override an explicit text_output.
		{"both", "[voice]\nauto_submit = true\ntext_output = \"none\"\n", OutputNone},
		{"garbage", "[voice]\ntext_output = \"paste-it\"\n", OutputPaste},
	} {
		cfg := loadFrom(t, test.content)
		if got := cfg.Voice.TextOutput; got != test.want {
			t.Errorf("%s: TextOutput = %q, want %q", test.name, got, test.want)
		}
		if cfg.Voice.AutoSubmit != nil {
			t.Errorf("%s: the legacy field survived the migration", test.name)
		}
	}
}

// Once migrated, the legacy key must not come back when the config is saved.
func TestSaveDropsAutoSubmit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.toml", []byte("[voice]\nauto_submit = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "auto_submit") {
		t.Fatalf("saved config still has auto_submit:\n%s", saved)
	}
	if !strings.Contains(string(saved), `text_output = "clipboard"`) {
		t.Fatalf("saved config lost the migrated value:\n%s", saved)
	}
}
