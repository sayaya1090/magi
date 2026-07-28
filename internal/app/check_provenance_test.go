package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
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

// A worker spawns workers: a step that decomposes, or a delegate that delegates, puts the write two
// levels down while the gate runs at the top. Observed live — a worker's own child wrote
// /app/bug_analysis.md, the file a step-4 check reads, while the check ran in the main session. A
// one-level walk sees the parent and not the writer, so a record composed only in a grandchild would
// have looked like a program's real output.
func TestDescendantsReachEveryDepth(t *testing.T) {
	a := &App{states: map[session.SessionID]*sessionState{}}
	add := func(id, parent session.SessionID) {
		a.states[id] = &sessionState{meta: session.Session{ID: id, Parent: parent}}
	}
	add("main", "")
	add("w1", "main")
	add("w2", "w1")  // the grandchild that does the writing
	add("w3", "w2")  // …and one deeper still
	add("other", "") // a sibling tree that must not be swept in
	add("otherkid", "other")

	got := map[session.SessionID]bool{}
	for _, s := range a.descendantsOf("main") {
		got[s] = true
	}
	for _, want := range []session.SessionID{"w1", "w2", "w3"} {
		if !got[want] {
			t.Errorf("%s is under main and must be walked", want)
		}
	}
	for _, no := range []session.SessionID{"main", "other", "otherkid"} {
		if got[no] {
			t.Errorf("%s is not under main", no)
		}
	}
	if n := len(a.descendantsOf("w2")); n != 1 {
		t.Errorf("descendantsOf must be relative to its root, got %d for w2", n)
	}
	if n := len(a.descendantsOf("w3")); n != 0 {
		t.Errorf("a leaf has no descendants, got %d", n)
	}

	// Nothing enforces acyclicity in the recorded metadata, and a hang inside a check audit would be
	// an expensive way to learn that.
	a.states["w1"].meta.Parent = "w3"
	done := make(chan int, 1)
	go func() { done <- len(a.descendantsOf("main")) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a cycle in the parent chain must not spin the walk")
	}
}

// The audit's whole point is a DELEGATED step: the worker runs in its own session and the gate runs
// in the parent's, so the write it must find is never in the session it is asked about. That walk
// had no test — `authorsIn` was tested directly and the end-to-end test put the write in the gating
// session — and a live run then showed a worker's `write` of a file, a `nonempty` check on that
// file passing thirty-one seconds later, and no finding attached.
func TestPathAuthorsSeesAWriteInAChildAndAGrandchild(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	main, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	// Register a session the way spawnResolved does: meta with Parent, then its created fact.
	spawn := func(id, parent session.SessionID) {
		t.Helper()
		app.mu.Lock()
		app.stateLocked(id).meta = session.Session{ID: id, Workdir: wd, Agent: "worker", Parent: parent}
		app.mu.Unlock()
		cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "worker", Parent: string(parent)})
		if aerr := app.appendFact(ctx, id, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "worker"}, cd); aerr != nil {
			t.Fatal(aerr)
		}
	}
	const kid, grandkid = session.SessionID("s_kid"), session.SessionID("s_grandkid")
	spawn(kid, main)
	spawn(grandkid, kid)

	// The live shape: the check names a workspace-relative path, the tool call an absolute one.
	if _, aerr := app.store.Append(ctx, kid,
		toolCallEvent("write", `{"path":"/app/docs/summary.txt","content":"Build Process Summary\n"}`)); aerr != nil {
		t.Fatal(aerr)
	}
	if _, aerr := app.store.Append(ctx, grandkid,
		toolCallEvent("bash", `{"command":"echo 'All tests passed' > /app/logs/test.log"}`)); aerr != nil {
		t.Fatal(aerr)
	}

	got := app.pathAuthors(ctx, main, "docs/summary.txt")
	if len(got) != 1 || got[0].tool != "write" {
		t.Fatalf("a child's write must be visible from the gating session, got %+v", got)
	}
	if deep := app.pathAuthors(ctx, main, "logs/test.log"); len(deep) != 1 || deep[0].tool != "bash" {
		t.Fatalf("a grandchild's composed redirect must be visible too, got %+v", deep)
	}
	// A path nobody wrote stays clean — the audit must not report on a real recording.
	if none := app.pathAuthors(ctx, main, "logs/build.log"); len(none) != 0 {
		t.Errorf("an unauthored path must yield no authors, got %+v", none)
	}
	// Asked about the child itself, the parent's siblings are irrelevant.
	if own := app.pathAuthors(ctx, kid, "logs/test.log"); len(own) != 1 {
		t.Errorf("a session's own subtree is what it sees, got %+v", own)
	}
}
