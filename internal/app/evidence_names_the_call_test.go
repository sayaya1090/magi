package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func evPromptUser() event.Event {
	d, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m1",
		Parts: []session.Part{{Kind: session.PartText, Text: "build it"}}})
	return event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}, Data: d}
}

func evCall(id, name string, args map[string]any) event.Event {
	a, _ := json.Marshal(args)
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: a}}})
	return event.Event{Type: event.TypePartAppended, Actor: event.Actor{Kind: event.ActorAgent, ID: "default"}, Data: d}
}

func evResult(id, out string, isErr bool) event.Event {
	c, _ := json.Marshal(out)
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr}}})
	return event.Event{Type: event.TypePartAppended, Actor: event.Actor{Kind: event.ActorAgent, ID: "default"}, Data: d}
}

// A result without its request is half a fact, and two very different calls can produce the same
// answer. Observed live (sqlite-with-gcov, 2026-07-30): the agent ran
//
//	ln -sf /app/sqlite/sqlite3 /usr/local/bin/sqlite3 && which sqlite3 && sqlite3 --version
//
// whose output is `/usr/local/bin/sqlite3` plus a version string — byte-for-byte what a bare
// `which sqlite3 && sqlite3 --version` prints. The block showed only the output, so a council member
// concluded, and told the agent, that "the sqlite3 command works only because
// /usr/local/bin/sqlite3 exists from before this task". It did not: the agent had created that
// symlink two and a half minutes earlier, and the call that did it was in the very block being read.
func TestEvidenceNamesTheCallNotJustItsAnswer(t *testing.T) {
	same := "/usr/local/bin/sqlite3\n3.50.4 2025-07-30 (64-bit)"
	evs := []event.Event{
		evPromptUser(),
		evCall("c1", "bash", map[string]any{"command": "ln -sf /app/sqlite/sqlite3 /usr/local/bin/sqlite3 && which sqlite3 && sqlite3 --version"}),
		evResult("c1", same, false),
		evCall("c2", "bash", map[string]any{"command": "which sqlite3 && sqlite3 --version"}),
		evResult("c2", same, false),
	}

	got := turnToolEvidence(evs, 8)
	if !strings.Contains(got, "ln -sf /app/sqlite/sqlite3 /usr/local/bin/sqlite3") {
		t.Errorf("the call that CREATED the symlink must be identifiable:\n%s", got)
	}
	lines := strings.Split(got, "\n- ")
	if len(lines) != 3 { // the reading-rule header, then the two evidence lines
		t.Fatalf("want a header and two evidence lines, got %d:\n%s", len(lines), got)
	}
	if lines[1] == lines[2] {
		t.Errorf("two different calls with the same output must not render identically:\n%s", got)
	}
	// The result is still there — naming the request does not displace the answer.
	if strings.Count(got, "3.50.4") != 2 {
		t.Errorf("both answers still appear:\n%s", got)
	}
}

// Which argument identifies a call depends on the tool: bash carries no path, so its command IS its
// identity; the file tools are named by what they pointed at. A tool whose arguments carry none of
// them still renders, just without a request.
func TestEvidenceArgsPicksWhatIdentifiesTheCall(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"bash", map[string]any{"command": "make world"}, "make world"},
		{"read", map[string]any{"path": "/app/run.py", "limit": 50}, "/app/run.py"},
		{"write", map[string]any{"path": "/app/f.c", "content": "int main(){}"}, "/app/f.c"},
		{"grep", map[string]any{"pattern": "TODO", "path": "/app"}, "TODO in /app"},
		{"grep", map[string]any{"pattern": "TODO"}, "TODO"},
		{"glob", map[string]any{"pattern": "**/*.c", "path": "/app/src"}, "**/*.c in /app/src"},
		{"webfetch", map[string]any{"url": "https://example.com"}, "https://example.com"},
		{"bash_output", map[string]any{"id": "bg_1"}, "bg_1"},
		{"council", map[string]any{"complete": true}, ""},
	} {
		a, _ := json.Marshal(c.args)
		if got := evidenceArgs(c.name, a); got != c.want {
			t.Errorf("%s: evidenceArgs = %q, want %q", c.name, got, c.want)
		}
	}
	// Malformed arguments must not panic or invent a request.
	if got := evidenceArgs("bash", json.RawMessage(`not json`)); got != "" {
		t.Errorf("unparseable arguments name nothing, got %q", got)
	}

	// A long command is clipped, not dropped — the line exists to say WHICH call this was.
	long := strings.Repeat("x", evidenceArgsCap*2)
	line := evidenceLine(toolCallBrief{name: "bash", args: long}, "ok", "done")
	if len(line) > evidenceArgsCap+200 {
		t.Errorf("the request is clipped hard: line is %d bytes", len(line))
	}
	if !strings.Contains(line, "tool bash [ok]") || !strings.HasSuffix(line, "done") {
		t.Errorf("clipping keeps the shape and the answer:\n%s", line)
	}
	// No request to show → the old shape, unchanged.
	if got := evidenceLine(toolCallBrief{name: "council"}, "ok", "accepted"); got != "tool council [ok]: accepted" {
		t.Errorf("a call with nothing to name renders as before, got %q", got)
	}
}
