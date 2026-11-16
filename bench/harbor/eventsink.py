"""The split of magi's `--output json` stream into a machine file and a readable one.

Its own module because the adapter beside it imports harbor, which lives in harbor's
own venv — a test that had to import the adapter to reach this could not run from the
repo. Nothing here needs harbor.
"""

import json
import re
from typing import IO


# How much of a tool result the readable transcript keeps. The full value is in the
# events file; this is only so a human can skim what came back.
_RESULT_CLIP = 220


class EventSink:
    """Splits magi's `--output json` stream into a machine file and a readable one.

    Two consumers want different things from the same stream. Post-hoc analysis wants
    every event with its timestamp; a person dissecting a wave wants the transcript that
    text mode used to print. Writing both from one pass costs nothing and means turning
    the events on does not take the readable log away.

    Deltas are the exception: a long task emits one per output token (114,802 in a single
    observed compcert run) and they say nothing individually. Only the FIRST of each
    message is kept — that one is the time-to-first-token, which is exactly the number
    that separates prefill from decode, and it is the reason the events file exists. The
    rest are dropped rather than written and stripped later.

    Chunks arrive split anywhere, including mid-line, so text is buffered to newlines.
    Anything that is not a JSON event is stderr (harbor merges the streams) and goes to
    the transcript verbatim — a crash message must not be lost to a parse failure.
    """

    def __init__(self, events: IO[str], transcript: IO[str]):
        self._events = events
        self._transcript = transcript
        self._buf = ""
        self._seen_delta: set[str] = set()

    def feed(self, text: str) -> None:
        self._buf += text
        while "\n" in self._buf:
            line, self._buf = self._buf.split("\n", 1)
            self._line(line)
        self._events.flush()
        self._transcript.flush()

    def close(self) -> None:
        if self._buf:
            self._line(self._buf)
            self._buf = ""
        self._events.flush()
        self._transcript.flush()

    def _line(self, line: str) -> None:
        s = line.strip()
        if not s:
            return
        ev = None
        if s.startswith("{"):
            try:
                ev = json.loads(s)
            except ValueError:
                ev = None
        if not isinstance(ev, dict) or "type" not in ev:
            self._transcript.write(line + "\n")  # stderr, or a line magi did not emit as JSON
            return
        if ev.get("type") == "part.delta":
            mid = str(ev.get("data", {}).get("messageId", ""))
            if mid in self._seen_delta:
                return
            self._seen_delta.add(mid)
        self._events.write(s + "\n")
        rendered = self._render(ev)
        if rendered:
            self._transcript.write(rendered + "\n")

    @staticmethod
    def _render(ev: dict) -> str:
        """One transcript line for an event, or "" for events a reader does not need.

        Mirrors what text mode printed: assistant text, each tool call with its
        arguments, and a clipped result. Reasoning is left out for the same reason text
        mode left it out — it is transient thinking, and the events file has it.
        """
        t = ev.get("type")
        d = ev.get("data") or {}
        if t == "part.appended":
            p = d.get("part") or {}
            kind = p.get("kind")
            if kind == "text":
                return (p.get("text") or "").rstrip("\n")
            if kind == "tool-call":
                c = p.get("toolCall") or {}
                return f"⚙ {c.get('name', '?')} {json.dumps(c.get('args'), ensure_ascii=False)}"
            if kind == "tool-result":
                r = p.get("toolResult") or {}
                body = r.get("content")
                text = body if isinstance(body, str) else json.dumps(body, ensure_ascii=False)
                clipped = text[:_RESULT_CLIP] + ("…" if len(text) > _RESULT_CLIP else "")
                return "  ✓ " + clipped.replace("\n", "\\n")
            return ""
        if t == "council.decided":
            return f"⚖ council: {d.get('decision', '?')} {json.dumps(d.get('tally'), ensure_ascii=False)}"
        if t == "error":
            return f"error: {d.get('message', json.dumps(d, ensure_ascii=False))}"
        if t == "turn.finished":
            return f"— turn finished {json.dumps(d.get('usage'), ensure_ascii=False)}"
        return ""


# What a run spent, as magi prints it on stderr when it ends (cmd/magi runHeadless): every request,
# council polls and subagents included. The streamed exec merges stderr into the transcript.
_TOTALS = re.compile(r"magi: tokens in (\d+) / out (\d+)(?: \u00b7 cost \$([0-9.]+))?")


def usage_from_logs(transcript, events):
    """Token fields for harbor's AgentContext from one trial's logs.

    Input, output and cost come from the last totals line. That line has no cached count, so the
    cached tokens are summed from turn.finished usage (the agent's own stream: a lower bound when
    councils or subagents also read from cache). A run killed before it printed totals falls back to
    the turns it finished. Nothing spent leaves every field None (unknown), never 0.
    """
    last = None
    for m in _TOTALS.finditer(transcript or ""):
        last = m
    turns = s_in = s_out = s_cached = 0
    cache_seen = False
    for ln in events:
        try:
            e = json.loads(ln)
        except (ValueError, TypeError):
            continue
        if not isinstance(e, dict) or e.get("type") != "turn.finished":
            continue
        u = (e.get("data") or {}).get("usage") or {}
        turns += 1
        s_in += int(u.get("in") or 0)
        s_out += int(u.get("out") or 0)
        if "cached" in u:
            cache_seen = True
            s_cached += int(u.get("cached") or 0)
    n_in = n_out = cost = None
    if last:
        n_in, n_out = int(last.group(1)), int(last.group(2))
        cost = float(last.group(3)) if last.group(3) else None
    elif turns:
        n_in, n_out = s_in, s_out
    return {"n_input_tokens": n_in, "n_output_tokens": n_out,
            "n_cache_tokens": s_cached if cache_seen else None, "cost_usd": cost}
