package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// literalOf decides what the audit can even look for. Too eager and a two-character run matches
// everywhere (every authored file "contains" it, and every pass gets flagged); too shy and the real
// marker is never found.
func TestLiteralOf(t *testing.T) {
	cases := []struct{ pat, want string }{
		{"^All tests passed$", "All tests passed"},
		{"All 12 tests passed", "All 12 tests passed"},
		{"(error|Segmentation fault)", "Segmentation fault"}, // longest ordinary run wins
		{"Done\\.", "Done"},
		{"^ok$", ""},   // too short to be about this contract
		{".*", ""},     // no literal core at all
		{"\\d+", ""},   //
		{"^$", ""},     //
		{"a|b|c", ""},  // every run is one character
		{"", ""},       //
		{"  ok  ", ""}, // trimmed, then too short
	}
	for _, tc := range cases {
		if got := literalOf(tc.pat); got != tc.want {
			t.Errorf("literalOf(%q) = %q, want %q", tc.pat, got, tc.want)
		}
	}
}

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

// The audit reports one shape and only one: the asserted pattern appears verbatim in text the worker
// composed into the file the check reads. Everything else stays silent, because a wrong accusation
// costs more than a missed one at this stage.
func TestAuditSourceProvenance(t *testing.T) {
	evs := []event.Event{toolCallEvent("write", `{"path":"/app/test.log","content":"All 12 tests passed\n"}`)}
	as, _ := parseAssertion("matches ^All 12 tests passed$")

	note := auditFinding(authorsIn(evs, "/app/test.log"), "/app/test.log", as)
	if !strings.Contains(note, "PROVENANCE") || !strings.Contains(note, "write") {
		t.Fatalf("note = %q, want it to name the authoring call", note)
	}

	// A recorded output: same file, same pattern, but the bytes came from a program.
	rec := []event.Event{toolCallEvent("bash", `{"command":"make test > /app/test.log 2>&1"}`)}
	if n := auditFinding(authorsIn(rec, "/app/test.log"), "/app/test.log", as); n != "" {
		t.Fatalf("a genuine recording was flagged: %q", n)
	}
	// Authored, but not the asserted string — the worker wrote the file for some other reason.
	other := []event.Event{toolCallEvent("write", `{"path":"/app/test.log","content":"placeholder\n"}`)}
	if n := auditFinding(authorsIn(other, "/app/test.log"), "/app/test.log", as); n != "" {
		t.Fatalf("an unrelated authorship was flagged: %q", n)
	}
	// A pattern with no literal core cannot be looked for in typed text, and a liveness probe reads
	// the world rather than a file — neither is auditable.
	for _, a := range []string{"matches .*", "process_alive"} {
		pa, ok := parseAssertion(a)
		if !ok {
			t.Fatalf("parseAssertion(%q) failed", a)
		}
		if n := auditFinding(authorsIn(evs, "/app/test.log"), "/app/test.log", pa); n != "" {
			t.Fatalf("%q produced a finding: %q", a, n)
		}
	}
	// The verbs with NO pattern are the ones an audit keyed on patterns could never see, and
	// `nonempty` is the cheapest assertion to fake — any text at all satisfies it. There the
	// authorship alone is the finding, whatever the composed text happened to say. Observed live:
	// `echo "Bootstrap completed successfully - no crash in build" > /app/crash.log` flipping a
	// `crash.log nonempty` check to pass on a step whose deliverable was "bootstrap crash
	// reproduced", one second after the write.
	for _, a := range []string{"nonempty", "absent Traceback", "equals /app/expected.log"} {
		pa, ok := parseAssertion(a)
		if !ok {
			t.Fatalf("parseAssertion(%q) failed", a)
		}
		n := auditFinding(authorsIn(other, "/app/test.log"), "/app/test.log", pa)
		if n == "" {
			t.Errorf("%q on a composed file must be reported", a)
		} else if !strings.Contains(n, "came out of the reply") {
			t.Errorf("%q finding must say where the bytes came from: %s", a, n)
		}
		// …and a file nothing authored is never flagged, whatever the verb.
		if n := auditFinding(nil, "/app/test.log", pa); n != "" {
			t.Errorf("%q flagged a file with no author: %q", a, n)
		}
	}
}

// The audit reads every event of every session under the gate, so it must be asked once per
// (source, pattern) rather than once per gate cycle.
func TestProvAuditDoneMemoizes(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	sid := session.SessionID("s-memo")
	app.mu.Lock()
	app.states[sid] = &sessionState{meta: session.Session{ID: sid}}
	app.mu.Unlock()

	if app.provAuditDone(sid, "/app/a.log", "passed") {
		t.Fatal("first ask must run")
	}
	if !app.provAuditDone(sid, "/app/a.log", "passed") {
		t.Fatal("second ask must be memoized")
	}
	if app.provAuditDone(sid, "/app/b.log", "passed") {
		t.Fatal("a different source is a different question")
	}
	if app.provAuditDone(sid, "/app/a.log", "failed") {
		t.Fatal("a different pattern is a different question")
	}
}
