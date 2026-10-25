"use strict";

// One page for the desktop window and `just-talk --web`. `saved` is the last
// state the server returned; `draft` holds the settings being edited and
// `edits` the engine fields changed since the last save, keyed by engine.

const form = $("form");

let saved = null;
let draft = null;
let edits = {};
let saving = false;

const clone = (v) => JSON.parse(JSON.stringify(v));

const POSITION_LABELS = {
  notch: "刘海（macOS）",
  "top-left": "左上角",
  "top-center": "顶部居中",
  "top-right": "右上角",
  "bottom-left": "左下角",
  "bottom-center": "底部居中",
  "bottom-right": "右下角",
};

// 识别结果去向，与 config.OutputPaste / OutputClipboard / OutputNone 对应。
const OUTPUT_LABELS = {
  paste: "上屏（粘贴到输入框）",
  clipboard: "仅复制到剪贴板",
  none: "不输出",
};

// ---- tabs ----

const PANES = ["general", "engine", "activity"];

function showPane(name) {
  if (!PANES.includes(name)) name = PANES[0];
  for (const p of PANES) {
    $(`pane-${p}`).hidden = p !== name;
    const tab = $(`tab-${p}`);
    tab.setAttribute("aria-selected", String(p === name));
    tab.tabIndex = p === name ? 0 : -1;
  }
  try { localStorage.setItem("justTalk.pane", name); } catch {}
}

for (const tab of document.querySelectorAll('[role="tab"]')) {
  tab.addEventListener("click", () => showPane(tab.dataset.pane));
  tab.addEventListener("keydown", (ev) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[ev.key];
    if (!step) return;
    const next = PANES[(PANES.indexOf(tab.dataset.pane) + step + PANES.length) % PANES.length];
    showPane(next);
    $(`tab-${next}`).focus();
  });
}

// ---- settings form ----

function applyState(state) {
  saved = state;
  draft = clone(state.settings);
  edits = {};
  document.body.classList.toggle("mac-app", state.shell === "app" && state.platform === "darwin");
  $("configPath").textContent = state.configPath;
  $("configPath").title = state.configPath;
  form.elements.enabled.checked = draft.enabled;
  form.elements.continuousEnabled.checked = draft.continuousEnabled;
  for (const name of ["pushToTalk", "continuousHotkey", "stopDelayMs", "hotwordsFile", "streamAddr"]) form.elements[name].value = draft[name];
  $("textOutput").replaceChildren(...state.textOutputs.map((o) => el("option", { value: o }, OUTPUT_LABELS[o] || o)));
  $("textOutput").value = draft.textOutput;
  $("language").replaceChildren(...state.languages.map((l) =>
    el("option", { value: l.code }, l.code && l.label !== l.code ? `${l.label}（${l.code}）` : l.label)));
  $("language").value = draft.language;
  $("overlayPosition").replaceChildren(...state.overlayPositions.map((p) => el("option", { value: p }, POSITION_LABELS[p] || p)));
  $("overlayPosition").value = draft.overlayPosition;
  endRecording();
  showHotkeyError("");
  renderSlots();
  renderDevices();
  renderHotwords();
  renderModes();
  renderEngines();
  updateDirty();
}

// ---- hotkey slots ----
//
// Two independent groups: the dictation hotkeys and the one that switches
// the transcription service. Each is a comma-separated list whose first
// entries are the slots shown here; any further ones (hand-edited config)
// are kept as they are. A slot is recorded from key events in this page
// while the server keeps the global hotkeys suspended.

const HOTKEY_GROUPS = {
  pushToTalk: { container: "slots", names: ["主热键", "第二热键"], extra: "hotkeyExtra" },
  continuousHotkey: { container: "continuousSlots", names: ["开关热键"], extra: null },
};
const MOD_ORDER = ["Ctrl", "Alt", "Shift", "Super", "Fn"];
const MOD_CODES = {
  ControlLeft: "Ctrl", ControlRight: "Ctrl", AltLeft: "Alt", AltRight: "Alt",
  ShiftLeft: "Shift", ShiftRight: "Shift", MetaLeft: "Super", MetaRight: "Super",
  OSLeft: "Super", OSRight: "Super", Fn: "Fn",
};
// KeyboardEvent.code → config key name (config.ParseHotkey).
const KEY_CODES = {
  Tab: "Tab", CapsLock: "CapsLock", Enter: "Enter", NumpadEnter: "Enter", Backspace: "Backspace",
  Space: "Space", Escape: "Escape", ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left",
  ArrowRight: "Right", Home: "Home", End: "End", PageUp: "PageUp", PageDown: "PageDown",
  Insert: "Insert", Delete: "Delete", ContextMenu: "Menu", Backquote: "`", Minus: "-",
  Equal: "=", BracketLeft: "[", BracketRight: "]", Backslash: "\\", Semicolon: ";",
  Quote: "'", Comma: ",", Period: ".", Slash: "/",
};

