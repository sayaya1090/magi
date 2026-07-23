package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeCouncil returns a scripted deliberation per round (the last repeats) and
// records the last request it received.
type fakeCouncil struct {
	mu      sync.Mutex
	delibs  []council.Deliberation
	calls   int
	lastReq port.DeliberationRequest
	// judge scripts the revision-addressed verdict; nil means "always addressed" (the
	// default, so existing multi-round plan-audit fixtures still loop to the round cap).
	// judgeCalls counts how many times JudgeRevision ran (0 proves the flag gated it off).
	judge      func(port.RevisionJudgeRequest) port.RevisionVerdict
	judgeCalls int
	judgeReqs  []port.RevisionJudgeRequest
}

func (f *fakeCouncil) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq = req
	i := f.calls
	f.calls++
	if i < len(f.delibs) {
		return f.delibs[i], nil
	}
	return f.delibs[len(f.delibs)-1], nil
}

func (f *fakeCouncil) JudgeRevision(ctx context.Context, req port.RevisionJudgeRequest) (port.RevisionVerdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.judgeCalls++
	f.judgeReqs = append(f.judgeReqs, req)
	if f.judge != nil {
		return f.judge(req), nil
	}
	return port.RevisionVerdict{Addressed: true, Reason: "fake: addressed"}, nil
}

// submitAndDrain creates a session, submits a prompt, and returns the events.
func submitAndDrain(t *testing.T, a *App, workdir string) []event.Event {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: workdir, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	return waitForTerminal(t, a, sid)
}

// The council gate holds the loop open until the council votes done: a "continue"
// round injects feedback and the loop runs again.
func TestCouncilGateContinuesThenFinishes(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "the tests are missing — add them"},
		{Round: 2, Decision: council.Done},
	}}
	// The agent revises its answer after the feedback (a varied reply) — an
	// unchanged resubmission would now legitimately skip re-deliberation.
	a, wd := newApp(t, workingLLM(textStep("first answer"), textStep("revised with tests")), Config{Council: fc})
	evs := submitAndDrain(t, a, wd)

	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2", got)
	}
	if got := countType(evs, event.TypeCouncilDecided); got != 2 {
		t.Fatalf("council.decided = %d, want 2", got)
	}
	// The continue round must inject the feedback as a council-authored prompt.
	injected := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "council" {
			var d event.PromptSubmittedData
			_ = json.Unmarshal(e.Data, &d)
			if len(d.Parts) > 0 && strings.Contains(d.Parts[0].Text, "add them") {
				injected = true
			}
		}
	}
	if !injected {
		t.Fatal("continue round should inject the council feedback as a prompt")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after the council votes done")
	}
}

// cancelCouncil cancels the surrounding context the instant it is asked to
// deliberate — simulating a user interrupt landing mid-deliberation — then returns
// a CONTINUE verdict with feedback. The gate must notice the cancellation and
// refuse to inject that feedback (otherwise an interrupted turn would be re-armed
// with a spurious council prompt).
type cancelCouncil struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelCouncil) Deliberate(_ context.Context, _ port.DeliberationRequest) (council.Deliberation, error) {
	c.calls++
	c.cancel()
	return council.Deliberation{Round: 1, Decision: council.Continue, Feedback: "keep going"}, nil
}

func (c *cancelCouncil) JudgeRevision(_ context.Context, _ port.RevisionJudgeRequest) (port.RevisionVerdict, error) {
	return port.RevisionVerdict{Addressed: true}, nil
}

// newGateSession creates a session and returns the resolved Session + AgentSpec so
// a test can drive runCouncilGate directly.
func newGateSession(t *testing.T, a *App, wd string) (session.Session, AgentSpec) {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := a.sessionInfo(context.Background(), sid)
	return s, a.agentFor(s)
}

// councilInjected reports whether the council appended a feedback prompt.
func councilInjected(t *testing.T, a *App, sid session.SessionID) bool {
	t.Helper()
	evs, _ := a.store.Read(context.Background(), sid, 0)
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "council" {
			return true
		}
	}
	return false
}

