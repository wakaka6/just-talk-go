package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/c/just-talk-go/hotkey"
)

type Config struct {
	Voice   VoiceConfig   `toml:"voice"`
	Debug   DebugConfig   `toml:"debug"`
	Overlay OverlayConfig `toml:"overlay"`
}

type DebugConfig struct {
	Enabled bool     `toml:"enabled"`
	Hotkeys []string `toml:"hotkeys"`
}

type OverlayConfig struct {
	Enabled     bool    `toml:"enabled"`
	Position    string  `toml:"position"`
	IdleVisible bool    `toml:"idle_visible"`
	ShowText    bool    `toml:"show_text"`
	Scale       float64 `toml:"scale"`
}

type VoiceConfig struct {
	Enabled     bool   `toml:"enabled"`
	Mode        string `toml:"mode"`
	PushToTalk  string `toml:"push_to_talk"`
	Device      string `toml:"device"`
	Gain        int    `toml:"gain"`
	StopDelayMs int    `toml:"stop_delay_ms"`
	Language    string `toml:"language"`
	// TextOutput 识别结果送到哪里：OutputPaste / OutputClipboard / OutputNone。
	TextOutput string `toml:"text_output"`
	// AutoSubmit 旧版开关，仅用于迁移到 TextOutput；迁移后置空，存盘时不再写出。
	AutoSubmit *bool `toml:"auto_submit,omitempty"`
	// StreamAddr 非空时在该回环地址开启 WebSocket 订阅服务（路径 /asr），
	// 向订阅者推送会话事件与每条识别结果；空 = 关闭。
	StreamAddr string `toml:"stream_addr"`
	// Engine 语音引擎类型；留空时使用默认引擎。
	Engine string `toml:"engine"`
	// EngineConfigs 按引擎类型分组保存私有配置。
	EngineConfigs map[string]map[string]string `toml:"engine_configs"`
	// EngineConfig 旧版平铺引擎配置，仅作为读取回退。
	EngineConfig map[string]string `toml:"engine_config"`
	AppKey       string            `toml:"app_key"`
	AccessKey    string            `toml:"access_key"`
	ResourceID   string            `toml:"resource_id"`
	Hotwords     []string          `toml:"hotwords"`
	// HotwordsFile 外部词表：一行一个词，# 开头为注释，与 Hotwords 合并。
	// 每次会话开始时重读，所以外部程序改完文件下一句就生效。
	HotwordsFile string `toml:"hotwords_file"`
	// Continuous 持续转写：与听写并行的另一路，转的是外部音源。
	Continuous ContinuousConfig `toml:"continuous"`
}

// ContinuousConfig 是持续转写那一路的配置。它和听写同时跑：你按热键说话上屏的
// 同时，它把会议等外部音源转给订阅者，互不影响——所以它有自己的录音设备。
type ContinuousConfig struct {
	Enabled bool `toml:"enabled"`
	// Hotkey 开关这一路的热键，留空 = 不注册。听写的热键划的是一句话的
	// 边界，这个划的是整条服务的开关，所以只有「按一下切换」——没人会按着
	// 一个键开完一场会。按键只改当前运行状态，不写配置文件。
	Hotkey string `toml:"hotkey"`
	// Device 外部音源的录音设备（通常是回环设备），与听写用的麦克风无关。
	Device string `toml:"device"`
	// SilenceGate 按说话声切句，静音时既不送音频也不留着会话。
	// 默认开启；写 false 退回「一直连着、断了就重连」的老行为。
	SilenceGate bool `toml:"silence_gate"`
}

// TextOutput 的取值。
const (
	// OutputPaste 粘贴到当前输入框（同时进剪贴板）。
	OutputPaste = "paste"
	// OutputClipboard 只写剪贴板。
	OutputClipboard = "clipboard"
	// OutputNone 不输出：只转写，结果只进日志和浮层（听写不进识别结果订阅）。
	OutputNone = "none"
)

// ValidTextOutput 报告 v 是否是合法的输出去向。
func ValidTextOutput(v string) bool {
	return v == OutputPaste || v == OutputClipboard || v == OutputNone
}

