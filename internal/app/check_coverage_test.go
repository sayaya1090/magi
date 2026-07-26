package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The coverage-fill prompt must carry the coverage contract: fill gaps with a SUPERSET (existing
// unchanged + one new check per uncovered producing step), scope each new check by step number, keep
// checks portable + idempotent, and skip read-only steps. Guards the criteria.go prompt against a
// silent regression that would let the fill drop checks or invent work-doing "checks".
func TestCoverageFillPromptCarriesContract(t *testing.T) {
	for _, want := range []string{
		"FILLING GAPS", "existing checks UNCHANGED", "one NEW check for EACH producing step",
		"1-based position", "IDEMPOTENT", "read-only step", "Do NOT alter or drop the existing checks",
	} {
		if !strings.Contains(coverageFillSystem, want) {
			t.Errorf("coverageFillSystem must state the coverage contract (missing %q)", want)
		}
	}
}

// With the flag off, ensureStepCoverage is a pure passthrough — the authored checks are used as-is and
// no fill call is made, even when the plan has more steps than covered checks (a real gap).
func TestEnsureCoverageFlagOffPassthrough(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "0")
	// A reply that WOULD add coverage, to prove the flag short-circuits before any call is used.
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a"},{"step":"2","command":"b"}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	if len(out) != 1 || out[0].Command != "a" {
		t.Fatalf("flag off must return the authored checks unchanged, got %+v", out)
	}
}

// When every plan step is already covered (covered distinct steps >= plan steps), there is no gap, so
// ensureStepCoverage returns the input untouched and makes no fill call.
func TestEnsureCoverageNoGap(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	a := newOrchApp(t, &gateLLM{text: `[{"step":"9","command":"junk"}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}, {Step: "2", Command: "b"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	if len(out) != 2 {
		t.Fatalf("no gap must return the authored checks unchanged, got %d checks: %+v", len(out), out)
	}
}

// The core case: an 11-step plan with a single authored check is a coverage gap; the fill pass replaces
// it with the superset the reviewer returns (existing + new per-step checks), so the termination gate's
// ledger now spans the whole plan instead of one step.
func TestEnsureCoverageFillsGap(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	filled := `[{"step":"1","command":"a"},{"step":"2","command":"b"},{"step":"3","command":"c"}]`
	a := newOrchApp(t, &gateLLM{text: filled}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	covered := map[string]bool{}
	for _, c := range out {
		covered[strings.TrimSpace(c.Step)] = true
	}
	if len(out) != 3 || len(covered) != 3 {
		t.Fatalf("gap must be filled to a 3-step superset, got %d checks over %d steps: %+v", len(out), len(covered), out)
	}
}

// A check carrying an out-of-range step label ("99" in a 3-step plan) attaches to no plan step, so it
// must NOT count as coverage. Previously the distinct-label count was inflated by such a label to ==
// plan-step count, wrongly concluding full coverage and suppressing the gap-fill for a step that
// genuinely had no check. Coverage is now counted by real 1-based step number, so the gap is filled.
func TestEnsureCoverageIgnoresOutOfRangeStepLabel(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	filled := `[{"step":"1","command":"a"},{"step":"2","command":"b"},{"step":"3","command":"c"}]`
	a := newOrchApp(t, &gateLLM{text: filled}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	// steps 1 and 2 covered, step 3 has NO check; the bogus "99" label must not mask that gap.
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}, {Step: "2", Command: "b"}, {Step: "99", Command: "bogus"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	covered := map[int]bool{}
	for _, c := range out {
		covered[leadingInt(c.Step)] = true
	}
	if !covered[3] {
		t.Fatalf("an out-of-range label must not suppress the gap-fill for the real uncovered step 3, got %+v", out)
	}
}

// A reply that adds no distinct step coverage is rejected — the authored contract is kept rather than
// weakened. Two sub-cases.
func TestEnsureCoverageRejectsNonSuperset(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	steps := []planStep{{Title: "one"}, {Title: "two"}}

	// (a) reply is empty: nothing to merge, so coverage is unchanged → keep authored.
	a := newOrchApp(t, &gateLLM{text: `[]`}, Config{Permission: "allow", MaxAgents: 10})
	if out := a.ensureStepCoverage(context.Background(), s, "task", steps, in); len(out) != 1 {
		t.Errorf("an empty reply must keep the authored set, got %+v", out)
	}

	// (b) reply is bigger but adds no NEW distinct step (both checks on step 1) → no coverage gained.
	a2 := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a"},{"step":"1","command":"a2"}]`}, Config{Permission: "allow", MaxAgents: 10})
	if out := a2.ensureStepCoverage(context.Background(), s, "task", steps, in); len(out) != 1 {
		t.Errorf("a reply adding no distinct-step coverage must keep the authored set, got %+v", out)
	}
}