let rec = null; // { group, slot, mods: Set, ready, keepalive }

function keyName(code) {
  if (KEY_CODES[code]) return KEY_CODES[code];
  let m = /^Key([A-Z])$/.exec(code) || /^Digit(\d)$/.exec(code);
  if (m) return m[1];
  if ((m = /^Numpad(\d)$/.exec(code))) return `Num${m[1]}`;
  if ((m = /^F(\d{1,2})$/.exec(code)) && +m[1] >= 1 && +m[1] <= 24) return code;
  return null;
}

function modName(ev) {
  return MOD_CODES[ev.code] || (ev.key === "Fn" ? "Fn" : null);
}

function renderSlots() {
  for (const [key, group] of Object.entries(HOTKEY_GROUPS)) renderGroup(key, group);
}

function renderGroup(key, group) {
  const list = splitHotkeys(draft[key]);
  $(group.container).replaceChildren(...group.names.map((name, i) => {
    const value = list[i] || "";
    const recording = rec?.group === key && rec?.slot === i;
    let body;
    if (recording) {
      const held = MOD_ORDER.filter((m) => rec.mods.has(m));
      body = held.length ? keycaps(held, saved.platform) : el("span", { class: "hint" }, rec.ready ? "请按下快捷键…" : "正在暂停全局热键…");
    } else {
      body = value ? keycaps(value.split("+"), saved.platform) : el("span", { class: "hint" }, i === 0 ? "点击录制" : "未设置，点击录制");
    }
    return el("div", { class: "slot" },
      el("span", { class: "slot-name" }, name),
      el("button", {
        type: "button",
        class: recording ? "recorder recording" : "recorder",
        "aria-label": `${name}：${value || "未设置"}，${recording ? "正在录制" : "点击录制"}`,
        onclick: () => (recording ? endRecording() : startRecording(key, i)),
      },
        recording ? el("span", { class: "rec-dot" }) : null,
        body,
        recording ? el("span", { class: "rec-hint" }, "Esc 取消") : null),
      // No hotkey at all is a valid setup, so every slot can be cleared.
      value && !recording
        ? el("button", { type: "button", class: "clear", title: `清除${name}`, "aria-label": `清除${name}`, onclick: () => setSlot(key, i, "") }, "×")
        : el("span")); // keeps the grid column
  }));
  if (!group.extra) return;
  const extra = list.slice(group.names.length);
  $(group.extra).hidden = extra.length === 0;
  $(group.extra).textContent = `配置中还有 ${extra.join("、")}，保存时一并保留；可用“手动编辑”修改。`;
}

function setSlot(key, i, value) {
  const list = splitHotkeys(draft[key]);
  // A combo can only mean one thing, in either group.
  const taken = Object.keys(HOTKEY_GROUPS).flatMap((k) =>
    splitHotkeys(draft[k]).filter((v, j) => !(k === key && j === i)));
  if (value && taken.includes(value)) {
    showHotkeyError(`${value} 已经被占用`);
    return;
  }
  list[i] = value;
  draft[key] = list.filter(Boolean).join(", ");
  form.elements[key].value = draft[key];
  showHotkeyError("");
  renderSlots();
  updateDirty();
}

function showHotkeyError(text) {
  $("hotkeyError").textContent = text;
  $("hotkeyError").hidden = !text;
}

