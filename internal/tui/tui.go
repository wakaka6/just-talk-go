package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/c/just-talk-go/config"
	"github.com/c/just-talk-go/hotkey"
	"github.com/c/just-talk-go/internal/settings"
	"github.com/c/just-talk-go/plugins/voice"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	lStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	vStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	aStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	wStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	eStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	dStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	hStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).MarginTop(1)
)

type backendMsg hotkey.ProviderInfo
type devMsg struct {
	Devices []voice.InputDevice
	Error   error
}
type fieldType int

const (
	fString fieldType = iota
	fToggle
	fSelect
	// fAction renders as a value row that opens a modal on enter (no edit mode).
	fAction
)

type field struct {
	label   string
	key     string
	help    string
	fType   fieldType
	input   textinput.Model
	boolVal bool
	opts    []string
	optIdx  int
	// optVals are the values saved for opts when the label differs from what
	// goes into the config (device names carry what they record).
	optVals []string
	// required marks a must-fill key in engine config modals (label gets *).
	required bool
	// secret masks value when not editing (API keys).
	secret bool
}

type Model struct {
	w, h    int
	ready   bool
	info    hotkey.ProviderInfo
	logs    []string
	devices []voice.InputDevice
	cfg     *config.Config
	// cfgPath is the file this instance was started with; saving anywhere
	// else would overwrite someone else's config.
	cfgPath     string
	debug       bool
	showLogs    bool
	OnSave      func(*config.Config) error
	fields      []field
	cursor      int
	editing     bool
	helpVisible bool
	logExpanded bool
	// overlay modal state (engine picker / engine config); overlayNone = main form
	overlay   overlayState
	pickerIdx int
	ecFields  []field
	ecCursor  int
	ecEditing bool
}

func New(cfg *config.Config, cfgPath string) *Model {
	vc := cfg.Voice
	ti := func(v string) textinput.Model { t := textinput.New(); t.SetValue(v); t.Cursor.Blink = false; return t }
	positions := settings.OverlayPositionOptions(cfg.Overlay.Position)
	// Devices are enumerated asynchronously (fetchDevices); until that lands
	// the select offers the system default and whatever is configured.
	devLabels, devValues, devIdx := deviceOptions(nil, vc.Device)
	contLabels, contValues, contIdx := deviceOptions(nil, vc.Continuous.Device)
	outLabels := []string{"上屏（粘贴到输入框）", "仅复制到剪贴板", "不输出"}
	outValues := []string{config.OutputPaste, config.OutputClipboard, config.OutputNone}
	fs := []field{
		{label: "语音输入", key: "enabled", help: "关闭后不注册热键", fType: fToggle, boolVal: vc.Enabled},
		{label: "录音设备", key: "device", help: deviceHelp, fType: fSelect, opts: devLabels, optVals: devValues, optIdx: devIdx},
		{label: "热键", key: "push_to_talk", help: "例: Alt+Super / F9 / Ctrl+Alt+Tab；多个用逗号分隔，如 Fn, F13；不支持字母、数字、标点、空格等普通字符键", fType: fString, input: ti(vc.PushToTalk)},
		{label: "模式", key: "mode", help: "热键怎么开始和结束一次听写：toggle 按一下开、再按一下停 / hold 按住说话", fType: fSelect, opts: config.ModeOptions(), optIdx: idxOf(config.ModeOptions(), vc.Mode)},
		{label: "引擎", key: "engine", help: "回车选择识别引擎；流式引擎支持实时字幕", fType: fAction},
		{label: "引擎配置", key: "engine_config", help: "回车配置当前引擎的凭据与参数；各引擎独立保存", fType: fAction},
		{label: "语言", key: "language", help: "BCP-47，如 zh-CN；空 = 引擎默认", fType: fString, input: ti(vc.Language)},
		{label: "输出", key: "text_output", help: "听写结果送到哪里；不输出 = 只转写，结果只进日志和浮层", fType: fSelect, opts: outLabels, optVals: outValues, optIdx: idxOf(outValues, vc.TextOutput)},
		{label: "停止延迟(ms)", key: "stop_delay_ms", help: "松手后补录毫秒", fType: fString, input: ti(fmt.Sprintf("%d", vc.StopDelayMs))},
		{label: "热词", key: "hotwords", help: "逗号分隔术语", fType: fString, input: ti(strings.Join(vc.Hotwords, ", "))},
		{label: "热词文件", key: "hotwords_file", help: "外部词表路径，一行一个词，# 开头为注释；与上面的热词合并，每次会话开始时重读", fType: fString, input: ti(vc.HotwordsFile)},
		{label: "持续转写", key: "continuous_enabled", help: "给别的程序订阅的后台服务：转会议等外部音源，由说话声切句，不输出文本、不占用刘海，只给日志和结果订阅", fType: fToggle, boolVal: vc.Continuous.Enabled},
		{label: "持续转写设备", key: "continuous_device", help: "外部音源的录音设备，通常是回环设备；和上面听写用的麦克风无关", fType: fSelect, opts: contLabels, optVals: contValues, optIdx: contIdx},
		{label: "持续转写热键", key: "continuous_hotkey", help: "按一下开、再按一下关；只改当前状态，不写配置。留空 = 不注册，不能与听写热键重复", fType: fString, input: ti(vc.Continuous.Hotkey)},
		{label: "订阅地址", key: "stream_addr", help: streamAddrHelp(), fType: fString, input: ti(vc.StreamAddr)},
		{label: "提示位置", key: "overlay_position", help: "notch 为 macOS 刘海，其他平台按 top-center 处理；保存后立即生效", fType: fSelect, opts: positions, optIdx: idxOf(positions, cfg.Overlay.Position)},
	}
	return &Model{cfg: cfg, cfgPath: cfgPath, fields: fs, logs: make([]string, 0, 100), cursor: -1, showLogs: true}
}