// The 0-step solo path: no authored checks and a single synthetic step for the objective is a gap, so
// the fill authors one check — solo work lands a verifiable contract instead of an empty ledger.
func TestEnsureCoverageSoloObjective(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","deliverable":"server on 5328","command":"python3 -c \"import socket;exit(0 if socket.socket().connect_ex(('localhost',5328))==0 else 1)\""}]`}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	steps := []planStep{{Title: "run a server on port 5328", Strategy: "solo"}}
	out := a.ensureStepCoverage(context.Background(), s, "run a server on port 5328", steps, nil)
	if len(out) != 1 || strings.TrimSpace(out[0].Command) == "" {
		t.Fatalf("solo objective must get one authored check, got %+v", out)
	}
}

// A CONFIRMED gap that the fill fails to close must SAY SO. Silence used to be indistinguishable
// from "fully covered", so a plan that landed with most of its steps ungated read exactly like a
// plan that needed no fill — the gate then had almost no executable evidence and nobody could tell
// from the log. Each failure mode names itself.
func TestEnsureCoverageReportsUnfilledGap(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}, {Title: "four"}, {Title: "five"}}
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}

	cases := []struct {
		name, reply, want string
	}{
		{"unparseable fill", "not a checks array at all", "did not parse"},
		{"fill returns nothing", `[]`, "none attach to an uncovered step"},
		{"fill adds no new step coverage", `[{"step":"1","command":"a"},{"step":"1","command":"a2"}]`, "none attach to an uncovered step"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newOrchApp(t, &gateLLM{text: c.reply}, Config{Permission: "allow", MaxAgents: 10})
			s := parentSession(t.TempDir())
			sub := watchProgress(t, a, s.ID)
			out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)
			if len(out) != 1 || out[0].Command != "a" {
				t.Fatalf("an unusable fill must leave the authored checks alone, got %+v", out)
			}
			note := sub.notes("check-coverage")
			if !strings.Contains(note, "gap NOT filled") || !strings.Contains(note, c.want) {
				t.Errorf("want a shortfall note naming %q, got:\n%s", c.want, note)
			}
			if !strings.Contains(note, "1/5 step(s) covered") {
				t.Errorf("the note must quantify the gap, got:\n%s", note)
			}
		})
	}
}

// The shape a live run actually hit: 4 authored checks ALL attached to step 1 of a 4-step plan, and a
// fill that answers with 3 checks spread across steps 1-3. The old guard compared counts (3 < 4) and
// discarded the fill, so a gap-FILLING pass rejected the only reply that filled the gap and the run
// landed 3 of 4 steps ungated. Coverage decides now, and the authored checks survive the merge.
func TestEnsureCoverageAcceptsAFillWithFewerButBroaderChecks(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}, {Title: "four"}}
	in := []council.DeliverableCheck{
		{Step: "1", Command: "a1"}, {Step: "1", Command: "a2"},
		{Step: "1", Command: "a3"}, {Step: "1", Command: "a4"},
	}
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a1"},{"step":"2","command":"b"},{"step":"3","command":"c"}]`},
		Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	out := a.ensureStepCoverage(context.Background(), s, "task", steps, in)

	covered := map[int]bool{}
	cmds := map[string]bool{}
	for _, c := range out {
		covered[leadingInt(c.Step)] = true
		cmds[c.Command] = true
	}
	for _, step := range []int{1, 2, 3} {
		if !covered[step] {
			t.Errorf("step %d must be covered after the fill, got %+v", step, out)
		}
	}
	// Merged, not replaced: the three authored commands the fill omitted are still in the contract.
	for _, cmd := range []string{"a1", "a2", "a3", "a4"} {
		if !cmds[cmd] {
			t.Errorf("authored check %q must survive the merge, got %+v", cmd, out)
		}
	}
	if len(out) != 6 { // 3 from the fill + 3 authored it dropped (a1 is shared)
		t.Errorf("want the union of both sets (6 checks), got %d: %+v", len(out), out)
	}
	if note := sub.notes("check-coverage"); !strings.Contains(note, "restored 3 authored check(s)") {
		t.Errorf("the note must say authored checks were put back, got:\n%s", note)
	}
}

// progressWatcher collects the transient tool-progress notes a session publishes. They go to the bus
// rather than the store, so a test that asserts on one has to be listening before the call.
type progressWatcher struct {
	mu   sync.Mutex
	seen []string
	stop func()
}

func watchProgress(t *testing.T, a *App, sid session.SessionID) *progressWatcher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch, unsub := a.bus.Subscribe(ctx, sid)
	w := &progressWatcher{stop: func() { unsub(); cancel() }}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			if e.Type != event.TypeToolProgress {
				continue
			}
			var d event.ToolProgressData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			w.mu.Lock()
			w.seen = append(w.seen, d.Name+": "+d.Text)
			w.mu.Unlock()
		}
	}()
	t.Cleanup(func() { w.stop(); <-done })
	return w
}