async function startRecording(group, slot) {
  endRecording();
  showHotkeyError("");
  const r = { group, slot, mods: new Set(), ready: false, keepalive: 0 };
  rec = r;
  renderSlots();
  try {
    await api("/api/hotkey/capture", { active: true });
  } catch (err) {
    if (rec === r) endRecording();
    showHotkeyError(err.message);
    return;
  }
  if (rec !== r) return;
  r.ready = true;
  r.keepalive = setInterval(() => api("/api/hotkey/capture", { active: true }).catch(() => {}), 5000);
  renderSlots();
}

function endRecording() {
  if (!rec) return;
  clearInterval(rec.keepalive);
  rec = null;
  api("/api/hotkey/capture", { active: false }).catch(() => {});
  renderSlots();
}

// A non-modifier key completes the combo at once; a modifier-only combo
// completes when its first modifier is released, with every modifier held.
async function finishRecording(key) {
  const { group, slot } = rec;
  const value = [...MOD_ORDER.filter((m) => rec.mods.has(m)), ...(key ? [key] : [])].join("+");
  endRecording();
  try {
    setSlot(group, slot, (await api("/api/hotkey/check", { value })).value);
  } catch (err) {
    showHotkeyError(err.message);
  }
}

window.addEventListener("keydown", (ev) => {
  if (!rec) return;
  ev.preventDefault();
  ev.stopPropagation();
  if (ev.repeat) return;
  const mod = modName(ev);
  if (mod) {
    rec.mods.add(mod);
    renderSlots();
    return;
  }
  if (ev.code === "Escape" && rec.mods.size === 0) {
    endRecording();
    return;
  }
  const key = keyName(ev.code);
  if (!key) {
    showHotkeyError(`无法录制该按键（${ev.code || ev.key}），可用“手动编辑”输入`);
    return;
  }
  // Modifiers held down before recording began only show up as flags.
  if (ev.ctrlKey) rec.mods.add("Ctrl");
  if (ev.altKey) rec.mods.add("Alt");
  if (ev.shiftKey) rec.mods.add("Shift");
  if (ev.metaKey) rec.mods.add("Super");
  if (ev.getModifierState?.("Fn")) rec.mods.add("Fn");
  finishRecording(key);
}, true);

window.addEventListener("keyup", (ev) => {
  if (!rec) return;
  ev.preventDefault();
  ev.stopPropagation();
  if (modName(ev) && rec.mods.size) finishRecording(null);
}, true);

// Keys pressed in another app never reach the page; stop instead of
// leaving the global hotkeys suspended.
window.addEventListener("blur", endRecording);

$("manualToggle").addEventListener("click", () => {
  const input = form.elements.pushToTalk;
  input.hidden = !input.hidden;
  $("manualToggle").textContent = input.hidden ? "手动编辑" : "收起手动编辑";
  if (!input.hidden) input.focus();
});

// Recording your own voice and recording what the computer plays are
// different jobs on different devices; group them so the choice is obvious.
const DEVICE_GROUPS = [
  ["mic", "麦克风（录外部声音）"],
  ["loopback", "回环设备（录本机播放的声音）"],
  ["unknown", "其他"],
];

// Dictation and the transcription service each pick their own device, so the
// same list is rendered into both selects.
function deviceOptions(value) {
  const devices = saved.devices || [];
  const current = /^default$/i.test(value) ? "" : value;
  const options = [el("option", { value: "" }, "系统默认输入")];
  for (const [kind, label] of DEVICE_GROUPS) {
    const items = devices.filter((d) => d.kind === kind);
    if (items.length) {
      options.push(el("optgroup", { label }, ...items.map((d) => el("option", { value: d.name }, d.name))));
    }
  }
  // Keep a configured device that is not plugged in right now, so opening the
  // settings page never silently rewrites the config.
  if (current && !devices.some((d) => d.name === current)) {
    options.push(el("option", { value: current }, `${current}（未检测到）`));
  }
  return { options, current };
}

function renderDevices() {
  const devices = saved.devices || [];
  for (const [id, value] of [["device", draft.device], ["continuousDevice", draft.continuousDevice]]) {
    const { options, current } = deviceOptions(value);
    $(id).replaceChildren(...options);
    $(id).value = current;
  }

  const hint = saved.platform === "darwin" && !devices.some((d) => d.kind === "loopback")
    ? "没有回环设备：macOS 不能直接录本机声音，需装 BlackHole 等虚拟声卡，并把要录的应用的播放输出指向它。"
    : "";
  $("deviceNote").textContent = hint;
  $("deviceNote").hidden = !hint;
  $("deviceError").textContent = saved.devicesError || "";
  $("deviceError").hidden = !saved.devicesError;
}

