package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
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
// (source, pattern) rather than once per gate cycle — and the memo must hand back the FINDING, not
// merely the fact of having asked. A check is evaluated more than once by design (the delegate step
// gate runs it, then the incremental recorder runs it again) and each evaluation records its own
// event, so a memo that answered "" the second time would leave whichever record someone actually
// reads with no finding on it.
func TestProvAuditMemoizesTheFinding(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	sid := session.SessionID("s-memo")
	app.mu.Lock()
	app.states[sid] = &sessionState{meta: session.Session{ID: sid}}
	app.mu.Unlock()

	if _, asked := app.provAudit(sid, "/app/a.log", "passed"); asked {
		t.Fatal("first ask must run")
	}
	app.rememberProvAudit(sid, "/app/a.log", "passed", "PROVENANCE: …")
	f, asked := app.provAudit(sid, "/app/a.log", "passed")
	if !asked || f != "PROVENANCE: …" {
		t.Fatalf("the finding must survive the memo, got %q asked=%v", f, asked)
	}
	// "asked and found nothing" is an answer too, and it must not read as "never asked".
	app.rememberProvAudit(sid, "/app/clean.log", "passed", "")
	if f, asked := app.provAudit(sid, "/app/clean.log", "passed"); !asked || f != "" {
		t.Fatalf("a clean answer must be remembered as one, got %q asked=%v", f, asked)
	}
	for _, k := range [][2]string{{"/app/b.log", "passed"}, {"/app/a.log", "failed"}} {
		if _, asked := app.provAudit(sid, k[0], k[1]); asked {
			t.Errorf("%v is a different question", k)
		}
	}
}

// `nonempty` is where the provenance answer decides the VERDICT rather than annotating it. An
// assertion that only requires "something is here", on a file whose something came out of the
// reply, is a check that proves nothing either way — the documented meaning of 126. Observed live
// one second apart: `echo "Bootstrap completed successfully - no crash in build" > /app/crash.log`,
// then that check flipping to pass on a step whose deliverable was "bootstrap crash reproduced".
//
// Ungated rather than FAILED is what keeps the legitimate case whole: when the deliverable is the
// file the worker wrote, this refuses to credit it and refuses to reject it.
func TestNonemptyOnAComposedFileYieldsNoVerdict(t *testing.T) {
	skipOnWindows(t)
	wd := t.TempDir()
	if werr := os.WriteFile(filepath.Join(wd, "test.log"), []byte("All tests passed\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	app := newShellApp(t, &shellPlatform{})
	sid, err := app.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	// Absolute, because the check runner reaches the platform directly and the test platform runs
	// commands without a working directory. samePath still matches the relative spelling the tool
	// call used.
	c := council.DeliverableCheck{Step: "1", Deliverable: "tests run",
		Source: filepath.Join(wd, "test.log"), Assert: "nonempty"}

	// A file no tool authored: an ordinary pass.
	out, code := app.runCheck(context.Background(), sid, wd, c)
	if code != 0 {
		t.Fatalf("an unauthored file must pass: %d %s", code, out)
	}

	// The same file, now with the worker's own composing call in the session's record → no verdict.
	app.mu.Lock()
	app.states[sid].provAudited = nil // the audit is asked once per (source, pattern)
	app.mu.Unlock()
	if _, err := app.store.Append(context.Background(), sid,
		toolCallEvent("bash", `{"command":"echo 'All tests passed' > test.log"}`)); err != nil {
		t.Fatal(err)
	}
	out, code = app.runCheck(context.Background(), sid, wd, c)
	if code != 126 {
		t.Fatalf("a composed file gives `nonempty` nothing to prove: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "came out of the reply") {
		t.Errorf("the verdict must carry the reason: %s", out)
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

// End to end, in the shape a delegated step actually has: the worker writes the file in ITS session,
// the gate runs the check in the PARENT's. Live evidence said a `nonempty` check passed on such a
// file with no finding attached, and neither the audit's unit tests nor its end-to-end one covered
// this arrangement — they put the write in the gating session.
func TestNonemptyOnAChildsComposedFileYieldsNoVerdict(t *testing.T) {
	skipOnWindows(t)
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "docs", "summary.txt"), []byte("Build Process Summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	const kid = session.SessionID("s_worker")
	app.mu.Lock()
	app.stateLocked(kid).meta = session.Session{ID: kid, Workdir: wd, Agent: "worker", Parent: main}
	app.mu.Unlock()
	cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "worker", Parent: string(main)})
	if aerr := app.appendFact(ctx, kid, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "worker"}, cd); aerr != nil {
		t.Fatal(aerr)
	}
	if _, aerr := app.store.Append(ctx, kid, toolCallEvent("write",
		`{"path":"`+filepath.ToSlash(filepath.Join(wd, "docs", "summary.txt"))+`","content":"Build Process Summary\n"}`)); aerr != nil {
		t.Fatal(aerr)
	}

	c := council.DeliverableCheck{Step: "1", Deliverable: "build process summarized",
		Source: "docs/summary.txt", Assert: "nonempty"}
	out, code := app.runCheck(ctx, main, wd, c)
	if code != 126 {
		t.Fatalf("a file the WORKER composed gives `nonempty` nothing to prove: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "came out of the reply") {
		t.Errorf("the verdict must carry the reason: %s", out)
	}
}
