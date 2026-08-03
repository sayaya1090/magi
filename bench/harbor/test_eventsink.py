"""What the split has to preserve, and what it is allowed to drop.

The fixtures below are verbatim lines from a real `--output json` run (trimmed only
where a payload was long). They are here so the shape the sink parses is pinned to
something magi actually emitted, not to what this test's author assumed it emits.
"""

import io
import json

from bench.harbor.eventsink import EventSink

# Real lines. context.usage is the one this whole file exists for: it is transient, so
# it never reaches the session store, and it carries the per-step prompt size and the
# turn's cumulative output — the two numbers that say where a step's time went.
CONTEXT_USAGE = (
    '{"seq":0,"sessionId":"s_9be34de1","type":"context.usage","actor":{"kind":"agent",'
    '"id":"default"},"ts":"2026-08-03T01:37:46.982533482Z","stage":"execute",'
    '"data":{"tokens":79943,"window":240000,"percent":33.309,"outTokens":85006}}'
)
TOOL_CALL = (
    '{"seq":985,"sessionId":"s_9be34de1","type":"part.appended","actor":{"kind":"agent",'
    '"id":"default"},"ts":"2026-08-03T01:40:23.404784393Z","stage":"execute",'
    '"data":{"messageId":"m_3a50d48f","role":"assistant","part":{"id":"p_bb1a0d8d",'
    '"kind":"tool-call","toolCall":{"callId":"call_ee1806a4","name":"bash",'
    '"args":{"command":"cat /tmp/CompCert/cparser/pre_parser.ml | head -10","timeout":10}}}}}'
)
TOOL_RESULT = (
    '{"seq":992,"sessionId":"s_9be34de1","type":"part.appended","actor":{"kind":"agent",'
    '"id":"default"},"ts":"2026-08-03T01:40:29.210755071Z","stage":"execute",'
    '"data":{"messageId":"m_dfd3a1e2","role":"tool","part":{"id":"p_de20130a",'
    '"kind":"tool-result","toolResult":{"callId":"call_c04e625d","content":"exit 0\\noutput: ok"}}}}'
)
TOOL_STARTED = (
    '{"seq":0,"sessionId":"s_9be34de1","type":"tool.started","actor":{"kind":"agent",'
    '"id":"default"},"ts":"2026-08-03T01:33:19.949793868Z","stage":"execute",'
    '"data":{"callId":"call_ba2ac934","name":"bash"}}'
)


def run(chunks):
    ev, tr = io.StringIO(), io.StringIO()
    sink = EventSink(events=ev, transcript=tr)
    for c in chunks:
        sink.feed(c)
    sink.close()
    return ev.getvalue(), tr.getvalue()


def events_of(raw):
    return [json.loads(l) for l in raw.splitlines() if l.strip()]


def delta(mid, text):
    return json.dumps(
        {
            "seq": 0,
            "type": "part.delta",
            "ts": "2026-08-03T01:00:00Z",
            "data": {"messageId": mid, "kind": "text", "text": text},
        }
    )


def test_transient_events_survive():
    """The point of the exercise: the store keeps none of these, the events file keeps all."""
    raw, _ = run([CONTEXT_USAGE + "\n" + TOOL_STARTED + "\n"])
    kinds = [e["type"] for e in events_of(raw)]
    assert kinds == ["context.usage", "tool.started"]
    assert events_of(raw)[0]["data"]["tokens"] == 79943


def test_only_the_first_delta_of_each_message_is_kept():
    """The first delta is the time to first token. The rest are one per output token."""
    lines = [delta("m_a", "he"), delta("m_a", "ll"), delta("m_a", "o"), delta("m_b", "x")]
    raw, tr = run(["\n".join(lines) + "\n"])
    got = events_of(raw)
    assert [e["data"]["messageId"] for e in got] == ["m_a", "m_b"]
    assert [e["data"]["text"] for e in got] == ["he", "x"], "kept delta must be the FIRST"
    assert tr == "", "deltas say nothing individually; they stay out of the transcript"


def test_a_line_split_across_chunks_is_not_lost():
    """Chunks arrive from a stream and break anywhere, including mid-token."""
    whole = CONTEXT_USAGE + "\n"
    for cut in (1, 40, len(whole) // 2, len(whole) - 2):
        raw, _ = run([whole[:cut], whole[cut:]])
        assert len(events_of(raw)) == 1, f"lost or split at cut={cut}"


def test_a_final_line_without_its_newline_still_lands():
    raw, _ = run([CONTEXT_USAGE])  # killed mid-write: no trailing newline
    assert len(events_of(raw)) == 1


def test_non_json_goes_to_the_transcript_and_not_the_events():
    """Harbor merges stderr into stdout. A crash message must not die in a parser."""
    raw, tr = run(["⋯ thinking\n", "magi: panic: nil map\n", "{not json at all}\n"])
    assert raw == ""
    assert "panic: nil map" in tr and "⋯ thinking" in tr and "{not json at all}" in tr


def test_the_transcript_still_reads_like_the_text_mode_it_replaced():
    _, tr = run([TOOL_CALL + "\n" + TOOL_RESULT + "\n"])
    lines = tr.splitlines()
    assert lines[0].startswith("⚙ bash ")
    assert "pre_parser.ml" in lines[0]
    assert lines[1].startswith("  ✓ exit 0")
    assert "\n" not in lines[1], "a result keeps to one line"


def test_a_long_result_is_clipped_in_the_transcript_but_whole_in_the_events():
    body = "x" * 5000
    line = json.dumps(
        {
            "seq": 1,
            "type": "part.appended",
            "ts": "2026-08-03T01:00:00Z",
            "data": {"part": {"kind": "tool-result", "toolResult": {"content": body}}},
        }
    )
    raw, tr = run([line + "\n"])
    assert len(tr) < 400 and tr.rstrip().endswith("…")
    assert events_of(raw)[0]["data"]["part"]["toolResult"]["content"] == body


def test_a_structured_result_renders_without_blowing_up():
    """content is raw JSON, so it is not always a string (list is what `list` returns)."""
    line = json.dumps(
        {
            "seq": 1,
            "type": "part.appended",
            "ts": "2026-08-03T01:00:00Z",
            "data": {
                "part": {
                    "kind": "tool-result",
                    "toolResult": {"content": [{"name": "ocaml", "isDir": True}]},
                }
            },
        }
    )
    _, tr = run([line + "\n"])
    assert "ocaml" in tr