// A cancel arriving while the council deliberates must abort the gate: it returns
// "don't continue" and injects no feedback prompt, so the loop unwinds the
// interrupt instead of looping again.
func TestCouncilGateCancelDuringDeliberation(t *testing.T) {
	cc := &cancelCouncil{}
	a, wd := newApp(t, workingLLM(), Config{Council: cc})
	s, agent := newGateSession(t, a, wd)

	ctx, cancel := context.WithCancel(context.Background())
	cc.cancel = cancel
	defer cancel()

	var ct councilTurn
	if keep, _ := a.runCouncilGate(ctx, s, agent, councilInput{turnTask: "task", lastText: "report", stepsLeft: 40}, &ct); keep {
		t.Error("gate must NOT continue after a mid-deliberation cancel")
	}
	if cc.calls != 1 {
		t.Errorf("Deliberate calls = %d, want exactly 1", cc.calls)
	}
	if councilInjected(t, a, s.ID) {
		t.Error("cancelled gate injected a council feedback prompt; it must unwind instead")
	}
}

// Entering the gate with an already-cancelled context is a no-op: no deliberation,
// no round consumed, no events — the loop just unwinds the cancellation.
func TestCouncilGateSkipsWhenAlreadyCancelled(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Continue, Feedback: "x"}}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc})
	s, agent := newGateSession(t, a, wd)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the gate runs

	var ct councilTurn
	if keep, _ := a.runCouncilGate(ctx, s, agent, councilInput{turnTask: "task", lastText: "report", stepsLeft: 40}, &ct); keep {
		t.Error("gate must not continue when entered with a cancelled context")
	}
	if fc.calls != 0 {
		t.Errorf("Deliberate must not run on a cancelled gate; calls=%d", fc.calls)
	}
	if ct.rounds != 0 {
		t.Errorf("a cancelled gate must not consume a round; rounds=%d", ct.rounds)
	}
	if countType(mustRead(t, a, s.ID), event.TypeCouncilConvened) != 0 {
		t.Error("a cancelled gate must not convene the council")
	}
}

// The self-measured cost cap stops round 2+ when deliberation has already eaten a
// disproportionate share of the turn's own wall clock: it finishes UNVERIFIED
// without deliberating again, and never on the first round.
func TestCouncilGateCostCapStopsLateRounds(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Continue, Feedback: "x"}}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc})
	s, agent := newGateSession(t, a, wd)
	ctx := context.Background()

	// rounds=1 (a round already ran), spent 5m of a 6m turn (>=60s and spent*4 >= turn).
	ct := councilTurn{rounds: 1, feedback: "still unmet: foo", spent: 5 * time.Minute}
	if keep, _ := a.runCouncilGate(ctx, s, agent, councilInput{turnTask: "task", lastText: "report", stepsLeft: 40, turnElapsed: 6 * time.Minute}, &ct); keep {
		t.Error("cost cap should finish (not continue) once deliberation dominates the turn")
	}
	if fc.calls != 0 {
		t.Errorf("cost cap must not deliberate again; calls=%d", fc.calls)
	}
	decided := mustRead(t, a, s.ID)
	found := false
	for _, e := range decided {
		if e.Type == event.TypeCouncilDecided && strings.Contains(string(e.Data), "UNVERIFIED") {
			found = true
		}
	}
	if !found {
		t.Error("cost-capped finish must be recorded as UNVERIFIED")
	}
}

// The cost cap never fires on the first round: even an expensive round-1 must run,
// so a single deliberation is always allowed.
func TestCouncilGateCostCapAllowsFirstRound(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc})
	s, agent := newGateSession(t, a, wd)

	ct := councilTurn{spent: 10 * time.Minute} // huge, but rounds==0 so the cap is not consulted
	_, _ = a.runCouncilGate(context.Background(), s, agent, councilInput{turnTask: "task", lastText: "report", stepsLeft: 40, turnElapsed: 6 * time.Minute}, &ct)
	if fc.calls != 1 {
		t.Errorf("first round must always deliberate regardless of prior cost; calls=%d", fc.calls)
	}
}