// 听写模式：热键怎么开始和结束一次听写。
const (
	// ModeToggle 按一下开始，再按一下结束。
	ModeToggle = "toggle"
	// ModeHold 按住说话，松开结束。
	ModeHold = "hold"
	// legacyModeContinuous 是旧配置里 mode 的第三个值。持续转写不是听写的一种
	// 方式，而是与它并行的另一路，现在住在 [voice.continuous]。
	legacyModeContinuous = "continuous"
)

// ModeOptions 返回界面里可选的录音模式，按展示顺序。
func ModeOptions() []string { return []string{ModeToggle, ModeHold} }

// ValidMode 报告 m 是否是合法的录音模式。
func ValidMode(m string) bool {
	return m == ModeToggle || m == ModeHold
}

// ExpandPath 把开头的 ~ 换成 home；路径是用户手填的，写 ~ 很自然。
func ExpandPath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// EngineConfigFor 返回指定引擎的私有配置；未分组时回退旧版平铺配置。
func (vc VoiceConfig) EngineConfigFor(engine string) map[string]string {
	if per, ok := vc.EngineConfigs[engine]; ok {
		return per
	}
	if vc.EngineConfig != nil && len(vc.EngineConfig) > 0 {
		return vc.EngineConfig
	}
	return nil
}

