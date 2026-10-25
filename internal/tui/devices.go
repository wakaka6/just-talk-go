// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

package tui

import (
	"slices"
	"strings"

	"github.com/c/just-talk-go/plugins/voice"
)

// deviceHelp explains the one distinction that decides which device to pick.
const deviceHelp = "麦克风录外部声音；回环设备录本机播放的声音（会议、视频），需先把播放输出指向它"

// deviceSuffix labels what a device records. The select shows one line at a
// time, so the kind goes on the entry itself instead of a group header.
var deviceSuffix = map[string]string{
	voice.DeviceMic:      " · 麦克风",
	voice.DeviceLoopback: " · 回环",
}

// deviceOptions builds the recording-device select: labels to show, the config
// value behind each one ("" = system default), and the index of current.
// Devices are ordered microphones first, then loopback, so the two kinds do
// not interleave while cycling.
func deviceOptions(devices []voice.InputDevice, current string) (labels, values []string, idx int) {
	if strings.EqualFold(current, "default") {
		current = "" // legacy spelling of the system default
	}
	labels, values = []string{"系统默认输入"}, []string{""}
	for _, kind := range []string{voice.DeviceMic, voice.DeviceLoopback, voice.DeviceUnknown} {
		for _, d := range devices {
			if d.Kind == kind {
				labels = append(labels, d.Name+deviceSuffix[kind])
				values = append(values, d.Name)
			}
		}
	}
	// Keep a configured device that is not connected right now: saving must
	// not silently switch the recording source.
	if current != "" && !slices.Contains(values, current) {
		labels = append(labels, current+"（未检测到）")
		values = append(values, current)
	}
	return labels, values, idxOf(values, current)
}

// setDevices refreshes the device field once the device list arrives.
func (m *Model) setDevices(devices []voice.InputDevice) {
	m.devices = devices
	for i := range m.fields {
		if m.fields[i].key != "device" {
			continue
		}
		f := &m.fields[i]
		f.opts, f.optVals, f.optIdx = deviceOptions(devices, f.selected())
	}
}
