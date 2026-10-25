//go:build darwin && cgo

package voice

import "testing"

func TestMatchInputDevice(t *testing.T) {
	devices := []inputDevice{
		{UID: "BuiltInMicrophoneDevice", Name: "MacBook Pro Microphone"},
		{UID: "BlackHole2ch_UID", Name: "BlackHole 2ch"},
	}
	cases := []struct {
		want, uid string
		ok        bool
	}{
		{"BlackHole2ch_UID", "BlackHole2ch_UID", true},
		{"BlackHole 2ch", "BlackHole2ch_UID", true},
		{"blackhole 2CH", "BlackHole2ch_UID", true},
		{"blackhole2ch_uid", "", false},
		{"BlackHole 16ch", "", false},
	}
	for _, c := range cases {
		d, ok := matchInputDevice(devices, c.want)
		if ok != c.ok || d.UID != c.uid {
			t.Errorf("matchInputDevice(%q) = %+v, %v; want uid %q, %v", c.want, d, ok, c.uid, c.ok)
		}
	}
}