function renderModes() {
  const labels = { toggle: "切换", hold: "按住" };
  $("modes").replaceChildren(...saved.modes.map((m) => el("button", {
    type: "button",
    role: "radio",
    "aria-checked": String(draft.mode === m),
    onclick: () => { draft.mode = m; renderModes(); updateDirty(); },
  }, labels[m] || m)));
}

// ---- hotwords ----

function renderHotwords() {
  const input = $("hotwordInput");
  $("hotwords").replaceChildren(...draft.hotwords.map((w, i) => el("span", { class: "chip" },
    w,
    el("button", { type: "button", title: `移除 ${w}`, "aria-label": `移除热词 ${w}`, onclick: () => removeHotword(i) }, "×"))), input);
}

function addHotwords(text) {
  const words = text.split(/[,，、\n]/).map((w) => w.trim()).filter(Boolean);
  const before = draft.hotwords.length;
  for (const w of words) if (!draft.hotwords.includes(w)) draft.hotwords.push(w);
  if (draft.hotwords.length === before) return;
  renderHotwords();
  updateDirty();
}

function removeHotword(i) {
  draft.hotwords.splice(i, 1);
  renderHotwords();
  updateDirty();
  $("hotwordInput").focus();
}

$("hotwords").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) $("hotwordInput").focus(); });
$("hotwordInput").addEventListener("keydown", (ev) => {
  const input = ev.target;
  if (ev.isComposing) return; // IME candidate selection
  if (ev.key === "Enter" || ev.key === "," || ev.key === "，" || ev.key === "、") {
    ev.preventDefault();
    addHotwords(input.value);
    input.value = "";
  } else if (ev.key === "Backspace" && input.value === "" && draft.hotwords.length) {
    removeHotword(draft.hotwords.length - 1);
  }
});
$("hotwordInput").addEventListener("paste", (ev) => {
  const text = ev.clipboardData?.getData("text") || "";
  if (!/[,，、\n]/.test(text)) return;
  ev.preventDefault();
  addHotwords(ev.target.value + text);
  ev.target.value = "";
});
$("hotwordInput").addEventListener("blur", (ev) => {
  if (!ev.target.value.trim()) return;
  addHotwords(ev.target.value);
  ev.target.value = "";
});

// ---- engines ----

function engineValue(engine, field) {
  return edits[engine.name]?.[field.key] ?? field.value;
}

// Mirrors settings.EngineConfigured for unsaved edits.
function isConfigured(engine, values = (f) => engineValue(engine, f)) {
  const v = (key) => {
    const f = engine.fields.find((f) => f.key === key);
    return f ? String(values(f)).trim() : "";
  };
  if (engine.name === "doubao-stream") return v("api_key") !== "" || (v("app_id") !== "" && v("access_key") !== "");
  return engine.fields.filter((f) => f.required).every((f) => v(f.key) !== "");
}

function renderEngines() {
  $("engines").replaceChildren(...saved.engines.map((e) => {
    const configured = isConfigured(e);
    return el("div", {
      class: "engine",
      role: "radio",
      tabindex: "0",
      "aria-checked": String(draft.engine === e.name),
      onclick: (ev) => { if (!ev.target.closest("button")) pickEngine(e); },
      onkeydown: (ev) => {
        if (ev.target !== ev.currentTarget) return;
        if (ev.key === " " || ev.key === "Enter") { ev.preventDefault(); pickEngine(e); }
      },
    },
      el("span", { class: "radio" }),
      el("span", { class: "title" },
        el("span", { class: "name" }, e.title),
        el("span", { class: "tag" }, e.streaming ? "流式" : "整段")),
      el("span", { class: configured ? "state ok" : "state missing" }, configured ? "已配置" : "需要配置"),
      e.fields.length
        ? el("button", { type: "button", class: "ghost configure", onclick: () => openSheet(e) }, "配置…")
        : el("span"),
      el("span", { class: "blurb" }, e.blurb),
    );
  }));
}

