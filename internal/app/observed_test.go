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

func jsonOf(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// A pipeline's exit belongs to its last stage, so `make … | tail` used to be filed as a status magi
// could not determine — and 59% of recorded bash calls are pipelines, so most of what it could not
// determine was this. The shell now reports every stage, and when the note says the head of the
// pipe failed, the record must say FAILED rather than shrug.
func TestAPipelineWhoseHeadFailedIsRecordedAsFailed(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	add := func(a, b event.Event) {
		t.Helper()
		for _, e := range []event.Event{a, b} {
			if _, err := app.store.Append(ctx, sid, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The shape the tool produces: the pipeline's own exit 0, with the stage note beside it.
	add(bashPair("c1", "make world opt 2>&1 | tee build.log | tail -50",
		"exit 0\n[note: this exit 0 is the LAST stage's. The pipeline's stages exited 2 → 0 → 0 "+
			"(left to right), so the work at the head of the pipe FAILED even though the pipeline "+
			"reported success.]\nbuilding…"))
	// A pipeline magi has no stage report for stays unknown — silence is not a verdict.
	add(bashPair("c2", "go build ./... | tail -5", "exit 0\n"))

	o := app.observe(ctx, sid)
	if len(o.ran()) != 1 {
		t.Fatalf("the pipeline with a stage report is now readable, the other is not: %+v", o.cmds)
	}
	if o.succeeded() {
		t.Error("a build that died at its first stage is not a success")
	}
	r := o.render()
	if !strings.Contains(r, "ran and FAILED") || !strings.Contains(r, "make world opt") {
		t.Errorf("the failed pipeline must be named as failed:\n%s", r)
	}
	if !strings.Contains(r, "status unknown") || !strings.Contains(r, "go build") {
		t.Errorf("the pipeline with no stage report must stay unknown:\n%s", r)
	}
}
