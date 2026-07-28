package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func bashPair(id, cmd, result string) (event.Event, event.Event) {
	cd, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: id, Name: "bash", Args: jsonOf(map[string]string{"command": cmd})}},
	})
	rd, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleUser,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: id, Content: jsonOf(result)}},
	})
	return event.Event{Type: event.TypePartAppended, Data: cd},
		event.Event{Type: event.TypePartAppended, Data: rd}
}

// The record separates four things a contract could not: work that ran clean, work that ran and
// failed, work whose status magi never learned, and inspection that exercised nothing at all.
func TestObserveSeparatesWorkFromInspection(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	add := func(a, b event.Event) {
		t.Helper()
		if _, e := app.store.Append(ctx, sid, a); e != nil {
			t.Fatal(e)
		}
		if _, e := app.store.Append(ctx, sid, b); e != nil {
			t.Fatal(e)
		}
	}
	add(bashPair("c1", "ls -la /app", "exit 0\n"))                                           // inspection only
	add(bashPair("c2", "cat build.log | head -20", "exit 0\n"))                              // inspection only
	add(bashPair("c3", "make world opt", "exit 0\n"))                                        // ran clean
	add(bashPair("c4", "make -C testsuite one DIR=tests/basic", "exit 2\n"))                 // ran and failed
	add(bashPair("c5", "make world opt 2>&1 | tee build.log | tail -50", "exit 0\n"))        // status is tail's
	add(bashPair("c6", "cd /app && ./sim 208 > out.txt", "started background command bg_1")) // no exit

	o := app.observe(ctx, sid)
	if n := len(o.ran()); n != 2 {
		t.Fatalf("ran() must hold only the exercising commands with a readable status, got %d: %+v", n, o.ran())
	}
	if !o.succeeded() {
		t.Error("a clean `make world opt` is a success magi observed")
	}
	if o.thin() {
		t.Error("a run with real commands and a written path is not thin")
	}
	// The tee target and the redirect are both paths this run wrote.
	if got := strings.Join(o.changed, ","); !strings.Contains(got, "build.log") || !strings.Contains(got, "out.txt") {
		t.Errorf("changed must hold what the commands wrote, got %q", got)
	}

	r := o.render()
	for _, want := range []string{
		"WHAT MAGI OBSERVED",
		"ran clean: make world opt",
		"ran and FAILED",
		"exit 2",
		"status unknown",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("render missing %q:\n%s", want, r)
		}
	}
	if strings.Contains(r, "ls -la /app") {
		t.Errorf("inspection must not be listed as a run:\n%s", r)
	}
}

// The question a termination hook asks: is there anything here worth calling work? Inspection and
// an unreadable status are not — counting `ls` as verification is the churn this exists to see
// through.
func TestThinRecord(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !app.observe(ctx, sid).thin() {
		t.Error("a session with no calls at all is thin")
	}
	for _, tc := range []struct {
		name, cmd, res string
		thin           bool
	}{
		{"inspection only", "ls -la && cat README", "exit 0\n", true},
		{"masked status", "make test 2>&1 | tail -5", "exit 0\n", true},
		{"failed but real", "pytest -q", "exit 1\n", false},
		{"clean and real", "pytest -q", "exit 0\n", false},
	} {
		a2 := newShellApp(t, &shellPlatform{})
		s2, err := a2.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		c, r := bashPair("c", tc.cmd, tc.res)
		if _, e := a2.store.Append(ctx, s2, c); e != nil {
			t.Fatal(e)
		}
		if _, e := a2.store.Append(ctx, s2, r); e != nil {
			t.Fatal(e)
		}
		if got := a2.observe(ctx, s2).thin(); got != tc.thin {
			t.Errorf("%s: thin() = %v, want %v", tc.name, got, tc.thin)
		}
	}
}

// A delegated step's work happens in a child session; the record has to reach across it or a
// delegate-heavy turn reads as empty — the same shape that made the parent's token totals look
// like nothing while the run cost the most.
func TestObserveReachesChildren(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	parent, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	const kid = session.SessionID("s_kid")
	app.mu.Lock()
	app.stateLocked(kid).meta = session.Session{ID: kid, Workdir: wd, Agent: "worker", Parent: parent}
	app.mu.Unlock()
	cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "worker", Parent: string(parent)})
	if e := app.appendFact(ctx, kid, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "worker"}, cd); e != nil {
		t.Fatal(e)
	}
	c, r := bashPair("c1", "go test ./...", "exit 0\n")
	if _, e := app.store.Append(ctx, kid, c); e != nil {
		t.Fatal(e)
	}
	if _, e := app.store.Append(ctx, kid, r); e != nil {
		t.Fatal(e)
	}
	if app.observe(ctx, parent).thin() {
		t.Error("a child's work is the parent's record too")
	}
	if !app.observe(ctx, parent).succeeded() {
		t.Error("the child's clean run must be visible from the parent")
	}
}

// The finish seam is where the built-in record and the workspace's configured Stop hooks meet: both
// answer "is there a reason not to end here", one from magi's own record, one from team procedure.
// The record carries what nothing else at that seam does — which commands magi could not determine
// the status of.
func TestStopRecordNamesWhatMagiCouldNotDetermine(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if rec := app.stopRecord(ctx, sid); rec != "" {
		t.Errorf("a session with no record says nothing, got %q", rec)
	}
	c, r := bashPair("c1", "make world opt 2>&1 | tee build.log | tail -50", "exit 0\n")
	if _, e := app.store.Append(ctx, sid, c); e != nil {
		t.Fatal(e)
	}
	if _, e := app.store.Append(ctx, sid, r); e != nil {
		t.Fatal(e)
	}
	rec := app.stopRecord(ctx, sid)
	if !strings.Contains(rec, "status unknown") || !strings.Contains(rec, "tee build.log") {
		t.Errorf("a build whose exit belongs to its tail must be reported as undetermined, not clean:\n%s", rec)
	}
	if strings.Contains(rec, "ran clean") {
		t.Errorf("an exit that is not the command's own must never read as a clean run:\n%s", rec)
	}
}

// Live evidence that the turn's usage was NOT what it reported: a headless run printed
//
//	magi: tokens in 1721335 / out 4880     (UsageTotal — every request)
//	turn.finished {"usage":{"in":48519,"out":4880}}
//
// The outputs agree and the inputs differ 35-fold, which is the signature of turnUsage falling back
// to the old accounting: 48,519 is the LAST prompt, not the sum. The fallback fires when the meter
// attributed nothing to the session, so this pins the attribution itself under the wiring production
// actually uses — the provider is guarded before the App wraps it.
func TestUsageIsAttributedThroughAGuardedProvider(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	a.mu.Lock()
	a.llm = GuardProvider(&meterLLM{in: 1000, out: 10})
	a.mu.Unlock()

	ctx := ctxWithUsageSID(context.Background(), "s_main")
	for i := 0; i < 3; i++ {
		ch, err := a.providerFor(AgentSpec{}).StreamChat(ctx, port.ChatRequest{Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
	}
	total, mine := a.UsageTotal(), a.UsageFor("s_main")
	if total.In != 3000 {
		t.Fatalf("the grand total must count every request, got %d", total.In)
	}
	if mine.In != total.In {
		t.Errorf("a session's own requests must be attributed to it: session %d vs total %d "+
			"— an unattributed request makes turn.finished fall back to the last prompt", mine.In, total.In)
	}
}
