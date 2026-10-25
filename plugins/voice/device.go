// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package voice

// Input device kinds. Recording your own voice and recording what the computer
// plays are different jobs, and on most systems they are different devices:
// the user has to be able to tell them apart before picking one.
const (
	// DeviceMic is a real input: microphone, audio interface, headset. It
	// captures sound from outside the computer.
	DeviceMic = "mic"
	// DeviceLoopback is a virtual cable that carries what the computer plays
	// back into an input: meeting audio, video, system sound. It only records
	// the applications whose output is routed into it.
	//
	// macOS has no built-in way to capture system audio, so a loopback device
	// comes from a driver such as BlackHole or Loopback.
	DeviceLoopback = "loopback"
	// DeviceUnknown is anything the platform cannot classify.
	DeviceUnknown = "unknown"
)

// InputDevice is one device Just Talk can record from.
type InputDevice struct {
	Name string `json:"name"`
	// UID is a stable platform identifier; empty when the platform has none.
	UID  string `json:"uid,omitempty"`
	Kind string `json:"kind"`
}

// DeviceNames returns the device names in order.
func DeviceNames(devices []InputDevice) []string {
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name)
	}
	return names
}