// SetEngineConfig 写入指定引擎的私有配置，并返回配置是否发生变化。
func (vc *VoiceConfig) SetEngineConfig(engine string, cfg map[string]string) bool {
	if vc.EngineConfigs == nil {
		vc.EngineConfigs = map[string]map[string]string{}
	}
	old := vc.EngineConfigs[engine]
	vc.EngineConfigs[engine] = cfg
	return !equalStringMap(old, cfg)
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func Default() *Config {
	return &Config{
		Voice: VoiceConfig{
			Enabled: true, Mode: ModeToggle, PushToTalk: "Alt+Super",
			Language: "zh-CN", TextOutput: OutputPaste, ResourceID: "volc.bigasr.sauc.duration",
			Continuous: ContinuousConfig{SilenceGate: true},
		},
		Overlay: OverlayConfig{
			Enabled: true, Position: DefaultOverlayPosition(), IdleVisible: false, ShowText: true, Scale: 1.0,
		},
	}
}

// DefaultOverlayPosition anchors the macOS overlay to the notch; other
// platforms keep the bottom-center capsule.
func DefaultOverlayPosition() string {
	if runtime.GOOS == "darwin" {
		return "notch"
	}
	return "bottom-center"
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = FindConfig()
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Voice.migrateOutput(md.IsDefined("voice", "text_output"))
	cfg.Voice.migrateContinuous()
	return cfg, nil
}

// migrateContinuous 把旧的 mode = "continuous" 换成 [voice.continuous]。
// 那份配置里的 device 是外部音源，不是听写用的麦克风；而且它本来就不注册
// 热键，所以热键也清掉——否则迁完会凭空多出一路能往输入框打字的听写。
func (vc *VoiceConfig) migrateContinuous() {
	if vc.Mode != legacyModeContinuous {
		return
	}
	vc.Continuous.Enabled = true
	vc.Continuous.Device, vc.Device = vc.Device, ""
	vc.Mode, vc.PushToTalk = ModeToggle, ""
}

// migrateOutput 把旧的 auto_submit 换成 text_output：只有没显式写 text_output 时才看它
// （Default() 已经填了 paste，光看值分不出没写和写了 paste）。
// 迁移后清掉旧字段，下次存盘配置里就只剩 text_output。
func (vc *VoiceConfig) migrateOutput(explicit bool) {
	if !explicit || !ValidTextOutput(vc.TextOutput) {
		vc.TextOutput = OutputPaste
		if vc.AutoSubmit != nil && !*vc.AutoSubmit {
			vc.TextOutput = OutputClipboard
		}
	}
	vc.AutoSubmit = nil
}

func FindConfig() string {
	candidates := []string{"./config.toml"}
	if path := DefaultPath(); path != "" {
		candidates = append(candidates, path)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "just-talk", "config.toml"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// SavePath 报告 Save 会写到哪里：path 优先，其次现有配置，最后默认路径。
func SavePath(path string) string {
	if path != "" {
		return path
	}
	if p := FindConfig(); p != "" {
		return p
	}
	return DefaultPath()
}

// Save 写回 path；path 为空时找一份现有配置或用默认路径。用 -config 启动的
// 实例必须把自己那份传进来，否则保存会覆盖另一份配置。
func Save(cfg *Config, path string) error {
	path = SavePath(path)
	if path == "" {
		return fmt.Errorf("cannot determine user config directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// DefaultPath returns the platform-standard per-user configuration path.
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
			return filepath.Join(dir, "just-talk", "config.toml")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "just-talk", "config.toml")
	}
	return ""
}

// ---- Hotkey parser ----

var modifierNames = map[string]hotkey.Modifier{
	"ctrl": hotkey.ModCtrl, "alt": hotkey.ModAlt, "shift": hotkey.ModShift,
	"control": hotkey.ModCtrl, "option": hotkey.ModAlt, "super": hotkey.ModSuper,
	"cmd": hotkey.ModSuper, "command": hotkey.ModSuper, "win": hotkey.ModSuper,
	"fn": hotkey.ModFn, "function": hotkey.ModFn,
}

var keyNameToCode = buildKeyNameMap()

func buildKeyNameMap() map[string]hotkey.KeyCode {
	m := make(map[string]hotkey.KeyCode)
	for i := hotkey.KeyA; i <= hotkey.KeyZ; i++ {
		m[strings.ToLower(i.String())] = i
		m[i.String()] = i
	}
	for i := hotkey.Key0; i <= hotkey.Key9; i++ {
		m[i.String()] = i
	}
	for i := hotkey.KeyF1; i <= hotkey.KeyF24; i++ {
		m[strings.ToLower(i.String())] = i
		m[i.String()] = i
	}
	m["ctrl"] = hotkey.KeyCtrl
	m["control"] = hotkey.KeyCtrl
	m["alt"] = hotkey.KeyAlt
	m["option"] = hotkey.KeyAlt
	m["shift"] = hotkey.KeyShift
	m["super"] = hotkey.KeySuper
	m["cmd"] = hotkey.KeySuper
	m["command"] = hotkey.KeySuper
	m["win"] = hotkey.KeySuper
	m["fn"] = hotkey.KeyFn
	m["function"] = hotkey.KeyFn
	for _, k := range []hotkey.KeyCode{
		hotkey.KeySpace, hotkey.KeyTab, hotkey.KeyEnter, hotkey.KeyEscape,
		hotkey.KeyBackspace, hotkey.KeyCapsLock,
		hotkey.KeyArrowUp, hotkey.KeyArrowDown, hotkey.KeyArrowLeft, hotkey.KeyArrowRight,
		hotkey.KeyHome, hotkey.KeyEnd, hotkey.KeyPageUp, hotkey.KeyPageDown,
		hotkey.KeyInsert, hotkey.KeyDelete,
		hotkey.KeyMenu,
		hotkey.KeyNum0, hotkey.KeyNum1, hotkey.KeyNum2, hotkey.KeyNum3, hotkey.KeyNum4,
		hotkey.KeyNum5, hotkey.KeyNum6, hotkey.KeyNum7, hotkey.KeyNum8, hotkey.KeyNum9,
		hotkey.KeyBacktick, hotkey.KeyMinus, hotkey.KeyEqual,
		hotkey.KeyLeftBracket, hotkey.KeyRightBracket, hotkey.KeyBackslash,
		hotkey.KeySemicolon, hotkey.KeyQuote,
		hotkey.KeyComma, hotkey.KeyPeriod, hotkey.KeySlash,
	} {
		m[strings.ToLower(k.String())] = k
		m[k.String()] = k
	}
	m["space"] = hotkey.KeySpace
	m["enter"] = hotkey.KeyEnter
	m["return"] = hotkey.KeyEnter
	m["esc"] = hotkey.KeyEscape
	m["escape"] = hotkey.KeyEscape
	m["backspace"] = hotkey.KeyBackspace
	m["tab"] = hotkey.KeyTab
	m["up"] = hotkey.KeyArrowUp
	m["down"] = hotkey.KeyArrowDown
	m["left"] = hotkey.KeyArrowLeft
	m["right"] = hotkey.KeyArrowRight
	m["home"] = hotkey.KeyHome
	m["end"] = hotkey.KeyEnd
	m["pageup"] = hotkey.KeyPageUp
	m["pagedown"] = hotkey.KeyPageDown
	m["insert"] = hotkey.KeyInsert
	m["delete"] = hotkey.KeyDelete
	m["capslock"] = hotkey.KeyCapsLock
	m["apps"] = hotkey.KeyMenu
	m["application"] = hotkey.KeyMenu
	m["`"] = hotkey.KeyBacktick
	m["-"] = hotkey.KeyMinus
	m["="] = hotkey.KeyEqual
	m["["] = hotkey.KeyLeftBracket
	m["]"] = hotkey.KeyRightBracket
	m["\\"] = hotkey.KeyBackslash
	m[";"] = hotkey.KeySemicolon
	m["'"] = hotkey.KeyQuote
	m[","] = hotkey.KeyComma
	m["."] = hotkey.KeyPeriod
	m["/"] = hotkey.KeySlash
	return m
}

func ParseHotkey(s string) (hotkey.Combo, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return hotkey.Combo{}, fmt.Errorf("empty hotkey string")
	}
	parts := strings.Split(s, "+")
	var mods hotkey.Modifier
	var key hotkey.KeyCode
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if mod, ok := modifierNames[lower]; ok {
			mods |= mod
			continue
		}
		if k, ok := keyNameToCode[lower]; ok {
			if key != hotkey.KeyNone {
				return hotkey.Combo{}, fmt.Errorf("multiple keys in %q", s)
			}
			if k.IsModifier() && key == hotkey.KeyNone {
				key = k
			} else if !k.IsModifier() {
				key = k
			}
			continue
		}
		return hotkey.Combo{}, fmt.Errorf("unknown key %q in %q", part, s)
	}
	if key == hotkey.KeyNone && mods != hotkey.ModNone {
		return hotkey.Combo{Mods: mods, Key: hotkey.KeyNone}, nil
	}
	if key != hotkey.KeyNone && mods == hotkey.ModNone && !key.IsModifier() {
		return hotkey.Combo{Mods: hotkey.ModNone, Key: key}, nil
	}
	if key.IsModifier() && mods == hotkey.ModNone {
		return hotkey.Combo{Mods: hotkey.KeyCodeToModifier(key), Key: hotkey.KeyNone}, nil
	}
	if key != hotkey.KeyNone && mods != hotkey.ModNone {
		return hotkey.Combo{Mods: mods, Key: key}, nil
	}
	return hotkey.Combo{}, fmt.Errorf("cannot parse hotkey %q", s)
}

func ParseHotkeys(strings []string) ([]hotkey.Combo, error) {
	var combos []hotkey.Combo
	for _, s := range strings {
		c, err := ParseHotkey(s)
		if err != nil {
			return nil, err
		}
		combos = append(combos, c)
	}
	return combos, nil
}

// ParseHotkeyList parses a comma-separated hotkey list such as "Fn, F13",
// dropping duplicates. A comma right after "+" is the comma key itself, so
// "Fn+," stays one hotkey. Full-width commas are accepted as separators.
// An empty list is valid: no hotkey means no dictation, which is how a
// transcription-only instance is configured.
func ParseHotkeyList(s string) ([]hotkey.Combo, error) {
	s = strings.ReplaceAll(s, "，", ",")
	var combos []hotkey.Combo
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && (s[i] != ',' || strings.HasSuffix(strings.TrimSpace(s[start:i]), "+")) {
			continue
		}
		part := strings.TrimSpace(s[start:i])
		start = i + 1
		if part == "" {
			continue
		}
		c, err := ParseHotkey(part)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(combos, c) {
			combos = append(combos, c)
		}
	}
	return combos, nil
}