// The council-deadlock signal (hook B's trigger for solo-stuck redecompose) must fire ONLY
// when the council spends its whole round budget without ever approving — NOT when a DONE vote
// merely lands on the last allowed round. Both outcomes leave rounds==cap with feedback still
// set, so the round count alone cannot tell them apart; the gate must flag the genuine deadlock
// explicitly. Driving the gate to completion exercises exactly the discrimination hook B relies on.
func TestCouncilGateDeadlockSignal(t *testing.T) {
	// run drives the gate repeatedly (as the loop does) until it finishes, returning the
	// deadlock flag it reported on the finishing call and the final round count.
	run := func(t *testing.T, delibs []council.Deliberation) (bool, int) {
		t.Helper()
		fc := &fakeCouncil{delibs: delibs}
		a, wd := newApp(t, workingLLM(), Config{Council: fc, CouncilMaxRounds: 2})
		s, agent := newGateSession(t, a, wd)
		var ct councilTurn
		for i := 0; i < 5; i++ { // bounded; the gate finishes well within the cap+1
			if keep, _ := a.runCouncilGate(context.Background(), s, agent,
				councilInput{turnTask: "task", lastText: "report", stepsLeft: 40}, &ct); !keep {
				break
			}
		}
		return ct.deadlocked, ct.rounds
	}

	// Distinct feedback every round (so the no-progress short-circuit never fires) → the council
	// votes Continue until the round cap forces the finish: a genuine deadlock.
	t.Run("cap exhausted without approval → deadlock", func(t *testing.T) {
		deadlock, rounds := run(t, []council.Deliberation{
			{Round: 1, Decision: council.Continue, Feedback: "still unmet A"},
			{Round: 2, Decision: council.Continue, Feedback: "still unmet B"},
		})
		if rounds != 2 {
			t.Fatalf("the cap (2) should be reached, rounds=%d", rounds)
		}
		if !deadlock {
			t.Error("a council that spent every round without approving must flag a deadlock (hook B should fire)")
		}
	})

	// Continue on round 1, then DONE on round 2 — the last allowed round. An approval, not a
	// deadlock, even though rounds==cap and the round-1 feedback is still on record.
	t.Run("DONE on the last allowed round → not a deadlock", func(t *testing.T) {
		deadlock, rounds := run(t, []council.Deliberation{
			{Round: 1, Decision: council.Continue, Feedback: "still unmet A"},
			{Round: 2, Decision: council.Done},
		})
		if rounds != 2 {
			t.Fatalf("rounds should reach 2 (DONE landed on the cap round), got %d", rounds)
		}
		if deadlock {
			t.Error("a DONE vote on the last allowed round is an approval, not a deadlock — hook B must NOT fire")
		}
	})
}

// mustRead returns the full fact log for a session.
func mustRead(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// With no council configured, the loop finishes as before — no council events.
func TestNoCouncilNoGate(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	evs := submitAndDrain(t, a, wd)
	if countType(evs, event.TypeCouncilConvened) != 0 {
		t.Fatal("no council should be convened when unconfigured")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish normally")
	}
}

// The council gate only judges turns that did real work. A purely conversational
// turn (a greeting, a question — no tool calls) must finish without convening the
// council, so the user isn't held in a deliberation loop over small talk.
func TestCouncilSkipsConversationalTurn(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Continue, Feedback: "more"}}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc}) // no steps → plain text reply, no tools
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 0 {
		t.Fatalf("council should not convene on a no-tool conversational turn, got %d", got)
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("conversational turn should finish immediately")
	}
}

