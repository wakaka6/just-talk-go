package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/c/just-talk-go/config"
)

// isolate runs the test in a temp dir holding ./config.toml, which
// config.Save prefers, with HOME and XDG_CONFIG_HOME pointing there too so
// no save can reach the real user config.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.WriteFile("config.toml", []byte("[voice]\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func postSettings(t *testing.T, h http.Handler, body settingsJSON) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(data)))
	return rec
}

func validSettings(cfg *config.Config) settingsJSON {
	return settingsJSON{
		Enabled:         cfg.Voice.Enabled,
		PushToTalk:      cfg.Voice.PushToTalk,
		Mode:            cfg.Voice.Mode,
		Language:        cfg.Voice.Language,
		OverlayPosition: cfg.Overlay.Position,
		Engine:          "openai",
	}
}

func TestSaveWritesEngineEditsAndReloads(t *testing.T) {
	isolate(t)
	cur := config.Default()
	cur.Voice.EngineConfigs = map[string]map[string]string{"openai": {"api_key": "old", "model": "whisper-1"}}
	var applied *config.Config
	h := Handler(Options{Config: cur, Apply: func(c *config.Config) error { applied = c; return nil }})

	in := validSettings(cur)
	in.PushToTalk = "F9, Alt+Super"
	in.Hotwords = []string{"Wails", " Just Talk ", "Wails", ""}
	in.EngineConfigs = map[string]map[string]string{"openai": {"api_key": " new ", "model": ""}}
	if rec := postSettings(t, h, in); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	if cur.Voice.EngineConfigs["openai"]["api_key"] != "old" || cur.Voice.PushToTalk != config.Default().Voice.PushToTalk {
		t.Fatal("saving modified the config the page started from")
	}
	if applied == nil {
		t.Fatal("saving did not reload the engine")
	}
	saved, err := config.Load("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	ec := saved.Voice.EngineConfigs["openai"]
	if ec["api_key"] != "new" {
		t.Fatalf("api_key = %q, want trimmed new value", ec["api_key"])
	}
	if _, ok := ec["model"]; ok {
		t.Fatal("a cleared field stayed in engine_configs")
	}
	if saved.Voice.Engine != "openai" || saved.Voice.PushToTalk != "F9, Alt+Super" {
		t.Fatalf("saved engine/hotkey = %q/%q", saved.Voice.Engine, saved.Voice.PushToTalk)
	}
	if got := saved.Voice.Hotwords; len(got) != 2 || got[0] != "Wails" || got[1] != "Just Talk" {
		t.Fatalf("hotwords = %q", got)
	}
}

func TestSaveRejectsInvalidValuesWithoutWriting(t *testing.T) {
	cases := map[string]func(*settingsJSON){
		"text key hotkey": func(s *settingsJSON) { s.PushToTalk = "Ctrl+A" },
		"fn function key": func(s *settingsJSON) { s.PushToTalk = "Fn+F5" },
		"unknown mode":    func(s *settingsJSON) { s.Mode = "" },
		"negative delay":  func(s *settingsJSON) { s.StopDelayMs = -1 },
		"unknown position": func(s *settingsJSON) {
			s.OverlayPosition = "middle"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			isolate(t)
			cur := config.Default()
			applied := false
			h := Handler(Options{Config: cur, Apply: func(*config.Config) error { applied = true; return nil }})
			in := validSettings(cur)
			mutate(&in)
			rec := postSettings(t, h, in)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			var body map[string]string
			if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] == "" {
				t.Fatalf("no error message in %s", rec.Body)
			}
			if data, _ := os.ReadFile("config.toml"); string(data) != "[voice]\n" || applied {
				t.Fatal("an invalid save reached the config file or the engine")
			}
		})
	}
}

// An engine the page did not edit must keep reading the legacy flat
// voice.engine_config; writing an empty per-engine table would hide it.
func TestSaveLeavesUneditedEnginesOnLegacyConfig(t *testing.T) {
	isolate(t)
	cur := config.Default()
	cur.Voice.EngineConfig = map[string]string{"api_key": "legacy"}
	h := Handler(Options{Config: cur})
	if rec := postSettings(t, h, validSettings(cur)); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	saved, err := config.Load("config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Voice.EngineConfigFor("openai")["api_key"]; got != "legacy" {
		t.Fatalf("openai api_key = %q, want the legacy value", got)
	}
}

func TestOpenOnlyCatalogLinks(t *testing.T) {
	var opened []string
	h := Handler(Options{Config: config.Default(), OpenURL: func(u string) { opened = append(opened, u) }})
	for url, want := range map[string]int{
		"https://platform.openai.com/docs/api-reference/audio": http.StatusNoContent,
		"file:///etc/passwd": http.StatusForbidden,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/open", bytes.NewReader([]byte(`{"url":"`+url+`"}`))))
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", url, rec.Code, want)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("opened %q", opened)
	}
}

func TestWindowActionsOnlyInApp(t *testing.T) {
	post := func(h http.Handler, action string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/window", bytes.NewReader([]byte(`{"action":"`+action+`"}`))))
		return rec.Code
	}
	// Browser mode has no window to show and must not quit the app.
	web := Handler(Options{Config: config.Default()})
	for _, action := range []string{"settings", "quit"} {
		if code := post(web, action); code != http.StatusNotFound {
			t.Errorf("web %s: status %d, want 404", action, code)
		}
	}

	var calls []string
	app := Handler(Options{
		Config:       config.Default(),
		ShowSettings: func() { calls = append(calls, "settings") },
		Quit:         func() { calls = append(calls, "quit") },
	})
	for action, want := range map[string]int{"settings": http.StatusNoContent, "quit": http.StatusNoContent, "reboot": http.StatusNotFound} {
		if code := post(app, action); code != want {
			t.Errorf("app %s: status %d, want %d", action, code, want)
		}
	}
	slices.Sort(calls)
	if !slices.Equal(calls, []string{"quit", "settings"}) {
		t.Fatalf("calls %q", calls)
	}
}
