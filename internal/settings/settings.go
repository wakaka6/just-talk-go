// Copyright (C) 2026 just-talk-go contributors
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk.

// Package settings holds the editable-settings catalog shared by the TUI and
// the GUI: selectable ASR engines, their per-engine fields, and the rules a
// voice hotkey must pass before it is saved.
package settings

import (
	"fmt"
	"slices"
	"strings"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
)

// DefaultEngine is used when voice.engine is empty.
const DefaultEngine = "doubao-stream"

// Engine describes one selectable engine.
// Order in Engines is the picker order: streaming engines first.
type Engine struct {
	Name      string   `json:"name"`      // registry type
	Title     string   `json:"title"`     // display name
	Blurb     string   `json:"blurb"`     // one-line pitch shown under the name
	Doc       string   `json:"doc"`       // documentation URL
	Streaming bool     `json:"streaming"` // live partials → real-time captions
	Required  []string `json:"-"`         // config keys that make the engine usable
}

var Engines = []Engine{
	{
		Name:      "doubao-stream",
		Title:     "豆包语音",
		Blurb:     "豆包大模型双向流式 ASR，边说边出字 + 二遍校正，支持热词",
		Doc:       "https://www.volcengine.com/docs/6561/1395867",
		Streaming: true,
		// Available() accepts either api_key (new console) or app_id+access_key
		// (legacy); checked specially in EngineConfigured.
	},
	{
		Name:      "dashscope",
		Title:     "千问 Fun-ASR",
		Blurb:     "阿里千问 Fun-ASR 实时流式，边录边出字；8k 模型自动降采样",
		Doc:       "https://www.alibabacloud.com/help/en/model-studio/fun-asr-realtime-websocket-api",
		Streaming: true,
		Required:  []string{"api_key"},
	},
	{
		Name:     "openai",
		Title:    "OpenAI Whisper",
		Blurb:    "OpenAI 兼容 Whisper 接口，整段识别；可指向本地/第三方端点",
		Doc:      "https://platform.openai.com/docs/api-reference/audio",
		Required: []string{"api_key"},
	},
	{
		Name:     "auralwise",
		Title:    "AuralWise",
		Blurb:    "AuralWise 异步任务转写，整段识别后上屏",
		Doc:      "https://auralwise.cn/",
		Required: []string{"api_key"},
	},
	{
		Name:     "mimo-asr",
		Title:    "小米 MiMo",
		Blurb:    "小米 MiMo 语音识别，整段识别",
		Doc:      "https://platform.xiaomimimo.com/",
		Required: []string{"api_key"},
	},
}

// EngineFor returns the catalog entry, or a generic entry for custom engines.
func EngineFor(name string) Engine {
	for _, e := range Engines {
		if e.Name == name {
			return e
		}
	}
	return Engine{Name: name, Title: name, Blurb: "自定义引擎"}
}

// CurrentEngine returns the configured engine, defaulting to DefaultEngine.
func CurrentEngine(vc config.VoiceConfig) string {
	if engine := strings.TrimSpace(vc.Engine); engine != "" {
		return engine
	}
	return DefaultEngine
}

// Field declares one per-engine setting stored under engine_configs[engine].
// A field with Options is a select; otherwise it is free text.
type Field struct {
	Label    string   `json:"label"`
	Key      string   `json:"key"`
	Help     string   `json:"help"`
	Options  []string `json:"options,omitempty"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"` // mask when not editing
	// Advanced fields have working defaults; UIs may fold them away.
	Advanced bool `json:"advanced"`
}

func EngineFields(engine string) []Field {
	switch engine {
	case "doubao-stream":
		return []Field{
			{Label: "API Key", Key: "api_key", Help: "新版控制台 X-Api-Key；填了就忽略下方旧版凭据", Secret: true},
			{Label: "App ID", Key: "app_id", Help: "旧版控制台 App ID（X-Api-App-Key）", Advanced: true},
			{Label: "Access Key", Key: "access_key", Help: "旧版 Access Token（X-Api-Access-Key）", Required: true, Secret: true, Advanced: true},
			{Label: "模型版本", Key: "model_version", Help: "1.0 = bigasr（旧资源），2.0 = seedasr（新控制台）", Options: []string{"1.0", "2.0"}},
			{Label: "流式计费", Key: "stream_billing", Help: "duration 按时长 / concurrent 按并发，对照套餐选择", Options: []string{"duration", "concurrent"}, Advanced: true},
			{Label: "Resource ID", Key: "resource_id", Help: "留空则按模型版本+计费推导；除非控制台另有资源 ID 一般不填", Advanced: true},
			{Label: "中英混识别", Key: "enable_lid", Help: "中英混说/方言时开启", Options: []string{"false", "true"}, Advanced: true},
			{Label: "Base Host", Key: "base_host", Help: "默认 openspeech.bytedance.com；仅自建代理/网关时改", Advanced: true},
		}
	case "dashscope":
		return []Field{
			{Label: "API Key", Key: "api_key", Help: "必填，DashScope 控制台创建（sk-…）", Required: true, Secret: true},
			{Label: "模型", Key: "model", Help: "默认 fun-asr-realtime；8k 型号（名字含 8k）自动降到 8000 Hz", Advanced: true},
			{Label: "采样率", Key: "sample_rate", Help: "16000 通用；8000 仅 8k 模型", Options: []string{"16000", "8000"}, Advanced: true},
		}
	case "openai":
		return []Field{
			{Label: "API Key", Key: "api_key", Help: "必填，Bearer token", Required: true, Secret: true},
			{Label: "Base URL", Key: "base_url", Help: "默认 https://api.openai.com/v1，可指本地 whisper.cpp server", Advanced: true},
			{Label: "模型", Key: "model", Help: "默认 whisper-1", Advanced: true},
		}
	case "auralwise":
		return []Field{
			{Label: "API Key", Key: "api_key", Help: "必填，auralwise.cn 账号设置里创建（asr_…）", Required: true, Secret: true},
			{Label: "Base URL", Key: "base_url", Help: "默认 https://api.auralwise.cn/v1", Advanced: true},
			{Label: "Optimize", Key: "optimize", Help: "auto 按语言自动 / true 省成本 / false 高质量词级时间戳", Options: []string{"auto", "true", "false"}, Advanced: true},
		}
	case "mimo-asr":
		return []Field{
			{Label: "API Key", Key: "api_key", Help: "必填，小米 MiMo 控制台", Required: true, Secret: true},
			{Label: "Base URL", Key: "base_url", Help: "默认 https://api.xiaomimimo.com/v1", Advanced: true},
			{Label: "模型", Key: "model", Help: "默认 mimo-v2.5-asr", Advanced: true},
		}
	}
	return nil
}

