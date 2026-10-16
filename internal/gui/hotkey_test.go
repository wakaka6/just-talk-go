package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c/just-talk-go/config"
)

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body))))
	return rec
}

func TestCheckHotkeyReturnsConfigName(t *testing.T) {
	h := Handler(Options{Config: config.Default()})
	for in, want := range map[string]string{
		"Alt+Super": "Alt+Super",
		"Super+Alt": "Alt+Super",
		"Fn":        "Fn",
		"Ctrl+Up":   "Ctrl+Up",
		"F13":       "F13",
	} {
		rec := post(t, h, "/api/hotkey/check", `{"value":"`+in+`"}`)
		var got map[string]string
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["value"] != want {
			t.Errorf("%s: status %d body %s, want %s", in, rec.Code, rec.Body, want)
		}
	}
	for _, in := range []string{"Ctrl+A", "Fn+F5", "Shift+Space", "Hyper"} {
		if rec := post(t, h, "/api/hotkey/check", `{"value":"`+in+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", in, rec.Code)
		}
	}
}

// While the page records a hotkey the voice hotkeys must be off, and they
// must come back when recording ends or a save lands.
func TestCaptureSuspendsAndRestoresHotkeys(t *testing.T) {
	isolate(t)
	cur := config.Default()
	var applied []bool
	h := Handler(Options{Config: cur, Apply: func(c *config.Config) error {
		applied = append(applied, c.Voice.Enabled)
		return nil
	}})

	post(t, h, "/api/hotkey/capture", `{"active":true}`)
	post(t, h, "/api/hotkey/capture", `{"active":true}`) // keepalive
	post(t, h, "/api/hotkey/capture", `{"active":false}`)
	post(t, h, "/api/hotkey/capture", `{"active":false}`)
	if len(applied) != 2 || applied[0] || !applied[1] {
		t.Fatalf("reloads during record/stop = %v, want [false true]", applied)
	}

	applied = nil
	post(t, h, "/api/hotkey/capture", `{"active":true}`)
	if rec := postSettings(t, h, validSettings(cur)); rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}
	post(t, h, "/api/hotkey/capture", `{"active":false}`)
	if len(applied) != 2 || applied[0] || !applied[1] {
		t.Fatalf("reloads during record/save/stop = %v, want [false true]", applied)
	}
}

func TestCaptureLeavesDisabledVoiceAlone(t *testing.T) {
	cur := config.Default()
	cur.Voice.Enabled = false
	reloads := 0
	h := Handler(Options{Config: cur, Apply: func(*config.Config) error { reloads++; return nil }})
	post(t, h, "/api/hotkey/capture", `{"active":true}`)
	post(t, h, "/api/hotkey/capture", `{"active":false}`)
	if reloads != 0 {
		t.Fatalf("reloaded %d times with voice input off", reloads)
	}
}
