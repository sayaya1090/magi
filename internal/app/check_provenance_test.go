package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestSamePath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/app/out.log", "/app/out.log", true},
		{"/app/./out.log", "/app/out.log", true},
		// The check names an absolute path; the tool call named it relative to the same workdir.
		{"/app/out.log", "out.log", true},
		{"out.log", "/app/out.log", true},
		{"/app/logs/out.log", "logs/out.log", true},
		{"/app/out.log", "/app/other.log", false},
		{"/app/out.log", "/app/xout.log", false}, // suffix must fall on a path boundary
		{"", "/app/out.log", false},
		{"/app/out.log", "", false},
	}
	for _, tc := range cases {
		if got := samePath(tc.a, tc.b); got != tc.want {
			t.Errorf("samePath(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// The whole judgement for bash turns on WHO produced the bytes. A real command's output redirected
// into the file is exactly the shape a check is supposed to read and must never be flagged; text the
// model composed is the fabrication.
func TestComposedRedirectTo(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"echo 'All tests passed' > /app/test.log", true},
		{"echo ok >> /app/test.log", true},
		{"printf 'Done\\n' > /app/test.log", true},
		{"cat > /app/test.log <<'EOF'\nAll tests passed\nEOF", true},
		{"cat <<EOF > /app/test.log\nAll tests passed\nEOF", true},
		// Genuine recordings.
		{"make test > /app/test.log 2>&1", false},
		{"python3 run.py > /app/test.log", false},
		{"./prog > /app/test.log", false},
		{"/usr/bin/make test > /app/test.log", false},
		{"cat other.log > /app/test.log", false}, // a copy, not a dictation
		{"make test > /app/other.log", false},    // a different file
		{"echo hi > /app/other.log", false},
		{"echo done", false}, // no redirect at all
		// A `>` inside quotes is data, not a redirect.
		{"grep 'a > b' input.txt > /app/test.log", false},
		// An env prefix does not become the producer.
		{"FOO=1 echo ok > /app/test.log", true},
	}
	for _, tc := range cases {
		if got := composedRedirectTo(tc.cmd, "/app/test.log"); got != tc.want {
			t.Errorf("composedRedirectTo(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestAuthoredBy(t *testing.T) {
	cases := []struct {
		name, tool, args string
		wantOK           bool
		wantIn           string
	}{
		{"write", "write", `{"path":"/app/test.log","content":"All tests passed\n"}`, true, "All tests passed"},
		{"write elsewhere", "write", `{"path":"/app/src.c","content":"All tests passed\n"}`, false, ""},
		{"edit", "edit", `{"path":"/app/test.log","old":"x","new":"All tests passed"}`, true, "All tests passed"},
		{"multiedit", "multiedit", `{"path":"/app/test.log","edits":[{"old":"a","new":"All tests"},{"old":"b","new":"passed"}]}`, true, "passed"},
		{"bash composing", "bash", `{"command":"echo 'All tests passed' > /app/test.log"}`, true, "All tests passed"},
		{"bash recording", "bash", `{"command":"make test > /app/test.log 2>&1"}`, false, ""},
		// A read of the file says nothing about how it came to exist.
		{"read", "read", `{"path":"/app/test.log"}`, false, ""},
		{"garbage args", "write", `not json`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := authoredBy(tc.tool, json.RawMessage(tc.args), "/app/test.log")
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tc.wantOK, got)
			}
			if ok && !strings.Contains(got.text, tc.wantIn) {
				t.Fatalf("text = %q, want it to carry %q", got.text, tc.wantIn)
			}
		})
	}
}

// toolCallEvent builds the one event shape the audit reads: a tool CALL with its arguments, which is
// what the model composed. The runtime writes these as a side effect of granting the call, which is
// why they cannot be edited by the thing being audited.
func toolCallEvent(tool, args string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: "c1", Name: tool, Args: json.RawMessage(args),
		}},
	})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func TestAuthorsIn(t *testing.T) {
	evs := []event.Event{
		toolCallEvent("bash", `{"command":"make test > /app/test.log 2>&1"}`),
		toolCallEvent("read", `{"path":"/app/test.log"}`),
		toolCallEvent("write", `{"path":"/app/notes.md","content":"All tests passed"}`),
		toolCallEvent("write", `{"path":"/app/test.log","content":"All tests passed\n"}`),
	}
	got := authorsIn(evs, "/app/test.log")
	if len(got) != 1 || got[0].tool != "write" || !strings.Contains(got[0].text, "All tests passed") {
		t.Fatalf("authorsIn = %+v, want the one write that authored the source", got)
	}
	if len(authorsIn(evs, "/app/other.log")) != 0 {
		t.Fatal("a path nothing wrote must yield no authors — a missing record is not a fabrication")
	}
	if len(authorsIn(nil, "/app/test.log")) != 0 {
		t.Fatal("no events must yield no authors")
	}
}
