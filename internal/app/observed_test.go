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

// The record lists EVERY command with the exit magi learned, and sorts none of them. It used to
// drop inspections and split the rest into "ran clean"/"ran and FAILED" — a reading, in the one
// place whose point is not to read, and one that was wrong repeatedly (`sed -n` and `grep` as
// program runs, a quoted `|` splitting a grep, `find` and `git log` still counted as runs).
func TestRecordListsEveryCommandWithItsExit(t *testing.T) {
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
	// The tee target and the redirect are both paths this run wrote.
	if got := strings.Join(o.changed, ","); !strings.Contains(got, "build.log") || !strings.Contains(got, "out.txt") {
		t.Errorf("changed must hold what the commands wrote, got %q", got)
	}

	r := o.render()
	for _, want := range []string{
		"WHAT MAGI OBSERVED",
		"make world opt",
		"make -C testsuite one DIR=tests/basic → exit 2", // the one status that changes what to do next
		"ls -la /app", // an inspection is a command magi granted, and it says so
		"cat build.log | head -20",
		"cd /app && ./sim 208 > out.txt",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("render missing %q:\n%s", want, r)
		}
	}
	// A zero and an unreadable status are both clutter: they are in the tool result beside this
	// block and neither changes what to do next.
	if strings.Contains(r, "exit 0") || strings.Contains(r, "exit unknown") {
		t.Errorf("only a non-zero exit magi learned belongs here:\n%s", r)
	}
	// No verdict words: which command exercised anything is the reader's call.
	for _, banned := range []string{"ran clean", "ran and FAILED", "nothing that exercises"} {
		if strings.Contains(r, banned) {
			t.Errorf("the record must not sort commands for the reader (%q):\n%s", banned, r)
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
	if !strings.Contains(rec, "make world opt 2>&1 | tee build.log") {
		t.Errorf("the command must be in the record:\n%s", rec)
	}
	// The exit belonged to the tail, so magi never learned this command's — and must not print one.
	if strings.Contains(rec, "exit 0") {
		t.Errorf("an exit that is not the command's own must never be printed as the command's:\n%s", rec)
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
// determine was this. The shell reports every stage now, and the record carries those numbers.
//
// It used to carry a reading of them instead: scan the note for "the work at the head of the pipe
// FAILED" and file a synthetic exit 1. The note stopped making that claim and the scan went quiet —
// the record simply stopped saying anything, and no test noticed. Numbers do not have that failure
// mode, and which of them matters is the reader's call anyway.
func TestAPipelineCarriesItsStageStatuses(t *testing.T) {
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
		"exit 0\n[note: the status above is the pipeline's LAST stage. Its stages exited 2 → 0 → 0 "+
			"(left to right).]\nbuilding…"))
	// A pipeline magi has no stage report for stays unknown — silence is not a verdict.
	add(bashPair("c2", "go build ./... | tail -5", "exit 0\n"))

	r := app.observe(ctx, sid).render()
	// The stage report is what the reported exit cannot show, so the record states it.
	if !strings.Contains(r, "make world opt 2>&1 | tee build.log | tail -50 → stages 2 0 0") {
		t.Errorf("a pipeline's stage statuses must reach the record:\n%s", r)
	}
	// Without a stage report magi never learned the status, so it claims none — silence, not zero.
	if !strings.Contains(r, "go build ./... | tail -5") || strings.Contains(r, "go build ./... | tail -5 → exit") {
		t.Errorf("the pipeline with no stage report must carry no exit at all:\n%s", r)
	}
}
