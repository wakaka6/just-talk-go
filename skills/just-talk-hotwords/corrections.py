#!/usr/bin/env python3
"""Pair each dictation Just Talk typed with the message the user then sent to
an agent, and print the words the user fixed by hand.

usage: python3 corrections.py <just-talk config.toml>

Each run covers the dictations since the previous run and moves the cursor
forward, so run it once per round. Dictations from the last few minutes are
left for the next round: the user may still be editing that message.
"""

import difflib
import glob
import json
import os
import re
import sys
import time
from datetime import datetime

SETTLE = 180  # seconds a dictation waits for the message it ends up in
REPLY = 600  # how long after a dictation its message may be sent
FIRST_LOOKBACK = 3600  # window of the first run, when there is no cursor
MIN_COVER = 0.6  # share of the dictation that must survive into the message
MAX_FIX = 24  # longer replacements are pasted text, not a fixed word

SESSION_GLOBS = [
    "~/.claude/projects/*/*.jsonl",
    "~/.codex/sessions/**/*.jsonl",
    "~/.pi/agent/sessions/*/*.jsonl",
    "~/.omp/agent/sessions/**/*.jsonl",
    "~/.factory/sessions/**/*.jsonl",
    "~/.copilot/session-state/*/events.jsonl",
]

OUTPUT_MARKERS = ("📋 已复制到剪贴板", "✅ 已上屏", "📝 已转写")
TRIVIAL = re.compile(r"^[\s\W_]*$")


def state_dir():
    if os.name == "nt":
        return os.path.join(os.environ["LOCALAPPDATA"], "just-talk")
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "just-talk")


def parse_time(value):
    if isinstance(value, (int, float)):
        return value / 1000 if value > 1e11 else float(value)
    if not isinstance(value, str):
        return None
    # Older Pythons reject "Z" and fractions that are not 3 or 6 digits.
    value = value.replace("Z", "+00:00")
    value = re.sub(r"\.(\d+)", lambda m: "." + (m.group(1) + "000000")[:6], value)
    try:
        return datetime.fromisoformat(value).timestamp()
    except ValueError:
        return None


def dictations(path, since, until):
    """Yield (time, text) for every dictation whose output landed in the window.

    A dictation runs from "开始录音" to its output marker; its text is the
    last "最终" line before that marker. Cancelled ones have no marker.
    """
    text = None
    with open(path, encoding="utf-8") as f:
        for line in f:
            try:
                entry = json.loads(line)
            except ValueError:
                continue
            msg = entry.get("text", "").strip()
            if msg.startswith("🎤 开始录音"):
                text = None
            elif msg.startswith("🎤 最终:"):
                text = msg[len("🎤 最终:"):].strip()
            elif msg.startswith(OUTPUT_MARKERS) and text:
                t = parse_time(entry.get("time"))
                if t is not None and since < t <= until:
                    yield t, text
                text = None


def message_text(entry):
    """Return (time, text) of a user message in any of the agents' JSONL
    formats, or None."""
    node = entry.get("message") or entry.get("payload") or entry.get("data") or entry
    if not isinstance(node, dict):
        return None
    if node.get("role") != "user" and entry.get("type") not in ("user", "user.message"):
        return None
    content = node.get("content", node.get("message"))
    if isinstance(content, str):
        text = content
    elif isinstance(content, list):
        text = " ".join(
            c.get("text", "")
            for c in content
            if isinstance(c, dict) and c.get("type") in ("text", "input_text")
        )
    else:
        return None
    text = text.strip()
    t = parse_time(node.get("timestamp")) or parse_time(entry.get("timestamp"))
    if not text or t is None:
        return None
    return t, text


def user_messages(files, since):
    for path in files:
        try:
            f = open(path, encoding="utf-8", errors="ignore")
        except OSError:
            continue
        with f:
            for line in f:
                if '"user' not in line:
                    continue
                try:
                    found = message_text(json.loads(line))
                except (ValueError, AttributeError):
                    continue
                if found and found[0] >= since:
                    yield found[0], found[1], path


def fixes(heard, sent):
    """Replacements the user made, skipping punctuation and pasted text."""
    out = []
    ops = difflib.SequenceMatcher(None, heard, sent, autojunk=False).get_opcodes()
    for tag, a, b, c, d in ops:
        old, new = heard[a:b].strip(), sent[c:d].strip()
        if tag != "replace" or TRIVIAL.match(old) or TRIVIAL.match(new):
            continue
        if len(old) <= MAX_FIX and len(new) <= MAX_FIX:
            out.append((old, new))
    return out


def coverage(heard, sent):
    blocks = difflib.SequenceMatcher(None, heard, sent, autojunk=False).get_matching_blocks()
    return sum(b.size for b in blocks) / len(heard)


def clock(t):
    return datetime.fromtimestamp(t).strftime("%m-%d %H:%M:%S")


def first_time(path):
    """Time of the oldest entry still in the activity log, which is capped."""
    with open(path, encoding="utf-8") as f:
        for line in f:
            try:
                return parse_time(json.loads(line).get("time"))
            except ValueError:
                continue
    return None


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__.strip())
    name = os.path.splitext(os.path.basename(sys.argv[1]))[0]
    sdir = state_dir()
    activity = os.path.join(sdir, name + ".activity.jsonl")
    cursor_path = os.path.join(sdir, name + ".hotwords-cursor")

    now = time.time()
    try:
        with open(cursor_path) as f:
            since = float(f.read().strip())
    except (OSError, ValueError):
        since = now - FIRST_LOOKBACK
    until = now - SETTLE

    has_log = os.path.exists(activity)
    heard = list(dictations(activity, since, until)) if has_log else []
    files = sorted(
        {
            p
            for g in SESSION_GLOBS
            for p in glob.glob(os.path.expanduser(g), recursive=True)
            if os.path.getmtime(p) >= since
        }
    )
    msgs = list(user_messages(files, since - 5)) if heard else []

    print("# window %s – %s" % (clock(since), clock(until)))
    oldest = first_time(activity) if has_log else None
    if oldest and oldest > since:
        print("# gap: the activity log only goes back to %s; earlier dictations are gone" % clock(oldest))
    for t, text in heard:
        cands = [m for m in msgs if t - 5 <= m[0] <= t + REPLY]
        best = max(cands, key=lambda m: coverage(text, m[1]), default=None)
        print("dictation %s" % clock(t))
        print("  heard: %s" % text)
        if best is None or coverage(text, best[1]) < MIN_COVER:
            print("  sent:  (no matching message)")
            continue
        found = fixes(text, best[1])
        print("  sent to: %s" % best[2])
        if found:
            print("  fixed: %s" % "; ".join("%s → %s" % f for f in found))
    print("# sessions updated")
    for p in files:
        print(p)

    if until > since:
        os.makedirs(sdir, exist_ok=True)
        tmp = cursor_path + ".tmp"
        with open(tmp, "w") as f:
            f.write("%f\n" % until)
        os.replace(tmp, cursor_path)


if __name__ == "__main__":
    main()
