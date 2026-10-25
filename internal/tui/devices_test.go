package tui

import (
	"slices"
	"testing"

	"github.com/c/just-talk-go/plugins/voice"
)

var testDevices = []voice.InputDevice{
	{Name: "BlackHole 2ch", UID: "BlackHole2ch_UID", Kind: voice.DeviceLoopback},
	{Name: "MacBook Pro麦克风", UID: "BuiltInMicrophoneDevice", Kind: voice.DeviceMic},
	{Name: "pulse", Kind: voice.DeviceUnknown},
}

func TestDeviceOptionsGroupsAndSelects(t *testing.T) {
	labels, values, idx := deviceOptions(testDevices, "BlackHole 2ch")
	wantLabels := []string{"系统默认输入", "MacBook Pro麦克风 · 麦克风", "BlackHole 2ch · 回环", "pulse"}
	if !slices.Equal(labels, wantLabels) {
		t.Errorf("labels = %q, want %q", labels, wantLabels)
	}
	wantValues := []string{"", "MacBook Pro麦克风", "BlackHole 2ch", "pulse"}
	if !slices.Equal(values, wantValues) {
		t.Errorf("values = %q, want %q", values, wantValues)
	}
	if values[idx] != "BlackHole 2ch" {
		t.Errorf("selected %q, want BlackHole 2ch", values[idx])
	}
}

func TestDeviceOptionsSelectsSystemDefault(t *testing.T) {
	for _, current := range []string{"", "default", "Default"} {
		_, values, idx := deviceOptions(testDevices, current)
		if values[idx] != "" {
			t.Errorf("device %q selected %q, want the system default", current, values[idx])
		}
	}
}

func TestDeviceOptionsKeepsDisconnectedDevice(t *testing.T) {
	labels, values, idx := deviceOptions(testDevices, "USB Mic")
	if values[idx] != "USB Mic" {
		t.Fatalf("selected %q, want the configured device to survive", values[idx])
	}
	if want := "USB Mic（未检测到）"; labels[idx] != want {
		t.Errorf("label = %q, want %q", labels[idx], want)
	}
}

func TestSetDevicesKeepsSelection(t *testing.T) {
	m := &Model{fields: []field{{key: "device", opts: []string{"系统默认输入", "BlackHole 2ch（未检测到）"},
		optVals: []string{"", "BlackHole 2ch"}, optIdx: 1}}}
	m.setDevices(testDevices)
	f := m.fields[0]
	if f.selected() != "BlackHole 2ch" {
		t.Fatalf("selected %q after the device list arrived", f.selected())
	}
	if f.opts[f.optIdx] != "BlackHole 2ch · 回环" {
		t.Errorf("label = %q, want the connected device", f.opts[f.optIdx])
	}
}