function pickEngine(engine) {
  const previous = draft.engine;
  draft.engine = engine.name;
  renderEngines();
  updateDirty();
  // Picking an engine that still needs credentials asks for them; 取消
  // puts the previous choice back.
  if (!isConfigured(engine) && engine.fields.length) openSheet(engine, previous);
}

// ---- engine sheet ----
//
// A sheet edits one engine's fields on a scratch copy; 完成 stages them in
// `edits` (saved with the rest of the page), 取消 drops them.

let sheet = null; // { engine, values, revertTo }

function fieldInput(f, value, onChange) {
  if (f.options) {
    const select = el("select", { onchange: onChange },
      el("option", { value: "" }, "默认"),
      ...f.options.map((o) => el("option", { value: o }, o)));
    select.value = value;
    return select;
  }
  if (f.secret) {
    const input = el("input", { type: "password", value, spellcheck: "false", oninput: onChange, autocomplete: "off" });
    return el("span", { class: "secret" }, input, el("button", {
      type: "button",
      class: "ghost",
      onclick: (ev) => {
        const show = input.type === "password";
        input.type = show ? "text" : "password";
        ev.target.textContent = show ? "隐藏" : "显示";
      },
    }, "显示"));
  }
  return el("input", { type: "text", value, spellcheck: "false", oninput: onChange, placeholder: "默认" });
}

function sheetRow(f) {
  return el("label", { class: "row" },
    el("span", { class: "label" },
      el("span", {}, f.label, f.required && !f.advanced ? el("span", { class: "req" }, "*") : null),
      f.help ? el("small", {}, f.help) : null),
    fieldInput(f, sheet.values[f.key], (ev) => {
      sheet.values[f.key] = ev.target.value;
      renderSheetAdvancedLabel();
    }));
}

function renderSheetAdvancedLabel() {
  const filled = sheet.engine.fields.filter((f) => f.advanced && String(sheet.values[f.key]).trim()).length;
  $("sheetAdvancedLabel").textContent = filled ? `高级选项（已填写 ${filled} 项）` : "高级选项";
}

function openSheet(engine, revertTo) {
  sheet = { engine, revertTo, values: Object.fromEntries(engine.fields.map((f) => [f.key, engineValue(engine, f)])) };
  $("sheetTitle").replaceChildren(engine.title, el("span", { class: "tag" }, engine.streaming ? "流式" : "整段"));
  $("sheetBlurb").textContent = engine.blurb;
  const main = engine.fields.filter((f) => !f.advanced);
  const advanced = engine.fields.filter((f) => f.advanced);
  $("sheetFields").replaceChildren(...main.map(sheetRow));
  $("sheetFields").hidden = main.length === 0;
  $("sheetAdvancedFields").replaceChildren(...advanced.map(sheetRow));
  $("sheetAdvanced").hidden = advanced.length === 0;
  // Open the advanced fields when they are what configures the engine
  // (e.g. legacy Doubao credentials), so nothing set is hidden away.
  $("sheetAdvanced").open = main.length === 0 ||
    (isConfigured(engine) && !isConfigured(engine, (f) => (f.advanced ? "" : sheet.values[f.key])));
  renderSheetAdvancedLabel();
  $("sheetDoc").hidden = !engine.doc;
  const backdrop = $("sheet");
  backdrop.hidden = false;
  requestAnimationFrame(() => backdrop.classList.add("open"));
  (backdrop.querySelector("input, select") || $("sheetDone")).focus();
}

function closeSheet(commit) {
  if (!sheet) return;
  if (commit) {
    const { engine, values } = sheet;
    const changed = Object.fromEntries(engine.fields
      .filter((f) => values[f.key] !== f.value)
      .map((f) => [f.key, values[f.key]]));
    if (Object.keys(changed).length) edits[engine.name] = changed;
    else delete edits[engine.name];
    renderEngines();
    updateDirty();
  } else if (sheet.revertTo !== undefined) {
    draft.engine = sheet.revertTo;
    renderEngines();
    updateDirty();
  }
  sheet = null;
  const backdrop = $("sheet");
  backdrop.classList.remove("open");
  setTimeout(() => { if (!sheet) backdrop.hidden = true; }, 220);
}