// A pure read-only turn (successful empty diff, no signals) is flagged NoChanges
// to the council so members judge the report on its merits instead of demanding a
// diff — and the configured CONSENSUS rule is preserved (NOT relaxed): the gate
// stays a real consensus, the churn fix lives in how members weigh evidence.
func TestCouncilNoChangesTurn(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, workingLLM(), builtin.Default(), bus.New(), platform.New(), Config{
		Council: fc, CouncilRule: "unanimous", Permission: "allow",
	})
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	wd := gitRepo(t, []string{"commit", "--allow-empty", "-q", "-m", "init"}) // clean tree → empty diff
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "do the task"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, a, sid)

	fc.mu.Lock()
	req := fc.lastReq
	fc.mu.Unlock()
	if !req.NoChanges {
		t.Fatal("a read-only turn (clean tree, no signals) should be flagged NoChanges to the council")
	}
	if string(req.Rule) != "unanimous" {
		t.Fatalf("the consensus rule must be preserved, not relaxed; got %q", req.Rule)
	}
}

// A council that always says continue is bounded by max_rounds, then finishes.
func TestCouncilMaxRoundsStops(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		// Distinct feedback each round so the no-progress guard doesn't fire first;
		// the round cap is what must stop it.
		{Round: 1, Decision: council.Continue, Feedback: "step one"},
		{Round: 2, Decision: council.Continue, Feedback: "step two"},
		{Round: 3, Decision: council.Continue, Feedback: "step three"},
		{Round: 4, Decision: council.Continue, Feedback: "step four"},
	}}
	a, wd := newApp(t, workingLLM(textStep("try one"), textStep("try two"), textStep("try three")), Config{Council: fc, CouncilMaxRounds: 2})
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2 (max rounds)", got)
	}
	if !hasDecidedNote(evs, "unresolved after") {
		t.Fatal("hitting the cap should record a forced-finish council.decided note")
	}
	// A deadlocked finish is not an approval: the note must say so and carry the
	// last outstanding feedback so the record shows WHAT was still unmet.
	if !hasDecidedNote(evs, "UNVERIFIED") {
		t.Fatal("cap-forced finish should mark the result UNVERIFIED, not read as an approval")
	}
	if !hasDecidedNote(evs, "step two") {
		t.Fatal("cap-forced finish should carry the last outstanding feedback")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after rounds are exhausted")
	}
}

// An agent that resubmits its rejected answer verbatim without running a single
// tool gets NO second deliberation: the round would re-judge identical evidence
// and print the same (often long) answer twice. One convene, then a skip note.
func TestCouncilSkipsRedeliberationOnUnchangedResubmission(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "do more"},
		{Round: 2, Decision: council.Continue, Feedback: "even more"},
	}}
	// workingLLM's fallback reply is the identical "done" text every round.
	a, wd := newApp(t, workingLLM(), Config{Council: fc, CouncilMaxRounds: 5})
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 1 {
		t.Fatalf("unchanged resubmission should not re-convene: convened=%d, want 1", got)
	}
	if !hasDecidedNote(evs, "resubmitted unchanged") {
		t.Fatal("the skip should be recorded as a decided note")
	}
	if !hasDecidedNote(evs, "UNVERIFIED") {
		t.Fatal("an unreviewed finish must read as UNVERIFIED")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after the skip")
	}
}

// Repeated feedback (no progress) stops the gate before max_rounds.
func TestCouncilNoProgressStops(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "same thing"},
		{Round: 2, Decision: council.Continue, Feedback: "same thing"},
	}}
	a, wd := newApp(t, workingLLM(textStep("attempt a"), textStep("attempt b"), textStep("attempt c")), Config{Council: fc, CouncilMaxRounds: 5})
	evs := submitAndDrain(t, a, wd)
	if got := countType(evs, event.TypeCouncilConvened); got != 2 {
		t.Fatalf("council.convened = %d, want 2 (stopped on no progress)", got)
	}
	if !hasDecidedNote(evs, "no new feedback") {
		t.Fatal("repeated feedback should record a no-progress council.decided note")
	}
	if !hasDecidedNote(evs, "UNVERIFIED") {
		t.Fatal("no-progress finish should mark the result UNVERIFIED, not read as an approval")
	}
	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatal("turn should finish after no-progress is detected")
	}
}

