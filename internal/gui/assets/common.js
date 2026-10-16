"use strict";

// Helpers shared by the settings page (app.js) and the tray panel (panel.js).

const $ = (id) => document.getElementById(id);

const STATE_LABELS = {
  idle: "待机",
  connecting: "连接中",
  recording: "录音中",
  stopping_delayed: "延迟停止",
  stopping: "停止中",
  error: "错误",
};

async function api(path, body) {
  const res = await fetch(path, body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = res.status === 204 ? {} : await res.json();
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
    else if (v !== false && v != null) node.setAttribute(k, v === true ? "" : v);
  }
  node.append(...children.filter((c) => c != null));
  return node;
}

function keyLabel(name, platform) {
  const mac = platform === "darwin";
  const labels = {
    Ctrl: mac ? "⌃ Control" : "Ctrl",
    Alt: mac ? "⌥ Option" : "Alt",
    Shift: mac ? "⇧ Shift" : "Shift",
    Super: mac ? "⌘ Command" : platform === "windows" ? "Win" : "Super",
    Fn: "fn", Up: "↑", Down: "↓", Left: "←", Right: "→", Tab: "⇥ Tab",
    CapsLock: "⇪ Caps Lock", Enter: "↩ Enter", Backspace: "⌫", Escape: "Esc", Menu: "☰ Menu",
  };
  return labels[name] || name;
}

function keycaps(names, platform) {
  return el("span", { class: "keycaps" }, ...names.map((n) => el("kbd", {}, keyLabel(n, platform))));
}

// Splits like config.ParseHotkeyList: a comma right after "+" is the key.
function splitHotkeys(s) {
  const out = [];
  let cur = "";
  for (const ch of s) {
    if ((ch === "," || ch === "，") && !cur.trimEnd().endsWith("+")) {
      if (cur.trim()) out.push(cur.trim());
      cur = "";
    } else {
      cur += ch;
    }
  }
  if (cur.trim()) out.push(cur.trim());
  return out;
}

// Characters per minute of recorded audio.
function cpm(chars, ms) {
  return ms > 0 ? Math.round(chars / (ms / 60000)) : 0;
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // The app's custom scheme may not count as a secure context.
    const area = el("textarea", { style: "position:fixed;opacity:0" });
    area.value = text;
    document.body.append(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

// The final transcript in a voice log line ("🎤 最终: …"), or null.
function finalTranscript(line) {
  const m = /^(?:🎤)?\s*最终[:：]\s*(.*)$/s.exec(line.trim());
  return m ? m[1] : null;
}

function copyButton(text) {
  const button = el("button", { type: "button", class: "ghost", onclick: async () => {
    button.textContent = (await copyText(text)) ? "已复制" : "复制失败";
    setTimeout(() => { button.textContent = "复制"; }, 1200);
  } }, "复制");
  return button;
}