$("sheetCancel").addEventListener("click", () => closeSheet(false));
$("sheetDone").addEventListener("click", () => closeSheet(true));
$("sheetDoc").addEventListener("click", () => openDoc(sheet.engine.doc));
$("sheet").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) closeSheet(false); });
$("sheet").addEventListener("keydown", (ev) => {
  if (ev.key === "Escape") { ev.preventDefault(); closeSheet(false); }
  if (ev.key === "Enter" && ev.target.tagName === "INPUT") { ev.preventDefault(); closeSheet(true); }
});

// ---- save ----

function readForm() {
  draft.enabled = form.elements.enabled.checked;
  draft.pushToTalk = form.elements.pushToTalk.value;
  draft.device = form.elements.device.value;
  draft.continuousEnabled = form.elements.continuousEnabled.checked;
  draft.continuousDevice = form.elements.continuousDevice.value;
  draft.continuousHotkey = form.elements.continuousHotkey.value;
  draft.textOutput = form.elements.textOutput.value;
  draft.hotwordsFile = form.elements.hotwordsFile.value.trim();
  draft.streamAddr = form.elements.streamAddr.value.trim();
  draft.language = form.elements.language.value;
  draft.stopDelayMs = Number.parseInt(form.elements.stopDelayMs.value, 10) || 0;
  draft.overlayPosition = form.elements.overlayPosition.value;
}

function isDirty() {
  if (!saved) return false;
  if (JSON.stringify(draft) !== JSON.stringify(saved.settings)) return true;
  return Object.entries(edits).some(([name, values]) => {
    const engine = saved.engines.find((e) => e.name === name);
    return Object.entries(values).some(([k, v]) => engine.fields.find((f) => f.key === k).value !== v);
  });
}

// After a save the outcome message stays; an edit replaces it.
function updateDirty({ message = true } = {}) {
  const dirty = isDirty();
  $("save").disabled = !dirty || saving;
  $("revert").disabled = !dirty || saving;
  if (!message) return;
  if (dirty) setMessage("有未保存的修改", "");
  else if ($("message").dataset.kind === "") setMessage("", "");
}

function setMessage(text, kind) {
  const m = $("message");
  m.textContent = text;
  m.title = text;
  m.className = kind ? `message ${kind}` : "message";
  m.dataset.kind = kind;
}

async function save() {
  readForm();
  if (!isDirty() || saving) return;
  saving = true;
  updateDirty({ message: false });
  try {
    const res = await api("/api/settings", { ...draft, engineConfigs: edits });
    applyState(res.state);
    if (res.warning) setMessage(res.warning, "warn");
    else setMessage("已保存", "ok");
  } catch (err) {
    setMessage(err.message, "error");
    if (/热键/.test(err.message)) {
      showPane("general");
      showHotkeyError(err.message);
    }
  } finally {
    saving = false;
    updateDirty({ message: false });
  }
}

function openDoc(url) {
  if (saved.shell === "app") api("/api/open", { url }).catch((err) => setMessage(err.message, "error"));
  else window.open(url, "_blank", "noopener");
}

form.addEventListener("input", (ev) => {
  if (!saved || ev.target.id === "hotwordInput") return;
  readForm();
  if (ev.target.name === "pushToTalk") {
    showHotkeyError("");
    renderSlots();
  }
  updateDirty();
});
form.addEventListener("change", (ev) => {
  if (!saved || ev.target.id === "hotwordInput") return;
  readForm();
  updateDirty();
});
form.addEventListener("submit", (ev) => { ev.preventDefault(); save(); });
$("save").addEventListener("click", save);
$("revert").addEventListener("click", () => { applyState(saved); setMessage("", ""); });
document.addEventListener("keydown", (ev) => {
  if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === "s") {
    ev.preventDefault();
    save();
  }
});

// ---- live status and activity ----

// The voice log is plain text written for the terminal: a leading emoji
// marks the kind of line, "\r…" is a streaming partial, and the final
// transcript is "🎤 最终: …".
const LEVELS = [["❌", "error"], ["⚠️", "warn"], ["✅", "ok"], ["📋", "ok"], ["🎤", "info"]];

