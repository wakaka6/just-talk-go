"use strict";

// The tray panel: live status, the latest transcript, usage statistics,
// switches for voice input and continuous transcription, and a read-only
// summary of the other saved settings. Editing happens in the settings
// window, which 打开设置 brings forward.

const MODE_LABELS = { toggle: "切换", hold: "按住" };
const OUTPUT_LABELS = { paste: "自动上屏", clipboard: "复制到剪贴板", none: "不输出" };

let state = null;
// The continuous switch shows the running state, which its hotkey can flip
// at any time, so every poll updates it unless a click is still in flight.
let continuousOn = null;
let continuousBusy = false;

function summaryRow(label, ...value) {
  return el("div", { class: "row" }, el("span", { class: "label" }, label), el("span", { class: "value" }, ...value));
}

function renderSummary() {
  const s = state.settings;
  const rows = [];
  const toggle = el("input", { type: "checkbox", class: "switch", "aria-label": "语音输入", onchange: () => setEnabled(toggle) });
  toggle.checked = s.enabled;
  rows.push(el("label", { class: "row" }, el("span", { class: "label" }, "语音输入"), el("span", { class: "value" }, toggle)));
  const cont = el("input", { type: "checkbox", class: "switch", id: "continuousSwitch", "aria-label": "持续转写", onchange: () => setContinuous(cont) });
  cont.checked = continuousOn ?? s.continuousEnabled;
  // It runs only while voice input is on.
  cont.disabled = !s.enabled;
  rows.push(el("label", { class: "row", title: s.enabled ? "" : "语音输入关闭时，持续转写不运行" },
    el("span", { class: "label" }, "持续转写"), el("span", { class: "value" }, cont)));
  const hotkeys = splitHotkeys(s.pushToTalk);
  rows.push(summaryRow("热键", ...(hotkeys.length
    ? hotkeys.flatMap((h, i) => [i ? el("span", { class: "muted" }, "或") : null, keycaps(h.split("+"), state.platform)])
    : [el("span", { class: "muted" }, "未设置")])));
  rows.push(summaryRow("模式", MODE_LABELS[s.mode] || s.mode));
  const engine = state.engines.find((e) => e.name === s.engine);
  rows.push(summaryRow("识别引擎", engine ? engine.title : s.engine,
    engine && !engine.configured ? el("span", { class: "badge warn" }, "需要配置") : null));
  const lang = state.languages.find((l) => l.code === s.language);
  rows.push(summaryRow("语言", lang ? lang.label : s.language));
  rows.push(summaryRow("识别后", OUTPUT_LABELS[s.textOutput] || s.textOutput));
  $("summary").replaceChildren(...rows);
}

async function setEnabled(toggle) {
  toggle.disabled = true;
  try {
    state = (await api("/api/enabled", { enabled: toggle.checked })).state;
  } catch {
    toggle.checked = !toggle.checked;
  }
  renderSummary();
}

async function setContinuous(toggle) {
  toggle.disabled = continuousBusy = true;
  try {
    state = (await api("/api/continuous", { enabled: toggle.checked })).state;
    continuousOn = toggle.checked;
  } catch {
    toggle.checked = !toggle.checked;
  }
  continuousBusy = false;
  renderSummary();
}

let lastKey = null;

function renderLast(status, logs) {
  // A streaming partial while recording, else the latest final transcript.
  const tail = logs[logs.length - 1];
  let live = null;
  if (tail && tail.text.startsWith("\r") && status.state !== "idle") live = tail.text.trim().replace(/^🎤\s*/, "");
  let final = null;
  for (let i = logs.length - 1; i >= 0 && final === null; i--) final = finalTranscript(logs[i].text);
  const key = `${live}\u0000${final}`;
  if (key === lastKey) return;
  lastKey = key;
  const box = $("lastText");
  box.classList.toggle("live", live !== null);
  if (live !== null) box.replaceChildren(`识别中：${live}`);
  else if (final !== null) box.replaceChildren(el("span", {}, final), copyButton(final));
  else box.replaceChildren(el("span", { class: "muted" }, "还没有识别记录。按下热键开始说话。"));
}

function renderStatus({ status, stats, logs }) {
  $("status").dataset.state = status.state;
  $("statusLabel").textContent = STATE_LABELS[status.state] || status.state;
  $("statSessions").textContent = stats.sessions.toLocaleString();
  $("statChars").textContent = stats.chars.toLocaleString();
  $("statAvg").textContent = cpm(stats.chars, stats.audioMs);
  $("statLast").textContent = cpm(stats.lastChars, stats.lastAudioMs);
  const cont = $("continuousSwitch");
  if (cont && !continuousBusy && status.continuousOn !== continuousOn) {
    continuousOn = status.continuousOn;
    cont.checked = continuousOn;
  }
  renderLast(status, logs);
}

async function refreshState() {
  try {
    state = await api("/api/state");
    renderSummary();
  } catch {}
}

async function poll() {
  if (!document.hidden) {
    try {
      renderStatus(await api("/api/status"));
    } catch {
      $("statusLabel").textContent = "未连接";
      $("status").dataset.state = "error";
    }
  }
  setTimeout(poll, 300);
}

// Settings saved in the window show up the next time the panel opens.
window.addEventListener("focus", refreshState);
document.addEventListener("visibilitychange", () => { if (!document.hidden) refreshState(); });
$("settings").addEventListener("click", () => api("/api/window", { action: "settings" }).catch(() => {}));
$("quit").addEventListener("click", () => api("/api/window", { action: "quit" }).catch(() => {}));

refreshState();
poll();