// streamAddrHelp offers unix: only where it is accepted: Windows refuses it.
func streamAddrHelp() string {
	const lead = "Just Talk 运行时一直在这个地址上开 WebSocket，把持续转写的结果推给别的程序（听写不推），关着时只报空闲；"
	if runtime.GOOS == "windows" {
		return lead + "默认 127.0.0.1:7590，只允许回环地址"
	}
	return lead + "默认 127.0.0.1:7590，只允许回环地址；也可写 unix:<路径>，只有你自己能连"
}

func (m *Model) SetDebug(debug bool) {
	m.debug = debug
}

// selected returns the config value of the chosen option.
func (f field) selected() string {
	if f.optVals != nil {
		return f.optVals[f.optIdx]
	}
	return f.opts[f.optIdx]
}

func idxOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

func (m *Model) Init() tea.Cmd { return tea.Batch(fetchDevices(), tea.EnterAltScreen, tickRefresh()) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h, m.ready = msg.Width, msg.Height, true
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case backendMsg:
		m.info = hotkey.ProviderInfo(msg)
	case refreshMsg:
		return m, tickRefresh()
	case devMsg:
		if msg.Error != nil {
			m.logf("设备: %s", msg.Error)
		} else {
			m.setDevices(msg.Devices)
		}
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// Modal overlays capture all input while open.
	if m.overlay != overlayNone {
		if m.handleOverlayKey(msg) {
			return nil
		}
	}

	// Editing a field
	if m.editing && m.cursor >= 0 && m.cursor < len(m.fields) {
		f := &m.fields[m.cursor]
		switch k {
		case "esc":
			m.editing = false
			f.input.Blur()
			return nil
		case "enter":
			m.editing = false
			f.input.Blur()
			m.save()
			return nil
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
		case fToggle:
			switch k {
			case " ":
				f.boolVal = !f.boolVal
			case "j", "down", "k", "up":
				m.editing = false
				m.cursor++
				if m.cursor >= len(m.fields) {
					m.cursor = 0
				}
			}
		case fString:
			var cmd tea.Cmd
			f.input, cmd = f.input.Update(msg)
			return cmd
		}
		return nil
	}

	// Navigation mode
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "s":
		m.save()
		return nil
	case "e", "i", "enter":
		if m.cursor >= 0 && m.cursor < len(m.fields) && m.fields[m.cursor].fType == fAction {
			switch m.fields[m.cursor].key {
			case "engine":
				m.openEnginePicker()
			case "engine_config":
				m.openEngineConfig()
			}
			return nil
		}
		m.editing = true
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.fields[m.cursor].fType == fString {
			m.fields[m.cursor].input.Focus()
		}
	case "j", "down":
		if m.cursor < 0 {
			m.cursor = 0
		} else {
			m.cursor++
			if m.cursor >= len(m.fields) {
				m.cursor = len(m.fields) - 1
			}
		}
	case "k", "up":
		if m.cursor < 0 {
			m.cursor = 0
		} else {
			m.cursor--
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
	case "l":
		if m.showLogs {
			m.logExpanded = !m.logExpanded
		}
	case "h":
		m.helpVisible = !m.helpVisible
	}
	return nil
}
func (m *Model) save() {
	next := *m.cfg
	vc := &next.Voice
	for _, f := range m.fields {
		switch f.key {
		case "enabled":
			vc.Enabled = f.boolVal
		case "push_to_talk":
			vc.PushToTalk = f.input.Value()
		case "mode":
			vc.Mode = f.selected()
		case "device":
			vc.Device = f.selected()
		case "language":
			vc.Language = strings.TrimSpace(f.input.Value())
		case "text_output":
			vc.TextOutput = f.selected()
		case "stop_delay_ms":
			fmt.Sscanf(f.input.Value(), "%d", &vc.StopDelayMs)
		case "hotwords":
			vc.Hotwords = settings.SplitList(f.input.Value())
		case "hotwords_file":
			vc.HotwordsFile = strings.TrimSpace(f.input.Value())
		case "stream_addr":
			if vc.StreamAddr = strings.TrimSpace(f.input.Value()); vc.StreamAddr == "" {
				vc.StreamAddr = config.DefaultStreamAddr
			}
		case "continuous_enabled":
			vc.Continuous.Enabled = f.boolVal
		case "continuous_device":
			vc.Continuous.Device = f.selected()
		case "continuous_hotkey":
			vc.Continuous.Hotkey = strings.TrimSpace(f.input.Value())
		case "overlay_position":
			next.Overlay.Position = f.selected()
		}
	}
	if err := settings.ValidateHotkeys(vc.PushToTalk); err != nil {
		m.logf("❌ %s", err)
		m.restorePushToTalkField()
		return
	}
	if err := settings.ValidateContinuousHotkey(vc.Continuous.Hotkey, vc.PushToTalk); err != nil {
		m.logf("❌ 持续转写热键：%s", err)
		return
	}
	if err := voice.CheckStreamAddr(vc.StreamAddr); err != nil {
		m.logf("❌ %s", err)
		return
	}
	positionChanged := next.Overlay.Position != m.cfg.Overlay.Position
	m.cfg = &next
	if err := config.Save(m.cfg, m.cfgPath); err != nil {
		m.logf("保存失败: %s", err)
	} else {
		m.logf("✅ 配置已保存到 %s", config.SavePath(m.cfgPath))
	}
	m.logf("  push_to_talk=%s", vc.PushToTalk)
	if positionChanged {
		m.logf("  overlay.position=%s", next.Overlay.Position)
	}
	if m.OnSave != nil {
		if err := m.OnSave(m.cfg); err != nil {
			m.logf("❌ 热键注册失败: %s", err)
		}
	}
}

