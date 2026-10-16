// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

// Engine picker + per-engine config modals for the TUI.
//
// Design: single flat main form stays untouched; pressing enter on the
// "引擎"/"引擎配置" action fields opens a centered modal overlay rendered on
// top of the normal view. Engine credentials live under
// voice.engine_configs[engine] so switching engines never clobbers keys.

package tui

import (
	"strings"

	"github.com/c/just-talk-go/internal/settings"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"
)

type overlayState int

const (
	overlayNone overlayState = iota
	overlayPicker
	overlayEngineConfig
)

// ---- modal open/close ----

func (m *Model) openEnginePicker() {
	m.overlay = overlayPicker
	m.pickerIdx = 0
	current := m.currentEngine()
	for i, meta := range settings.Engines {
		if meta.Name == current {
			m.pickerIdx = i
			break
		}
	}
}

func (m *Model) openEngineConfig() {
	engine := m.currentEngine()
	vc := m.cfg.Voice
	ec := vc.EngineConfigFor(engine)
	ti := func(v string) textinput.Model {
		t := textinput.New()
		t.SetValue(v)
		t.Cursor.Blink = false
		return t
	}
	specs := settings.EngineFields(engine)
	fs := make([]field, 0, len(specs))
	for _, spec := range specs {
		f := field{label: spec.Label, key: spec.Key, help: spec.Help, fType: fString, required: spec.Required, secret: spec.Secret}
		v := settings.EngineValue(vc, engine, ec, spec.Key)
		if len(spec.Options) > 0 {
			f.fType = fSelect
			f.opts = spec.Options
			f.optIdx = idxOf(spec.Options, v)
		} else {
			f.input = ti(v)
		}
		fs = append(fs, f)
	}
	m.ecFields = fs
	m.ecCursor = 0
	m.ecEditing = false
	m.overlay = overlayEngineConfig
}

func (m *Model) currentEngine() string {
	return settings.CurrentEngine(m.cfg.Voice)
}

func (m *Model) closeOverlay() {
	m.overlay = overlayNone
	m.ecEditing = false
	for i := range m.ecFields {
		m.ecFields[i].input.Blur()
	}
}

// ---- modal key handling ----

func (m *Model) handleOverlayKey(msg tea.KeyMsg) bool {
	switch m.overlay {
	case overlayPicker:
		m.handlePickerKey(msg.String())
		return true
	case overlayEngineConfig:
		m.handleConfigKey(msg)
		return true
	}
	return false
}

func (m *Model) handlePickerKey(k string) {
	switch k {
	case "esc", "q":
		m.closeOverlay()
	case "j", "down":
		m.pickerIdx++
		if m.pickerIdx >= len(settings.Engines) {
			m.pickerIdx = 0
		}
	case "k", "up":
		m.pickerIdx--
		if m.pickerIdx < 0 {
			m.pickerIdx = len(settings.Engines) - 1
		}
	case "o":
		openURL(settings.Engines[m.pickerIdx].Doc)
	case "enter", "e", "i":
		m.cfg.Voice.Engine = settings.Engines[m.pickerIdx].Name
		m.openEngineConfig()
	}
}

func (m *Model) handleConfigKey(msg tea.KeyMsg) {
	k := msg.String()
	if m.ecEditing && m.ecCursor >= 0 && m.ecCursor < len(m.ecFields) {
		f := &m.ecFields[m.ecCursor]
		switch k {
		case "esc":
			m.ecEditing = false
			f.input.Blur()
			return
		case "enter":
			m.ecEditing = false
			f.input.Blur()
			m.commitEngineConfig(f)
			return
		}
		switch f.fType {
		case fSelect:
			switch k {
			case "j", "down":
				f.optIdx++
				if f.optIdx >= len(f.opts) {
					f.optIdx = 0
				}
			case "k", "up":
				f.optIdx--
				if f.optIdx < 0 {
					f.optIdx = len(f.opts) - 1
				}
			}
		case fString:
			f.input, _ = f.input.Update(msg)
			return
		}
		return
	}

	switch k {
	case "esc", "q":
		m.closeOverlay()
	case "o":
		openURL(settings.EngineFor(m.currentEngine()).Doc)
	case "s":
		m.save()
	case "j", "down":
		m.ecCursor++
		if m.ecCursor >= len(m.ecFields) {
			m.ecCursor = 0
		}
	case "k", "up":
		m.ecCursor--
		if m.ecCursor < 0 {
			m.ecCursor = len(m.ecFields) - 1
		}
	case "e", "i", "enter":
		if m.ecCursor >= 0 && m.ecCursor < len(m.ecFields) {
			m.ecEditing = true
			f := &m.ecFields[m.ecCursor]
			if f.fType == fString {
				f.input.Focus()
			}
		}
	}
}

// commitEngineConfig writes one edited field into cfg.Voice.EngineConfigs.
func (m *Model) commitEngineConfig(f *field) {
	engine := m.currentEngine()
	ec := m.cfg.Voice.EngineConfigFor(engine)
	if ec == nil {
		ec = map[string]string{}
	}
	switch f.fType {
	case fSelect:
		v := f.opts[f.optIdx]
		if v == "" {
			delete(ec, f.key)
		} else {
			ec[f.key] = v
		}
	default:
		v := strings.TrimSpace(f.input.Value())
		if v == "" {
			delete(ec, f.key)
		} else {
			ec[f.key] = v
		}
	}
	m.cfg.Voice.SetEngineConfig(engine, ec)
}

// ---- modal rendering ----