// EngineConfigured reports whether every required key of the engine is filled.
// doubao legacy flat fields count as fallbacks, matching asrcfg.ConfigMap.
func EngineConfigured(vc config.VoiceConfig, engine string) bool {
	ec := vc.EngineConfigFor(engine)
	if engine == "doubao-stream" {
		if EngineValue(vc, engine, ec, "api_key") != "" {
			return true
		}
		return EngineValue(vc, engine, ec, "app_id") != "" &&
			EngineValue(vc, engine, ec, "access_key") != ""
	}
	for _, k := range EngineFor(engine).Required {
		if EngineValue(vc, engine, ec, k) == "" {
			return false
		}
	}
	return true
}

// EngineValue resolves a config key: engine_configs value first, then
// doubao legacy flat fields (so existing installs keep working in the UI).
func EngineValue(vc config.VoiceConfig, engine string, ec map[string]string, key string) string {
	if v := strings.TrimSpace(ec[key]); v != "" {
		return v
	}
	if engine != "doubao-stream" {
		return ""
	}
	switch key {
	case "app_id":
		return vc.AppKey
	case "access_key":
		return vc.AccessKey
	case "resource_id":
		return vc.ResourceID
	}
	return ""
}

// ValidateHotkeys checks a voice push_to_talk value. The error text is
// user-facing.
func ValidateHotkeys(value string) error {
	combos, err := config.ParseHotkeyList(value)
	if err != nil {
		return fmt.Errorf("热键格式错误: %w", err)
	}
	for _, combo := range combos {
		if combo.Mods&hotkey.ModFn != 0 && combo.Key.IsFunctionKey() {
			return fmt.Errorf("Fn 不能与 F1-F24 等功能键组合: %s", combo)
		}
		if combo.Key.IsTextKey() && combo.Mods&hotkey.ModFn == 0 {
			return fmt.Errorf("热键不支持普通字符键: %s，请使用 Alt+Super、F9、Alt+F8、Ctrl+Alt+Tab 等全局快捷键", combo)
		}
	}
	return nil
}

// ValidateContinuousHotkey checks [voice.continuous] hotkey: the same rules as
// a dictation hotkey, and no combo may already be one of pushToTalk's. The
// voice plugin refuses such a config, so saving it would leave Just Talk
// unable to start its hotkeys next time. The error text is user-facing.
func ValidateContinuousHotkey(value, pushToTalk string) error {
	if err := ValidateHotkeys(value); err != nil {
		return err
	}
	combos, _ := config.ParseHotkeyList(value)
	dictation, err := config.ParseHotkeyList(pushToTalk)
	if err != nil {
		return fmt.Errorf("热键格式错误: %w", err)
	}
	for _, combo := range combos {
		if slices.Contains(dictation, combo) {
			return fmt.Errorf("%s 已经是听写热键，不能再用来开关持续转写", combo)
		}
	}
	return nil
}

// SplitList splits a comma/newline separated list, trimming and dropping
// empty and duplicate items. Full-width commas are accepted.
func SplitList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

var OverlayPositions = []string{"notch", "top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}

// Language is a selectable voice.language value. Every engine takes a
// BCP-47 tag and maps it to its own form (zh-CN → zh); empty lets the
// engine detect the language.
type Language struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

var Languages = []Language{
	{"", "自动检测"},
	{"zh-CN", "简体中文"},
	{"en-US", "English"},
	{"ja-JP", "日本語"},
	{"ko-KR", "한국어"},
	{"fr-FR", "Français"},
	{"de-DE", "Deutsch"},
	{"es-ES", "Español"},
	{"ru-RU", "Русский"},
}

// LanguageOptions keeps a configured language outside the list selectable,
// so saving from a UI never rewrites it silently.
func LanguageOptions(current string) []Language {
	for _, l := range Languages {
		if l.Code == current {
			return Languages
		}
	}
	return append(append([]Language(nil), Languages...), Language{current, current})
}

// OverlayPositionOptions keeps a configured value outside the known list
// selectable, so saving from a UI never rewrites it silently.
func OverlayPositionOptions(current string) []string {
	for _, p := range OverlayPositions {
		if p == current {
			return OverlayPositions
		}
	}
	return append(append([]string(nil), OverlayPositions...), current)
}