// The agent's plan (the todos it sets THIS turn, with status) is handed to the
// council as the contract (D15), so an unfinished item is visible grounds to judge
// "not done". (A new prompt clears prior todos, so the plan is built in-turn.)
func TestCouncilReceivesPlan(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("todowrite", `{"todos":[{"content":"write the parser","status":"completed"},{"content":"add tests","status":"pending"}]}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	submitAndDrain(t, a, wd)

	fc.mu.Lock()
	plan := fc.lastReq.Plan
	fc.mu.Unlock()
	// The plan must convey both items AND their status (a pending item is the
	// council's grounds to judge "not done").
	if !strings.Contains(plan, "[x] write the parser") || !strings.Contains(plan, "[ ] add tests") {
		t.Fatalf("council should receive the plan with statuses as the contract, got %q", plan)
	}
}

// verifyFailPlatform fails the verify command (non-git Exec) and reports a clean
// git tree, so the test is robust to however many Exec calls GitDiff makes.
type verifyFailPlatform struct{}

func (verifyFailPlatform) Exec(ctx context.Context, c port.Cmd) (port.ExecResult, error) {
	if c.Path == "git" {
		return port.ExecResult{ExitCode: 0}, nil
	}
	return port.ExecResult{Stdout: []byte("--- FAIL: TestX"), ExitCode: 1}, nil
}
func (verifyFailPlatform) ConfigDir() string                        { return "" }
func (verifyFailPlatform) DataDir() string                          { return "" }
func (verifyFailPlatform) TerminalCaps() port.TermCaps              { return port.TermCaps{} }
func (verifyFailPlatform) ProcessCPUTime(int) (time.Duration, bool) { return 0, false }

// With a "verify" signal configured, the gate runs it and feeds the outcome to
// the council; the convened event surfaces a summary (D16, opt-in).
func TestCouncilVerifySignal(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, _, wd := newWorkflowApp(t, workingLLM(), verifyFailPlatform{}, Config{
		Council: fc, CouncilSignals: []CouncilSignalSpec{{Name: "verify", Command: "go test ./..."}},
	})
	evs := submitAndDrain(t, a, wd)

	fc.mu.Lock()
	sigs := fc.lastReq.Signals
	fc.mu.Unlock()
	if len(sigs) != 1 || sigs[0].Source != "verify" || sigs[0].Status != "fail" {
		t.Fatalf("council should receive a failing verify signal, got %+v", sigs)
	}
	// The convened event surfaces the signal summary for observability.
	found := false
	for _, e := range evs {
		if e.Type == event.TypeCouncilConvened {
			var d event.CouncilConvenedData
			if json.Unmarshal(e.Data, &d) == nil {
				for _, s := range d.Signals {
					if strings.Contains(s, "verify: fail") {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("convened event should carry the verify signal summary")
	}
}

// An unverified deliverable — one the agent produced this turn but never ran anything to
// exercise — is fed to the council as an always-on deterministic signal (no config), so a
// text-only vote can't wave it through. The signal is structural (produced-but-not-exercised),
// not a scan of the file's wording, so it is language-agnostic.
func TestCouncilFabricationSignal(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	// The agent writes a deliverable and finishes without running any command against it.
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("write", `{"path":"sol.py","content":"print('ok')\n"}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	submitAndDrain(t, a, wd)

	fc.mu.Lock()
	sigs := fc.lastReq.Signals
	fc.mu.Unlock()
	found := false
	for _, s := range sigs {
		if s.Source == "self-check" && s.Kind == "unverified" && s.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("council should receive a failing self-check/unverified signal, got %+v", sigs)
	}
}

// Events are tagged with their macro loop stage (D15): execute for the agent's
// work, council for the deliberation, finalize for the turn end.
func TestEventStageTags(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc})
	evs := submitAndDrain(t, a, wd)

	stageOf := func(ty event.Type) string {
		for _, e := range evs {
			if e.Type == ty {
				return e.Stage
			}
		}
		return "<absent>"
	}
	if s := stageOf(event.TypeCouncilConvened); s != stageCouncil {
		t.Errorf("council.convened stage = %q, want %q", s, stageCouncil)
	}
	if s := stageOf(event.TypeCouncilDecided); s != stageCouncil {
		t.Errorf("council.decided stage = %q, want %q", s, stageCouncil)
	}
	if s := stageOf(event.TypeTurnFinished); s != stageFinalize {
		t.Errorf("turn.finished stage = %q, want %q", s, stageFinalize)
	}
	// The agent's assistant message is tagged execute.
	gotExecute := false
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant {
			if e.Stage != stageExecute {
				t.Errorf("assistant part stage = %q, want %q", e.Stage, stageExecute)
			}
			gotExecute = true
		}
	}
	if !gotExecute {
		t.Fatal("expected an assistant part.appended to assert execute stage")
	}
}