var (
	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")).
			MaxWidth(84).
			Padding(0, 1)
	modalTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

func (m *Model) renderPicker() string {
	var b strings.Builder
	b.WriteString(modalTitle.Render("选择识别引擎") + "\n")
	b.WriteString(dStyle.Render("回车选定并进入引擎配置，o 打开文档，esc 返回") + "\n\n")
	current := m.currentEngine()
	for i, meta := range settings.Engines {
		marker := "  "
		if i == m.pickerIdx {
			marker = aStyle.Render("▸ ")
		}
		chosen := "  "
		if meta.Name == current {
			chosen = aStyle.Render("● ")
		}
		var tag string
		if meta.Streaming {
			tag = aStyle.Render("[流式]")
		} else {
			tag = dStyle.Render("[整段]")
		}
		status := dStyle.Render("未配置")
		if settings.EngineConfigured(m.cfg.Voice, meta.Name) {
			status = aStyle.Render("已配置")
		}
		b.WriteString(marker + chosen + vStyle.Render(meta.Name) + " " + tag + " " + status + "\n")
		b.WriteString("      " + dStyle.Render(meta.Blurb) + "\n")
	}
	return modalStyle.Render(b.String())
}

func (m *Model) renderEngineConfig() string {
	engine := m.currentEngine()
	meta := settings.EngineFor(engine)
	var b strings.Builder
	b.WriteString(modalTitle.Render("引擎配置 · "+meta.Name) + "  ")
	if meta.Streaming {
		b.WriteString(aStyle.Render("[流式]"))
	} else {
		b.WriteString(dStyle.Render("[整段]"))
	}
	b.WriteString("\n" + dStyle.Render(meta.Blurb) + "\n\n")
	for i, f := range m.ecFields {
		marker := "  "
		if i == m.ecCursor {
			if m.ecEditing {
				marker = aStyle.Render("▶ ")
			} else {
				marker = aStyle.Render("▸ ")
			}
		}
		label := f.label
		if f.required {
			label += "*"
		}
		line := marker + lStyle.Render(label+": ") + m.renderECField(i, f)
		if f.help != "" {
			line += " " + dStyle.Render("("+f.help+")")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + hStyle.Render("  j/k 导航 | e 编辑 | 回车提交 | s 保存 | o 文档 | esc 返回"))
	return modalStyle.Render(squeezeWidth(b.String(), 72))
}

// squeezeWidth truncates every line of s to maxCells display cells so the
// surrounding border never exceeds the modal cap. ANSI-aware via cellSlice.
func squeezeWidth(s string, maxCells int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > maxCells {
			lines[i] = cellSlice(l, 0, maxCells)
		}
	}
	return strings.Join(lines, "\n")
}

// renderECField renders one engine-config field; secrets mask when not editing.
func (m *Model) renderECField(i int, f field) string {
	editing := m.ecEditing && m.ecCursor == i
	switch f.fType {
	case fString:
		v := f.input.Value()
		if f.secret && !editing && len(v) > 8 {
			v = v[:8] + "***"
		}
		if editing {
			return f.input.View()
		}
		if v == "" {
			return dStyle.Render("(空)")
		}
		return vStyle.Render(v)
	case fSelect:
		v := f.opts[f.optIdx]
		if v == "" {
			v = "(默认)"
		}
		if editing {
			return aStyle.Render("[" + v + " ▲▼]")
		}
		return vStyle.Render(v)
	}
	return ""
}

// ---- compositing ----

// compositeOver draws box centered on base, replacing those cells.
// ANSI-aware: cellSlice walks escape sequences without counting them.
func compositeOver(base, box string, w, h int) string {
	boxLines := strings.Split(box, "\n")
	const maxBoxW = 80
	for i, l := range boxLines {
		if lipgloss.Width(l) > maxBoxW {
			boxLines[i] = cellSlice(l, 0, maxBoxW)
		}
	}
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < h {
		baseLines = append(baseLines, "")
	}
	boxW := 0
	for _, l := range boxLines {
		if lw := lipgloss.Width(l); lw > boxW {
			boxW = lw
		}
	}
	boxH := len(boxLines)
	if boxW >= w || boxH >= h {
		return box // terminal smaller than modal: just show modal
	}
	top := (h - boxH) / 2
	left := (w - boxW) / 2
	for i, l := range boxLines {
		row := top + i
		lw := lipgloss.Width(l)
		prefix := cellSlice(baseLines[row], 0, left)
		if lp := lipgloss.Width(prefix); lp < left {
			prefix += strings.Repeat(" ", left-lp)
		}
		baseLines[row] = prefix + l + cellSlice(baseLines[row], left+lw, w)
	}
	return strings.Join(baseLines, "\n")
}

// cellSlice returns the substring of s covering display cells [start, end).
// ANSI escape sequences pass through without consuming cells; a trailing
// reset is appended so the following text is not contaminated by open styles.
func cellSlice(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	var state int // 0 normal, 1 esc, 2 csi, 3 osc
	col := 0
	inRange := false
	for _, r := range s {
		switch state {
		case 0:
			if r == 0x1b {
				state = 1
				if inRange {
					b.WriteRune(r)
				}
				continue
			}
			w := uniseg.StringWidth(string(r))
			if col+w > start && col < end {
				inRange = true
			}
			if inRange && col < end {
				b.WriteRune(r)
			}
			col += w
			if col >= end {
				inRange = false
			}
		case 1:
			if inRange {
				b.WriteRune(r)
			}
			switch r {
			case '[':
				state = 2
			case ']':
				state = 3
			default:
				state = 0
			}
		case 2: // CSI: ends on a letter
			if inRange {
				b.WriteRune(r)
			}
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				state = 0
			}
		case 3: // OSC: ends on BEL or ESC-backslash
			if inRange {
				b.WriteRune(r)
			}
			if r == 0x07 {
				state = 0
			}
			// ESC-terminated OSC is rare in TUI output; BEL suffices here.
		}
	}
	if b.Len() > 0 {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