// notes returns every note published under the given tool name, joined. It polls briefly because the
// publish and the collecting goroutine are concurrent.
func (w *progressWatcher) notes(name string) string {
	for i := 0; i < 200; i++ {
		w.mu.Lock()
		var hit []string
		for _, s := range w.seen {
			if strings.HasPrefix(s, name+": ") {
				hit = append(hit, s)
			}
		}
		w.mu.Unlock()
		if len(hit) > 0 {
			return strings.Join(hit, "\n")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ""
}

// A parse failure must print the REPLY, not only its length. The side calls leave no session
// record, so an excerpt is the only way to tell "the model answered with prose" from "it wrapped
// the array in an object" from "an element had the wrong shape" — a distinction that blocked
// diagnosis for three consecutive runs.
func TestCoverageAndAuditParseFailuresShowTheReply(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	reply := "I could not produce checks for these steps because the plan is exploratory."

	// coverage fill
	a := newOrchApp(t, &gateLLM{text: reply}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	a.ensureStepCoverage(context.Background(), s, "task",
		[]planStep{{Title: "one"}, {Title: "two"}}, []council.DeliverableCheck{{Step: "1", Command: "a"}})
	note := sub.notes("check-coverage")
	if !strings.Contains(note, "did not parse") || !strings.Contains(note, "exploratory") {
		t.Errorf("the coverage failure must quote the reply:\n%s", note)
	}

	// check audit
	a2 := newOrchApp(t, &gateLLM{text: reply}, Config{Permission: "allow", MaxAgents: 10})
	s2 := parentSession(t.TempDir())
	sub2 := watchProgress(t, a2, s2.ID)
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	if got := a2.validateChecks(context.Background(), a2.agentFor(s2), s2, in); len(got) != 1 {
		t.Fatalf("an unusable review must keep the authored checks, got %+v", got)
	}
	if n := sub2.notes("check-audit"); !strings.Contains(n, "unreviewed") || !strings.Contains(n, "exploratory") {
		t.Errorf("the audit failure must say the checks went unreviewed and quote the reply:\n%s", n)
	}
}

// "Added nothing that attaches" has two very different causes — the fill returned nothing at all,
// or it returned checks whose step labels fall outside the plan — and only the labels tell them
// apart. The note must therefore carry them.
func TestCoverageShortfallNamesTheStepLabels(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	steps := []planStep{{Title: "one"}, {Title: "two"}}
	in := []council.DeliverableCheck{{Step: "1", Command: "a"}}
	// The fill answers with a check scoped to a step that does not exist.
	a := newOrchApp(t, &gateLLM{text: `[{"step":"1","command":"a"},{"step":"99","command":"b"}]`},
		Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	a.ensureStepCoverage(context.Background(), s, "task", steps, in)
	note := sub.notes("check-coverage")
	if !strings.Contains(note, `"99"`) {
		t.Errorf("the note must name the step labels the fill carried:\n%s", note)
	}
	if !strings.Contains(note, "returned 2 check(s)") || !strings.Contains(note, "plan has 2 step(s)") {
		t.Errorf("the note must quantify both sides:\n%s", note)
	}
}

// The empty fill is the case the run actually hit, and it is the one the note above cannot
// describe: with zero checks there are no step labels to print, so the message degenerated to
// "returned 0 check(s) ... labels were []" — true, and useless. Three different events land here
// (an empty array, checks whose `command` was empty and were dropped as unrunnable, prose that
// merely contained a bare `[]`) and only the reply distinguishes them, so the reply must be shown.
func TestEnsureCoverageShowsTheReplyWhenTheFillIsEmpty(t *testing.T) {
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	steps := []planStep{{Title: "one"}, {Title: "two"}, {Title: "three"}}

	// Both seeds matter and they take DIFFERENT branches: with checks already authored an empty fill
	// reads as "dropped them all", with none authored it reads as "added no coverage". The observed
	// run was the second — a 6-step plan with zero checks — and neither branch could say what came
	// back, so the two were indistinguishable in the log.
	cases := []struct {
		name, reply, want string
		in                []council.DeliverableCheck
	}{
		{name: "empty array, nothing authored", reply: `[]`, want: "[]"},
		{name: "commandless checks, nothing authored", reply: `[{"step":"2","deliverable":"the binary runs"}]`, want: "the binary runs"},
		{name: "empty array over authored checks", reply: `[]`, want: "[]", in: []council.DeliverableCheck{{Step: "1", Command: "a"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newOrchApp(t, &gateLLM{text: c.reply}, Config{Permission: "allow", MaxAgents: 10})
			s := parentSession(t.TempDir())
			sub := watchProgress(t, a, s.ID)
			if out := a.ensureStepCoverage(context.Background(), s, "task", steps, c.in); len(out) != len(c.in) {
				t.Fatalf("an empty fill must leave the authored checks alone, got %+v", out)
			}
			note := sub.notes("check-coverage")
			if !strings.Contains(note, "gap NOT filled") {
				t.Fatalf("want a shortfall note, got:\n%s", note)
			}
			if !strings.Contains(note, c.want) {
				t.Errorf("the note must quote the reply (%q), got:\n%s", c.want, note)
			}
		})
	}
}