function logItem(entry, isLast) {
  const time = el("time", {}, new Date(entry.time).toLocaleTimeString("zh-CN", { hour12: false }));
  const partial = entry.text.startsWith("\r");
  let text = entry.text.trim();
  let level = "info";
  for (const [mark, lv] of LEVELS) {
    if (text.startsWith(mark)) {
      level = lv;
      text = text.slice(mark.length).trim();
      break;
    }
  }
  if (partial) {
    if (!isLast) return null; // superseded by what came after it
    return el("li", { class: "live" }, time, el("span", { class: "lv" }), el("span", { class: "msg" }, `识别中：${text}`));
  }
  const final = finalTranscript(text);
  if (final !== null) {
    return el("li", { class: "ok" }, time, el("span", { class: "lv" }),
      el("span", { class: "transcript" }, el("span", {}, final), copyButton(final)));
  }
  return el("li", { class: level }, time, el("span", { class: "lv" }), el("span", { class: "msg" }, text));
}

let lastLogKey = null;

function renderLogs(logs) {
  const key = logs.length ? `${logs.length}:${logs[logs.length - 1].time}:${logs[logs.length - 1].text}` : "";
  if (key === lastLogKey) return;
  lastLogKey = key;
  const list = $("logs");
  const atBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 8;
  const items = logs.map((e, i) => logItem(e, i === logs.length - 1)).filter(Boolean);
  if (!items.length) items.push(el("li", { class: "empty" }, "还没有记录。按下热键开始说话后，这里会显示每次识别的过程和结果。"));
  list.replaceChildren(...items);
  if (atBottom) list.scrollTop = list.scrollHeight;
}

function renderStatus({ status, stats, logs }) {
  const box = $("status");
  box.dataset.state = status.state;
  $("statusLabel").textContent = STATE_LABELS[status.state] || status.state;
  const parts = [];
  if (status.detail) parts.push(status.detail);
  if (status.stopInMs > 0) parts.push(`剩余 ${status.stopInMs}ms`);
  if (status.errorHoldS > 0) parts.push(`保留 ${status.errorHoldS}s`);
  if (status.pendingFinishes > 0) parts.push(`后台收尾 ${status.pendingFinishes}`);
  if (status.sessionId > 0) parts.push(`#${status.sessionId}`);
  if (status.debug) parts.push(...status.debug);
  $("statusDetail").textContent = parts.join("  ");
  box.title = parts.join("  ");
  // The server only starts after a save, so say what it is actually doing.
  $("streamState").textContent = status.streamUrl
    ? `监听中 ${status.streamUrl} · ${status.streamSubscribers} 个订阅者`
    : (draft && draft.streamAddr ? "未启动（保存后生效；地址被占用时见日志）" : "已关闭");

  $("statSessions").textContent = stats.sessions.toLocaleString();
  $("statChars").textContent = stats.chars.toLocaleString();
  $("statAvg").textContent = cpm(stats.chars, stats.audioMs);
  $("statLast").textContent = cpm(stats.lastChars, stats.lastAudioMs);
  renderLogs(logs);
}

async function poll() {
  if (!document.hidden) {
    try {
      renderStatus(await api("/api/status"));
    } catch {
      $("statusLabel").textContent = "未连接";
      $("status").dataset.state = "error";
      $("statusDetail").textContent = "";
    }
  }
  setTimeout(poll, 250);
}

async function boot() {
  let pane = PANES[0];
  try { pane = localStorage.getItem("justTalk.pane") || pane; } catch {}
  showPane(pane);
  try {
    applyState(await api("/api/state"));
  } catch (err) {
    $("loadError").textContent = `无法读取配置：${err.message}`;
    $("loadError").hidden = false;
  }
  poll();
}

// The tray panel can switch voice input while this page is open. Take the
// saved settings again on focus; with unsaved edits only the switch
// follows, and only if it was not edited here.
window.addEventListener("focus", async () => {
  if (!saved || saving || sheet) return;
  let fresh;
  try { fresh = await api("/api/state"); } catch { return; }
  if (!isDirty()) {
    applyState(fresh);
    return;
  }
  if (draft.enabled === saved.settings.enabled) {
    draft.enabled = fresh.settings.enabled;
    form.elements.enabled.checked = draft.enabled;
  }
  saved.settings.enabled = fresh.settings.enabled;
  updateDirty();
});

boot();