func (m *Model) restorePushToTalkField() {
	value := m.cfg.Voice.PushToTalk
	if settings.ValidateHotkeys(value) != nil {
		value = config.Default().Voice.PushToTalk
	}
	for i := range m.fields {
		if m.fields[i].key == "push_to_talk" {
			m.fields[i].input.SetValue(value)
			break
		}
	}
	m.logf("↩️  热键已恢复为 %s", value)
}

func (m *Model) View() string {
	if !m.ready {
		return "loading..."
	}
	var b strings.Builder
	b.WriteString(tStyle.Render("🎙️ 🗣️ Just Talk") + "\n")
	b.WriteString(vStyle.Render("减少用键盘的次数，改用口喷吧。") + "\n")
	b.WriteString(m.renderVoiceStats() + "\n\n")
	b.WriteString(lStyle.Render("── 配置 (e 编辑, s 保存, h 帮助, j/k 导航) ──") + "\n")
	for i, f := range m.fields {
		marker := "  "
		if i == m.cursor {
			if m.editing {
				marker = aStyle.Render("▶ ")
			} else {
				marker = aStyle.Render("▸ ")
			}
		}
		line := marker + lStyle.Render(f.label+": ") + m.renderField(i, f)
		if m.helpVisible && f.help != "" {
			line += " " + dStyle.Render("("+f.help+")")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + lStyle.Render("── 录音状态 ──") + "\n")
	b.WriteString("  " + m.renderVoiceStatus() + "\n")
	if url, subs := voice.StreamStatus(); url != "" {
		b.WriteString("  " + dStyle.Render(fmt.Sprintf("结果订阅 %s · %d 个订阅者", url, subs)) + "\n")
	}
	if m.showLogs {
		b.WriteString("\n" + lStyle.Render("── 日志 (l 展开) ──") + "\n")
		maxLogs := 5
		if m.logExpanded {
			maxLogs = 100
		}
		// TUI internal logs
		s := 0
		if len(m.logs) > maxLogs {
			s = len(m.logs) - maxLogs
		}
		for _, l := range m.logs[s:] {
			b.WriteString("  " + dStyle.Render(l) + "\n")
		}
		// Voice plugin logs
		voiceLogs := voice.TUILogs()
		sv := 0
		if len(voiceLogs) > maxLogs {
			sv = len(voiceLogs) - maxLogs
		}
		for _, l := range voiceLogs[sv:] {
			b.WriteString("  " + dStyle.Render(l) + "\n")
		}
	}
	b.WriteString(hStyle.Render("  j/k 导航 | e 编辑 | h 帮助 | esc 退出编辑 | s 保存 | q 退出"))
	view := b.String()
	switch m.overlay {
	case overlayPicker:
		view = compositeOver(view, m.renderPicker(), m.w, m.h)
	case overlayEngineConfig:
		view = compositeOver(view, m.renderEngineConfig(), m.w, m.h)
	}
	return view
}

func (m *Model) renderVoiceStats() string {
	stats := voice.TUIStats()
	cpm := 0.0
	if stats.AudioDuration > 0 {
		cpm = float64(stats.Chars) / stats.AudioDuration.Minutes()
	}
	lastCPM := 0.0
	if stats.LastAudioDuration > 0 {
		lastCPM = float64(stats.LastTextChars) / stats.LastAudioDuration.Minutes()
	}
	last := dStyle.Render("最近速度 ") + wStyle.Render("0") + dStyle.Render(" 字/分钟")
	if stats.LastAudioDuration > 0 {
		last = dStyle.Render("最近速度 ") + wStyle.Render(fmt.Sprintf("%.0f", lastCPM)) + dStyle.Render(" 字/分钟")
	}
	return strings.Join([]string{
		dStyle.Render("历史 ") + aStyle.Render(fmt.Sprintf("%d", stats.Sessions)) + dStyle.Render(" 次"),
		dStyle.Render("总字数 ") + aStyle.Render(fmt.Sprintf("%d", stats.Chars)),
		dStyle.Render("平均速度 ") + wStyle.Render(fmt.Sprintf("%.0f", cpm)) + dStyle.Render(" 字/分钟"),
		last,
	}, "  |  ")
}

func (m *Model) renderVoiceStatus() string {
	status := voice.TUIStatus()
	label := "待机"
	style := dStyle
	switch status.State {
	case "connecting":
		label, style = "连接中", wStyle
	case "recording":
		label, style = "录音中", aStyle
	case "stopping_delayed":
		label, style = "延迟停止", wStyle
	case "stopping":
		label, style = "停止中", wStyle
	case "error":
		label, style = "错误", eStyle
	}
	parts := []string{style.Render(label)}
	if status.Detail != "" {
		parts = append(parts, vStyle.Render(status.Detail))
	}
	if !status.StopAt.IsZero() {
		remaining := time.Until(status.StopAt)
		if remaining < 0 {
			remaining = 0
		}
		parts = append(parts, fmt.Sprintf("剩余 %dms", remaining.Milliseconds()))
	}
	if status.State == "error" && !status.ErrorUntil.IsZero() {
		remaining := time.Until(status.ErrorUntil)
		if remaining < 0 {
			remaining = 0
		}
		parts = append(parts, fmt.Sprintf("保留 %ds", int(remaining.Seconds())))
	}
	if status.PendingFinishes > 0 {
		parts = append(parts, fmt.Sprintf("后台收尾 %d", status.PendingFinishes))
	}
	if status.SessionID > 0 {
		parts = append(parts, dStyle.Render(fmt.Sprintf("#%d", status.SessionID)))
	}
	if !m.debug {
		return strings.Join(parts, "  ")
	}
	if !status.UpdatedAt.IsZero() {
		age := time.Since(status.UpdatedAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("更新 %dms 前", age.Milliseconds())))
	}
	if !status.LastHotkeyAt.IsZero() {
		age := time.Since(status.LastHotkeyAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("热键 %s %dms 前", status.LastHotkeyType, age.Milliseconds())))
	}
	if !status.LastHandledAt.IsZero() {
		age := time.Since(status.LastHandledAt)
		if age < 0 {
			age = 0
		}
		parts = append(parts, dStyle.Render(fmt.Sprintf("处理 %s %dms 前", status.LastHandledType, age.Milliseconds())))
	}
	if status.QueuedHotkeys > 0 || status.HandledHotkeys > 0 {
		parts = append(parts, dStyle.Render(fmt.Sprintf("入队/处理 %d/%d q=%d", status.QueuedHotkeys, status.HandledHotkeys, status.EventQueueLen)))
	}
	return strings.Join(parts, "  ")
}