// The council runs every configured signal and feeds them all as evidence (D16).
func TestCouncilMultipleSignals(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, _, wd := newWorkflowApp(t, workingLLM(), verifyFailPlatform{}, Config{
		Council: fc,
		CouncilSignals: []CouncilSignalSpec{
			{Name: "test", Command: "go test ./..."},
			{Name: "lint", Command: "golangci-lint run"},
		},
	})
	submitAndDrain(t, a, wd)

	fc.mu.Lock()
	sigs := fc.lastReq.Signals
	fc.mu.Unlock()
	if len(sigs) != 2 {
		t.Fatalf("council should receive 2 signals, got %d: %+v", len(sigs), sigs)
	}
	names := map[string]bool{}
	for _, s := range sigs {
		names[s.Source] = true
		if s.Status != "fail" { // verifyFailPlatform fails non-git commands
			t.Errorf("signal %q status = %q, want fail", s.Source, s.Status)
		}
	}
	if !names["test"] || !names["lint"] {
		t.Fatalf("expected test+lint signals, got %v", names)
	}
}

// countLLM replies "- done" to everything and counts acceptance-criteria calls.
// Its first real agent turn does a read-only tool call so the council gate applies
// (the gate skips turns that used no tools).
type countLLM struct {
	mu            sync.Mutex
	criteriaCalls int
	turnCalls     int
}

func (c *countLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	c.mu.Lock()
	criteria := strings.Contains(r.System, "acceptance criteria")
	first := false
	if criteria {
		c.criteriaCalls++
	} else {
		c.turnCalls++
		first = c.turnCalls == 1
	}
	c.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	if first {
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_read", Name: "read", Args: json.RawMessage(`{"path":"x"}`)}}
	} else {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "- done"}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// With criteria on, the council's contract includes elicited acceptance criteria,
// elicited once per turn (cached across rounds).
func TestCouncilCriteriaElicitedOncePerTurn(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "do more"},
		{Round: 2, Decision: council.Done},
	}}
	llm := &countLLM{}
	a, wd := newApp(t, llm, Config{Council: fc, CouncilCriteria: true})
	submitAndDrain(t, a, wd)

	llm.mu.Lock()
	n := llm.criteriaCalls
	llm.mu.Unlock()
	if n != 1 {
		t.Fatalf("acceptance criteria should be elicited once per turn, got %d calls", n)
	}
	fc.mu.Lock()
	plan := fc.lastReq.Plan
	fc.mu.Unlock()
	if !strings.Contains(plan, "Acceptance criteria:") {
		t.Fatalf("council contract should include acceptance criteria, got %q", plan)
	}
}

// Criteria is off by default — no extra elicitation, no criteria in the contract.
func TestCouncilNoCriteriaWhenOff(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc})
	submitAndDrain(t, a, wd)
	fc.mu.Lock()
	plan := fc.lastReq.Plan
	fc.mu.Unlock()
	if strings.Contains(plan, "Acceptance criteria:") {
		t.Fatalf("criteria should be off by default, got %q", plan)
	}
}

func hasDecidedNote(evs []event.Event, sub string) bool {
	for _, e := range evs {
		if e.Type == event.TypeCouncilDecided {
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && strings.Contains(d.Note, sub) {
				return true
			}
		}
	}
	return false
}