func (m *Model) renderField(i int, f field) string {
	editing := m.editing && m.cursor == i
	switch f.fType {
	case fAction:
		switch f.key {
		case "engine":
			meta := settings.EngineFor(m.currentEngine())
			var tag string
			if meta.Streaming {
				tag = aStyle.Render(" [流式]")
			} else {
				tag = dStyle.Render(" [整段]")
			}
			return vStyle.Render(meta.Name) + tag + " " + dStyle.Render("(回车选择)")
		case "engine_config":
			engine := m.currentEngine()
			if settings.EngineConfigured(m.cfg.Voice, engine) {
				return aStyle.Render("已配置") + " " + dStyle.Render("(回车修改)")
			}
			return wStyle.Render("缺少必填项") + " " + dStyle.Render("(回车配置)")
		}
	case fString:
		v := f.input.Value()
		if f.key == "access_key" && !editing && len(v) > 8 {
			v = v[:8] + "***"
		}
		if editing {
			return f.input.View()
		}
		return vStyle.Render(v)
	case fToggle:
		if f.boolVal {
			return aStyle.Render("● 开") + "  " + dStyle.Render("(空格)")
		}
		return dStyle.Render("○ 关  (空格)")
	case fSelect:
		v := f.opts[f.optIdx]
		if v == "" {
			v = "(未设置)"
		}
		if editing {
			return aStyle.Render("[" + v + " ▲▼]")
		}
		return vStyle.Render(v)
	}
	return ""
}

func (m *Model) logf(format string, args ...interface{}) {
	if !m.showLogs {
		return
	}
	m.logs = append(m.logs, fmt.Sprintf(format, args...))
}

func SetProviderInfo(info hotkey.ProviderInfo) tea.Cmd {
	return func() tea.Msg { return backendMsg(info) }
}
func fetchDevices() tea.Cmd {
	return func() tea.Msg {
		devices, err := voice.ListDevices()
		if err != nil {
			return devMsg{Error: err}
		}
		return devMsg{Devices: devices}
	}
}

type refreshMsg struct{}

func tickRefresh() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return refreshMsg{} })
}

// Handle refreshMsg in Update
