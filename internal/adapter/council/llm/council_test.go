package llm

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeLLM returns a scripted assistant reply per request. reply may inspect the
// request (e.g. its System prompt names the member) to vary the verdict.
type fakeLLM struct {
	reply func(port.ChatRequest) string
	err   error
}

func (f fakeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: f.reply(r)}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func memberIn(r port.ChatRequest, name string) bool {
	return strings.Contains(r.System, "You are "+name)
}

// textOf returns the concatenated user-message text of a request (the evidence body,
// where the rebuttal round's peer digest appears).
func textOf(r port.ChatRequest) string {
	var b strings.Builder
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// only returns a resolver that always yields p (when per-member routing is irrelevant).
func only(p port.LLMProvider) func(string) port.LLMProvider {
	return func(string) port.LLMProvider { return p }
}

// The terminate-phase member prompt must carry the artifact-grounding clause (a
// description is not the deliverable) WITHOUT displacing the no-churn balance, and
// the clause must NOT leak into the pre-flight plan-audit prompt.
// The council must see what the turn actually produced (model text + tool results) as
// real, git-independent evidence — so a create task in a non-git workdir is judged on
// its actions, not on an absent diff.
func TestEvidenceActions(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task:    "create hello.txt",
		Report:  "done",
		Actions: "- tool write [ok]: wrote 13 bytes to hello.txt\n- tool bash [ok]: Hello, world!",
	})
	if !strings.Contains(got, "verified tool outputs") {
		t.Errorf("actions section header missing:\n%s", got)
	}
	if !strings.Contains(got, "wrote 13 bytes to hello.txt") {
		t.Errorf("actions content missing:\n%s", got)
	}
	// No actions → no section.
	if e := evidence(port.DeliberationRequest{Task: "x", Report: "y"}); strings.Contains(e, "verified tool outputs") {
		t.Errorf("empty actions should not render the section:\n%s", e)
	}
}

// JudgeRevision parses the model's {addressed,reason} verdict, and fails OPEN
// (Addressed=true) on a backend error or an unparseable reply so a flaky judge never
// falsely cuts a productive re-plan loop.
func TestJudgeRevision(t *testing.T) {
	ctx := context.Background()
	req := port.RevisionJudgeRequest{Critique: "size A1", PriorPlan: "1. compute", RevisedPlan: "1. size A1\n2. compute"}

	// Parsed true, with surrounding prose + a code fence (weak-model tolerance).
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		return "Sure:\n```json\n{\"addressed\": true, \"reason\": \"adds a sizing step\"}\n```"
	}}), "m")
	v, err := c.JudgeRevision(ctx, req)
	if err != nil || !v.Addressed || v.Reason != "adds a sizing step" {
		t.Fatalf("parsed-true: got %+v err=%v", v, err)
	}

	// Parsed false is honored (this is what triggers early convergence stop).
	c = New(only(fakeLLM{reply: func(port.ChatRequest) string { return `{"addressed": false, "reason": "same steps"}` }}), "m")
	if v, _ := c.JudgeRevision(ctx, req); v.Addressed || v.Reason != "same steps" {
		t.Fatalf("parsed-false: got %+v", v)
	}

	// Unparseable reply → fail open.
	c = New(only(fakeLLM{reply: func(port.ChatRequest) string { return "I think it's fine, no JSON here" }}), "m")
	if v, _ := c.JudgeRevision(ctx, req); !v.Addressed || !strings.Contains(v.Reason, "unparseable") {
		t.Fatalf("unparseable should fail open: got %+v", v)
	}

	// Backend error → fail open.
	c = New(only(fakeLLM{err: errors.New("boom")}), "m")
	if v, _ := c.JudgeRevision(ctx, req); !v.Addressed || !strings.Contains(v.Reason, "unavailable") {
		t.Fatalf("backend error should fail open: got %+v", v)
	}
}

// A verdict spelled as a quoted word is still a verdict. A strict bool rejected the whole object
// over that one field, and because this call fails open the reply then landed as the OPPOSITE of
// what the judge said — with its reason, which named exactly what the revision had omitted,
// discarded along with it. Observed live on a fully delivered 280-byte reply.
func TestJudgeRevisionReadsAQuotedVerdict(t *testing.T) {
	ctx := context.Background()
	req := port.RevisionJudgeRequest{Critique: "size A1", PriorPlan: "1. compute", RevisedPlan: "1. compute"}

	for _, tc := range []struct {
		reply      string
		wantYes    bool
		wantReason string
	}{
		{`{"addressed": "false", "reason": "omits the required test step"}`, false, "omits the required test step"},
		{`{"addressed": "true", "reason": "adds it"}`, true, "adds it"},
		{`{"addressed": "no", "reason": "same steps"}`, false, "same steps"},
		{`{"addressed": "yes", "reason": "reordered"}`, true, "reordered"},
	} {
		c := New(only(fakeLLM{reply: func(port.ChatRequest) string { return tc.reply }}), "m")
		v, err := c.JudgeRevision(ctx, req)
		if err != nil {
			t.Fatalf("%s: err=%v", tc.reply, err)
		}
		if v.Addressed != tc.wantYes || v.Reason != tc.wantReason {
			t.Errorf("%s → %+v, want addressed=%v reason=%q", tc.reply, v, tc.wantYes, tc.wantReason)
		}
	}
}

// An object that parses but carries no readable verdict still fails open — a judge that answers
// in a shape this code cannot read must not cut a productive loop. It is reported as its own case,
// because calling a whole, well-formed reply "unparseable" sends the next reader after the stream.
func TestJudgeRevisionFailsOpenOnAnUnreadableVerdict(t *testing.T) {
	ctx := context.Background()
	req := port.RevisionJudgeRequest{Critique: "size A1", PriorPlan: "1. compute", RevisedPlan: "1. compute"}

	for _, reply := range []string{
		`{"addressed": "probably", "reason": "hard to say"}`,
		`{"reason": "no verdict field at all"}`,
	} {
		c := New(only(fakeLLM{reply: func(port.ChatRequest) string { return reply }}), "m")
		v, _ := c.JudgeRevision(ctx, req)
		if !v.Addressed || !strings.Contains(v.Reason, "no readable verdict") {
			t.Errorf("%s → %+v, want fail-open with a 'no readable verdict' reason", reply, v)
		}
	}
}

func TestEvidenceBudgetNote(t *testing.T) {
	// Low remaining budget → a note telling members to prefer DONE over unactionable rounds.
	low := evidence(port.DeliberationRequest{Task: "x", Report: "y", StepsLeft: 3})
	if !strings.Contains(low, "# Budget") || !strings.Contains(low, "3 step") || !strings.Contains(low, "prefer DONE") {
		t.Errorf("low budget should render the budget note:\n%s", low)
	}
	// Ample budget → no note (don't rush the council when there's room).
	if e := evidence(port.DeliberationRequest{Task: "x", Report: "y", StepsLeft: 40}); strings.Contains(e, "# Budget") {
		t.Errorf("ample budget should not render a budget note:\n%s", e)
	}
	// Plan-audit phase never carries a budget note (there's no execution budget to spend yet).
	if e := evidence(port.DeliberationRequest{Phase: "plan", Task: "x", Plan: "p", StepsLeft: 1}); strings.Contains(e, "# Budget") {
		t.Errorf("plan phase should not render a budget note:\n%s", e)
	}
}

// A report that rationalizes incompletion ("impossible, so this is full completion",
// "nothing needed fixing") must be treated as an admission, not a done — the clause
// The keep clause + schema field appear ONLY when keep is requested (MAGI_COUNCIL_KEEP),
// so the baseline prompt is byte-for-byte unchanged when it is off.
func TestMemberPromptKeepGated(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	off := memberSystem(m, "terminate", "fix the bug", false, false)
	if strings.Contains(off, "\"keep\"") || strings.Contains(off, "must NOT redo or revert") {
		t.Error("keep clause/schema must be absent when keep is off")
	}
	on := memberSystem(m, "terminate", "fix the bug", true, false)
	if !strings.Contains(on, "must NOT redo or revert") {
		t.Error("keep clause missing when keep is on")
	}
	if !strings.Contains(on, "\"keep\"") {
		t.Error("keep schema field missing when keep is on")
	}
	// Advisory framing: it must say it does not change the vote.
	if !strings.Contains(on, "NEVER changes your decision") {
		t.Error("keep clause must state it is advisory (never changes the decision)")
	}
}

// Plan-phase keep is gated the same way and, crucially, asks each member to note what to preserve
// EVEN WHEN APPROVING — so a revision forced by another member's flaw doesn't drop the good steps.
// A re-audited plan carries its revision history in the evidence so a member can judge the
// DELTA (a rewrite that dropped prior work is a regression). The section must appear only when
// there is a revision to describe, and — like every other prompt surface — must stay
// task-agnostic: the framing is ours, the specifics come from the run.
func TestPlanEvidenceCarriesRevision(t *testing.T) {
	rev := "The concern that forced this revision: add a step that produces the artifact\n" +
		"The plan BEFORE the revision: 1. survey the inputs\nConvergence judge: the revision did NOT engage the concern"
	e := evidence(port.DeliberationRequest{Phase: "plan", Task: "build a server", Plan: "1. survey", Revision: rev})
	if !strings.Contains(e, "REVISED") {
		t.Errorf("plan evidence must label the revision section:\n%s", e)
	}
	if !strings.Contains(e, "DROPPED or WEAKENED") {
		t.Errorf("the revision section must tell members a regression counts, not just a step-count change:\n%s", e)
	}
	if !strings.Contains(e, "the revision did NOT engage the concern") {
		t.Errorf("the judge verdict must reach the members:\n%s", e)
	}
	// No revision → no section (a first-round audit must not be told about a rewrite that never happened).
	if plain := evidence(port.DeliberationRequest{Phase: "plan", Task: "build a server", Plan: "1. survey"}); strings.Contains(plain, "REVISED") {
		t.Errorf("first-round plan evidence must not carry a revision section:\n%s", plain)
	}
	for _, banned := range []string{"grpcio", "kv-store", "cobol", "1.73", "extract.js"} {
		if strings.Contains(e, banned) {
			t.Errorf("revision section leaks eval-set token %q", banned)
		}
	}
}

func TestPlanMemberPromptKeepGated(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	off := memberSystem(m, "plan", "build a server", false, false)
	if strings.Contains(off, "\"keep\"") || strings.Contains(off, "EVEN WHEN YOU APPROVE") {
		t.Error("plan keep clause/schema must be absent when keep is off")
	}
	on := memberSystem(m, "plan", "build a server", true, false)
	if !strings.Contains(on, "EVEN WHEN YOU APPROVE") {
		t.Error("plan keep clause must ask to preserve even on approve")
	}
	if !strings.Contains(on, "\"keep\"") {
		t.Error("plan keep schema field missing when keep is on")
	}
	if !strings.Contains(on, "never changes your vote") {
		t.Error("plan keep clause must state it is advisory")
	}
}

// The plan-audit member authors checks as DATA, so its prompt must say what that shape buys: the
// runner reads `source` and applies `assert` with no shell, so a check cannot re-do the step's work,
// cannot mutate anything, and cannot false-fail on a missing tool. Those three used to be prose
// warnings a model could ignore; the prompt must now state that they are gone by construction, and
// must spell out the closed vocabulary the runner actually understands.
func TestPlanMemberPromptSeparatesWorkFromCheck(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "download and analyze the results", false, false)
	for _, want := range []string{
		"no shell in the path", "CANNOT re-do the step's work", "CANNOT mutate anything",
		"RECORD AND READ IS THE ONLY SHAPE", "belongs to", "nonempty", "matches <regexp>",
		"absent <regexp>", "equals <path>", "port_open <port>", "process_alive",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must state the typed shape (missing %q)", want)
		}
	}
}

// The check-authoring prompt must require an INTEGER step label (so the numeric gate matches
// instead of falling back to a flattened union) and state that the per-step checks be jointly
// satisfiable and checklist-driven — the guard against the plexus #224 contradictory checklist.
func TestPlanMemberPromptScopesChecksToSteps(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "compress, extract, analyze, then clean up", false, false)
	for _, want := range []string{"INTEGER STEP NUMBER", "JOINTLY", "CHECKLIST-DRIVEN"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must scope checks to steps (missing %q)", want)
		}
	}
}

// The terminate member prompt must judge a CHECK-SUBSTITUTION: accept an equivalent that reasonably
// verifies the goal, reject an inadequate one.
func TestTerminateMemberPromptCheckSubstitution(t *testing.T) {
	m := council.Member{Name: "x", Lens: "verification"}
	p := memberSystem(m, "terminate", "stand up a service", false, false)
	for _, want := range []string{"CHECK-SUBSTITUTION", "EQUIVALENT", "REASONABLY verifies", "INADEQUATE"} {
		if !strings.Contains(p, want) {
			t.Errorf("terminate prompt missing check-substitution fragment %q", want)
		}
	}
}

// The scope/boundary clause is GATED by MAGI_CONSTRAINT_GATE (the constraints arg): OFF by default so
// it does not add a rejection criterion to an already over-strict council, ON only for the A/B arm.
// When ON it verifies stated scope/boundary constraints against the diff/artifact — an off-limits file
// edited, a required structural element missing, a forbidden action taken — with a necessity guard
// against inventing a limit the task never stated. Grounds the observed self-acknowledged constraint
// violation (an agent editing a protected file it knew was off-limits). Task-agnostic: no eval tokens.
func TestTerminateMemberPromptScopeBoundary(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	// OFF (default): the clause is absent — byte-identical baseline, no added rejection criterion.
	if off := memberSystem(m, "terminate", "fix the crash without touching the shared files", false, false); strings.Contains(off, "SCOPE and BOUNDARY") {
		t.Error("scope/boundary clause must be ABSENT when the constraint gate is off (default)")
	}
	// ON (A/B arm): the clause is present with its guards.
	p := memberSystem(m, "terminate", "fix the crash without touching the shared files", false, true)
	for _, want := range []string{"SCOPE and BOUNDARY", "OFF-LIMITS", "out-of-scope edit", "violating constraint", "invent a limit the task never stated"} {
		if !strings.Contains(p, want) {
			t.Errorf("terminate prompt missing scope/boundary fragment %q", want)
		}
	}
	for _, banned := range []string{"user.cpp", "main.cpp", ".vim", "kv-store"} {
		if strings.Contains(p, banned) {
			t.Errorf("terminate prompt leaks an eval-set token %q", banned)
		}
	}
}

// The plan member prompt must carry the CONTEST clause (re-judge a contested concern against the
// task, drop an over-demand), and evidence must render the author's contest for the plan phase.
func TestPlanMemberPromptContest(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "build a thing", false, false)
	for _, want := range []string{"CONTEST", "OVER-DEMAND", "ground in the task"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan prompt missing contest fragment %q", want)
		}
	}
	e := evidence(port.DeliberationRequest{Phase: "plan", Task: "t", Plan: "1. do it", Contest: "the task never asks for retries"})
	if !strings.Contains(e, "CONTESTED") || !strings.Contains(e, "never asks for retries") {
		t.Errorf("plan evidence must render the contest: %q", e)
	}
}

// The contract-gate member prompt (Phase=="contract") bounds the acceptance contract on BOTH
// sides: a LOWER bound (sufficiency — exercise the behavior, not mere existence of a stub) and an
// The contract member prompt agrees GOAL-level criteria only (no executable checks at contract time),
// is bounded by necessity + sufficiency, and is LENIENT — it must not demand what only doing reveals.
// memberSystem must route the contract phase to it, and it must stay task-agnostic (no eval-set tokens).
func TestContractMemberPromptBounds(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "contract", "implement the interface", false, false)
	for _, want := range []string{"ACCEPTANCE CONTRACT", "NECESSITY", "SUFFICIENCY", "LENIENT", "GOAL", "APPROVE"} {
		if !strings.Contains(p, want) {
			t.Errorf("contract member prompt missing %q", want)
		}
	}
	// Goals only — the contract phase must NOT solicit executable checks/commands.
	if strings.Contains(p, `"checks"`) || strings.Contains(p, "shell `command`") {
		t.Errorf("contract prompt must not author executable checks at contract time:\n%s", p)
	}
	for _, banned := range []string{"grpcio", "kv-store", "headless"} {
		if strings.Contains(p, banned) {
			t.Errorf("contract prompt leaks eval-set token %q", banned)
		}
	}
}

// The contract phase renders only the task (and any draft to refine) — no plan-audit "procedure"
// framing, since no plan exists yet.
func TestContractEvidenceRendersTaskAndDraft(t *testing.T) {
	e := evidence(port.DeliberationRequest{Phase: "contract", Task: "build the widget", Plan: "draft: it must run"})
	if !strings.Contains(e, "build the widget") || !strings.Contains(e, "draft: it must run") {
		t.Errorf("contract evidence must render task and draft: %q", e)
	}
	if strings.Contains(e, "procedure") {
		t.Errorf("contract evidence must not use the plan-audit procedure framing: %q", e)
	}
}

// The terminate member prompt must carry the per-item acceptance clause: when the criteria are an
// enumerated checklist, judge each item and land done only if EVERY item is satisfied.
func TestTerminateMemberPromptPerItem(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	p := memberSystem(m, "terminate", "stand up a service", false, false)
	for _, want := range []string{"PER-ITEM ACCEPTANCE", "NUMBERED checklist", "EVERY item", "UNSATISFIED"} {
		if !strings.Contains(p, want) {
			t.Errorf("terminate prompt missing per-item clause fragment %q", want)
		}
	}
}

// The check-authoring prompt must forbid over-demand: a check may assert only what the task states,
// never a version/build-id/incidental the task did not pin. Over-specification false-fails correct
// work and never converges — the mirror of the too-weak file-existence trap.
// A continue demand for a task-unspecified specific (a type width, version pin, or identifier
// spelling) must be grounded in the task's own words — the terminate-phase member prompt has to
// place that burden on the member, or a phantom requirement churns a correct deliverable to the
// wall clock (kv-store: a council int64 demand the grader never checked, cost an AgentTimeout).
func TestMemberPromptGroundsDemandsInTask(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "terminate", "stand up a service", false, false)
	for _, want := range []string{"GROUND every continue demand in the TASK", "where the TASK", "phantom requirement"} {
		if !strings.Contains(p, want) {
			t.Errorf("terminate member prompt must require continue demands be grounded in the task (missing %q)", want)
		}
	}
	// de-overfit: the grounding clause must illustrate the failure mode without eval-set tokens.
	for _, banned := range []string{"grpcio", "kv-store", "int64", "int32"} {
		if strings.Contains(p, banned) {
			t.Errorf("terminate member prompt leaks eval-set-specific token %q — keep the example task-agnostic", banned)
		}
	}
}

// Evidence-method over-demand (the gap #371 exposed): a member dismissed a PASSING in-process test
// as "mere simulation" and demanded a harder real-world reproduction the task never required — the
// same task passed elsewhere without it. The terminate-phase prompt must forbid upgrading the
// ACCEPTANCE METHOD beyond what the task implies, WHILE still refusing a behavior only claimed, faked,
// or never actually run. Task-agnostic (no SIGINT/cancel tokens).
func TestMemberPromptForbidsEvidenceMethodOverDemand(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "terminate", "make the handler resilient", false, false)
	for _, want := range []string{"mere simulation", "real-world reproduction", "ACCEPTANCE METHOD", "already shown working"} {
		if !strings.Contains(p, want) {
			t.Errorf("terminate prompt must forbid evidence-method over-demand (missing %q)", want)
		}
	}
	// It must NOT weaken the legitimate strictness a faked/never-run behavior owes (the #372 case).
	if !strings.Contains(p, "CLAIMED, faked, or never actually run") {
		t.Error("the extension must still refuse a claimed/faked/never-run behavior")
	}
	// de-overfit: no eval-set task token from the case that motivated it.
	for _, banned := range []string{"SIGINT", "cancel-async", "Ctrl-C", "KeyboardInterrupt"} {
		if strings.Contains(p, banned) {
			t.Errorf("prompt leaks eval-set token %q — keep it task-agnostic", banned)
		}
	}
}

// The devil advocate hunts for a reason the turn is not done, so it too can manufacture a
// task-unspecified specific (the reviewDevil round catches spurious ones downstream, but the
// concern should be grounded at the source, consistent with the members' obligation).
func TestDevilPromptGroundsDemandsInTask(t *testing.T) {
	for _, want := range []string{"When that defect is itself a SPECIFIC", "where the TASK ITSELF states it", "manufactured doubt"} {
		if !strings.Contains(devilSystem, want) {
			t.Errorf("devil prompt must require a specific defect be grounded in the task (missing %q)", want)
		}
	}
	for _, banned := range []string{"grpcio", "kv-store", "int64", "int32"} {
		if strings.Contains(devilSystem, banned) {
			t.Errorf("devil prompt leaks eval-set-specific token %q — keep the example task-agnostic", banned)
		}
	}
}

func TestPlanMemberPromptForbidsOverDemand(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "install a dependency and run a server", false, false)
	for _, want := range []string{"do NOT demand MORE than the task states", "Over-specification", "minimal condition"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must forbid over-demand (missing %q)", want)
		}
	}
}

// The sufficiency floor: the check-authoring prompt must reject proxy-only checks. Reaching the artifact
// (exists, port accepts a connection, module imports, build succeeds, process alive) is a precondition a
// non-functional stub also passes; the prompt must demand the check invoke the stated behavior and assert
// the result, choosing the weakest input that forces the real code path.
func TestPlanMemberPromptDemandsContractExercise(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "install a dependency and run a server", false, false)
	for _, want := range []string{"PRECONDITION, not proof", "non-functional stub", "weakest input", "real code path"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must reject proxy-only (too-weak) checks (missing %q)", want)
		}
	}
}

// Guard against benchmark overfitting: no eval-set task's exact command, filename, or value may be
// baked into a prompt the model sees. Every phase and lens is swept, not just the check-authoring
// one: an audit found leaks in three prompts that no guard read, while the two that were guarded
// stayed clean — a prompt no test reads is a prompt that drifts back toward the eval set.
func TestPlanMemberPromptNoEvalSetSpecifics(t *testing.T) {
	banned := []string{
		"pmars", "flashpaper", "rave.red", "extract-elf", "extract.js", "a.out", "grpcio", "kv-store",
		"ocaml", "cobol", "compcert", "corewars", "caffe", "cifar", "qemu", "fasttext", "sparql",
		"grpc", "_pb2", ".proto", "gcov", "opam", "valgrind", "sqlite",
		"208", "377", "Cleaned up",
	}
	for _, phase := range []string{"", "plan", "contract", "substitution", "terminate"} {
		for _, lens := range []string{"correctness", "verification", "completeness"} {
			m := council.Member{Name: "x", Lens: lens}
			for _, keep := range []bool{false, true} {
				for _, cons := range []bool{false, true} {
					p := memberSystem(m, phase, "build the thing", keep, cons)
					for _, b := range banned {
						if strings.Contains(p, b) {
							t.Errorf("phase=%q lens=%q keep=%v cons=%v leaks eval-set-specific token %q — use a task-agnostic example",
								phase, lens, keep, cons, b)
						}
					}
				}
			}
		}
	}
}

// that closes the reval3 play-zork / run-pdp11 / fasttext class of false approvals.
func TestMemberPromptRationalizedDone(t *testing.T) {
	m := council.Member{Name: "x", Lens: "verification"}
	s := memberSystem(m, "terminate", "beat the game", false, false)
	if !strings.Contains(s, "RATIONALIZES incompletion") {
		t.Error("terminate prompt missing the rationalized-done clause")
	}
	if !strings.Contains(s, "ADMISSION") {
		t.Error("rationalized-done clause must frame the excuse as an admission")
	}
	// The escape hatch must point at an honest failed/blocked report, not a lowered bar.
	if !strings.Contains(s, "failed/blocked") {
		t.Error("rationalized-done clause missing the honest failed/blocked exit")
	}
	// Checkable behavior demands a real run: existence of the artifact is not enough
	// (reval3: password-recovery/create-bucket/new-encrypt-command all passed council
	// 3:0 on unexercised artifacts, then failed the task tests).
	if !strings.Contains(s, "Existence is not correctness") {
		t.Error("terminate prompt missing the verification-run clause")
	}
	// Plan phase judges a procedure before any report exists — the clause must not leak.
	if p := memberSystem(m, "plan", "beat the game", false, false); strings.Contains(p, "RATIONALIZES incompletion") {
		t.Error("rationalized-done clause leaked into the plan-audit prompt")
	}
}

// A council-invented verification must state the OBJECTIVE and leave the method to the agent,
// never prescribe a specific inspection command that may be absent in the container. A passing
// end-to-end exercise satisfies the must-respond/run bar (kv-store-grpc run17: `ps: not found`
// made the council reject a live, working gRPC server across 3 rounds because it demanded a
// process listing instead of crediting the successful client round-trip).
func TestMemberPromptObjectiveNotMethod(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	s := memberSystem(m, "terminate", "run a server on port 5328", false, false)

	// The old wording prescribed the method; it must be gone.
	if strings.Contains(s, "name the exact check to run") {
		t.Error("prompt still tells the member to name the exact check (prescribes method)")
	}
	// It must ask for the objective and delegate the how.
	if !strings.Contains(s, "name the OBJECTIVE still to be shown true") {
		t.Error("prompt must ask the member to name the objective, not a command")
	}
	if !strings.Contains(s, "leave HOW to the agent") {
		t.Error("prompt must delegate the verification method to the agent")
	}
	// It must forbid prescribing an environment-specific inspection command.
	if !strings.Contains(s, "ps/netstat/lsof/curl/pgrep") {
		t.Error("prompt must forbid prescribing a specific inspection command")
	}
	// A passing end-to-end exercise must be accepted as the run (no extra process/port listing).
	if !strings.Contains(s, "working end-to-end") || !strings.Contains(s, "ritual churn") {
		t.Error("prompt must credit a passing end-to-end exercise instead of demanding a listing")
	}
	// The task-specified literal-contract requirement must remain intact.
	if !strings.Contains(s, "EXACT command was run") {
		t.Error("literal task-contract requirement was lost")
	}
}

// When the report contests a prior demand, the member must adjudicate the cited evidence:
// if it shows the requirement met or the method impossible-as-stated, drop the demand (do
// not reissue); but a contest only removes that one point and is never itself proof of done,
// and a contest with no concrete evidence is disregarded (false-done guard).
func TestMemberPromptContestAdjudication(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	s := memberSystem(m, "terminate", "run a server on port 5328", false, false)

	if !strings.Contains(s, "CONTEST") {
		t.Error("terminate prompt must instruct the member how to judge a CONTEST")
	}
	// Valid contest -> do not reissue the demand.
	if !strings.Contains(s, "do NOT reissue it") {
		t.Error("a valid contest must stop the member from reissuing the demand")
	}
	// Removal-only: never itself proof of done.
	if !strings.Contains(s, "NEVER "+"itself evidence the whole task is done") {
		t.Error("contest must be removal-only, never itself proof of done")
	}
	// Evidence bar: a no-evidence contest is disregarded (keeps the false-done guard).
	if !strings.Contains(s, "disregard it and keep the demand") {
		t.Error("a contest with no concrete evidence must be disregarded")
	}
	// Plan phase judges a procedure with no report — the terminate-only clause must not leak.
	if p := memberSystem(m, "plan", "run a server", false, false); strings.Contains(p, "do NOT reissue it") {
		t.Error("contest-adjudication clause leaked into the plan-audit prompt")
	}
}

func TestMemberPromptArtifactGrounding(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	s := memberSystem(m, "terminate", "build a CLI tool", false, false)
	if !strings.Contains(s, "is NOT itself the artifact") {
		t.Error("terminate prompt missing artifact-grounding clause")
	}
	// no-churn balance retained (existing wording):
	if !strings.Contains(s, "ABSENCE of a diff or signal is NEVER a reason to continue") {
		t.Error("artifact clause must not displace the no-churn balance")
	}
	// read-only carve-out retained:
	if !strings.Contains(s, "investigation") {
		t.Error("read-only carve-out lost")
	}
	// The deliverable is anchored to the user's TASK, not the plan/criteria wording —
	// this is what stops a review task's "write a summary" step being read as a file.
	if !strings.Contains(s, "USER'S TASK") {
		t.Error("deliverable not anchored to the user's task")
	}
	// Files the agent only READ are inputs, never missing deliverables (the README-as-
	// missing-deliverable misfire).
	if !strings.Contains(s, "INPUTS") {
		t.Error("inputs-are-not-deliverables clause missing")
	}
	// The file/diff/document prohibition (handles "you didn't create README.md").
	if !strings.Contains(s, "never demand a") {
		t.Error("review-task file prohibition missing")
	}
	// A "write a summary" step is satisfied by the report (handles "summary not written").
	if !strings.Contains(s, "write/produce a summary") {
		t.Error("summary-step-satisfied-by-report clause missing")
	}
	// terminate-only: the plan-audit prompt must NOT demand artifacts pre-flight, nor
	// carry the terminate-phase artifact framing.
	p := memberSystem(m, "plan", "build a CLI tool", false, false)
	if strings.Contains(p, "is NOT itself the artifact") {
		t.Error("artifact clause leaked into the plan-audit prompt")
	}
	if strings.Contains(p, "USER'S TASK") || strings.Contains(p, "INPUTS") {
		t.Error("terminate-phase artifact framing leaked into the plan-audit prompt")
	}
	// The plan-audit criteria instruction must steer review tasks away from inventing a
	// file deliverable (the second channel that injected the false artifact).
	if !strings.Contains(p, "never a new file") {
		t.Error("plan criteria instruction missing the review-task carve-out")
	}
	if strings.Contains(s, "never a new file") {
		t.Error("plan-only criteria carve-out leaked into the terminate prompt")
	}
}

// TestMemberPromptProportionality guards the analysis/survey calibration: neither
// phase may derive or enforce an exhaustive "list ALL N with EXACT lines" contract
// for a large-set analysis task (the '리팩토링 요소 찾아줘' loop, where plan-audit
// approved an impossible contract the completion council then enforced).
func TestMemberPromptProportionality(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}

	// terminate phase: representative coverage of a large set is done; demanding
	// exhaustive enumeration / atom-level precision is churn, not a defect.
	s := memberSystem(m, "terminate", "find refactoring candidates", false, false)
	if !strings.Contains(s, "EXHAUSTIVE enumeration") {
		t.Error("terminate prompt missing the proportional-completeness clause")
	}
	if !strings.Contains(s, "reasonably and representatively") &&
		!strings.Contains(s, "REASONABLY and representatively") {
		t.Error("terminate prompt missing the representative-coverage bar")
	}
	// The carve-out must NOT relax the concrete-deliverable gate — anchored to any
	// CREATE/BUILD/RUN/FIX PART, so a compound "analyze + fix" task can't route the
	// fix half into the relaxed analyze branch (reviewer Finding 1).
	if !strings.Contains(s, "CREATE/BUILD/RUN/FIX PART") {
		t.Error("terminate proportionality carve-out not anchored to the concrete-deliverable PART")
	}
	// Guard the guardrail: proportionality must sit ALONGSIDE, not replace, the
	// existence/correctness/run-the-check anchors it defers to. A regression that
	// deletes those paragraphs must fail here, not pass green (reviewer Finding 2).
	if !strings.Contains(s, "Existence is not correctness") {
		t.Error("run-the-check anchor gone — proportionality must not displace it")
	}
	if !strings.Contains(s, "actually RAN that check") {
		t.Error("the 'must actually run the check' requirement is gone")
	}
	if !strings.Contains(s, "RATIONALIZES incompletion") {
		t.Error("the rationalized-incompletion anchor is gone")
	}

	// plan phase: criteria must be achievable/proportionate — no "all N with exact
	// lines" done-condition; the old exhaustive "every doc is covered" example is gone.
	p := memberSystem(m, "plan", "find refactoring candidates", false, false)
	if !strings.Contains(p, "ACHIEVABLE and PROPORTIONATE") {
		t.Error("plan criteria instruction missing the proportionality guidance")
	}
	if !strings.Contains(p, "EXHAUSTIVE enumeration") {
		t.Error("plan criteria instruction missing the no-exhaustive-enumeration steer")
	}
	if strings.Contains(p, "every doc is covered") {
		t.Error("stale exhaustive 'every doc is covered' example still present")
	}
	// Guard the plan-side carve-out too: the criteria relaxation must keep requiring
	// a concrete artifact + check for a CREATE/BUILD/RUN/FIX task (reviewer Finding 2).
	if !strings.Contains(p, "CREATE/BUILD/RUN/FIX") {
		t.Error("plan criteria carve-out for concrete-deliverable tasks is gone")
	}
	// terminate-only proportionality framing must not leak into the plan prompt, and
	// vice-versa — each phase keeps its own wording.
	if strings.Contains(p, "reasonably and representatively") {
		t.Error("terminate-phase proportionality framing leaked into the plan prompt")
	}
}

// The plan-audit lens must guide, not reject, an intentionally abstract refine step
// (abstractness is expanded at execution time) WITHOUT waving through an absurd plan —
// a genuinely unsound abstract plan is still critical. This is the ①/② balance the whole
// refine strategy leans on; it lives in the plan prompt only.
func TestMemberPromptRefine(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	p := memberSystem(m, "plan", "build a small interpreted language", false, false)

	// Abstractness alone is never a critical revision.
	if !strings.Contains(p, "NEVER critical-revise a refine step for abstractness") {
		t.Error("plan prompt missing the refine 'abstractness is not a flaw' carve-out")
	}
	// …but the carve-out is not a pass for a bad plan: an unsound abstract plan stays critical.
	if !strings.Contains(p, "STILL critical") || !strings.Contains(p, "Reject the absurd, approve the merely abstract") {
		t.Error("plan prompt missing the 'absurd abstract plan is still critical' balance")
	}
	// The refine guidance is plan-audit only — it must not leak into the terminate prompt.
	if s := memberSystem(m, "terminate", "build a small interpreted language", false, false); strings.Contains(s, "critical-revise a refine step") {
		t.Error("refine plan-audit guidance leaked into the terminate prompt")
	}
}

func TestDeliberateAllDone(t *testing.T) {
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		return `{"decision":"done","confidence":0.9,"rationale":"looks complete"}`
	}}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done", d.Decision)
	}
	if len(d.Verdicts) != 3 { // defaults to the MAGI
		t.Fatalf("verdicts = %d, want 3 (default members)", len(d.Verdicts))
	}
}

func TestDeliberateMajorityContinueWithFeedback(t *testing.T) {
	// Melchior + Casper say continue (with feedback), Balthasar says done →
	// majority continue.
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if memberIn(r, "Balthasar") {
			return `{"decision":"done","rationale":"tests pass"}`
		}
		return `{"decision":"continue","rationale":"incomplete","feedback":"add the missing flag"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 2, Task: "do x", Rule: council.RuleMajority})
	if d.Decision != council.Continue {
		t.Fatalf("decision = %q, want continue", d.Decision)
	}
	if !strings.Contains(d.Feedback, "add the missing flag") {
		t.Fatalf("feedback should aggregate continuing members:\n%s", d.Feedback)
	}
}

func TestDeliberateUnparseableAbstains(t *testing.T) {
	// No JSON anywhere → every member abstains → tally resolves to Continue.
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		return "I think it is probably fine, hard to say really."
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if d.Decision != council.Continue {
		t.Fatalf("decision = %q, want continue (all abstained)", d.Decision)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain {
			t.Fatalf("member %s = %q, want abstain", v.Member, v.Decision)
		}
	}
}

func TestDeliberateProviderErrorAbstains(t *testing.T) {
	c := New(only(fakeLLM{err: errors.New("backend down")}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if d.Decision != council.Continue {
		t.Fatalf("decision = %q, want continue (errors abstain)", d.Decision)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain || !strings.Contains(v.Rationale, "unavailable") {
			t.Fatalf("member %s verdict = %+v, want abstain/unavailable", v.Member, v)
		}
	}
}

func TestDeliberateCustomMembersAndModel(t *testing.T) {
	var sawModel string
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		sawModel = r.Model
		return `{"decision":"done"}`
	}}), "default-model")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round:   1,
		Task:    "x",
		Members: []council.Member{{Name: "Solo", Lens: "correctness", Model: "special-model"}},
		Rule:    council.RuleUnanimous,
	})
	if len(d.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(d.Verdicts))
	}
	if sawModel != "special-model" {
		t.Fatalf("member model = %q, want special-model (member override)", sawModel)
	}
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done", d.Decision)
	}
}

// Each member is polled over the backend its provider name resolves to, so cheap
// and strong models can be mixed.
func TestDeliberatePerMemberProvider(t *testing.T) {
	// The resolver returns a backend whose verdict depends on the provider name,
	// so a member's decision reveals which backend it was routed to.
	resolve := func(name string) port.LLMProvider {
		dec := "done"
		if name == "weak" {
			dec = "continue"
		}
		return fakeLLM{reply: func(port.ChatRequest) string {
			return `{"decision":"` + dec + `","feedback":"x"}`
		}}
	}
	c := New(resolve, "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "x", Rule: council.RuleUnanimous,
		Members: []council.Member{
			{Name: "A", Provider: "weak"}, // routed to the "weak" backend
			{Name: "B"},                   // default backend
		},
	})
	got := map[string]council.Decision{}
	for _, v := range d.Verdicts {
		got[v.Member] = v.Decision
	}
	if got["A"] != council.Continue {
		t.Fatalf("member A (provider=weak) = %q, want continue", got["A"])
	}
	if got["B"] != council.Done {
		t.Fatalf("member B (default backend) = %q, want done", got["B"])
	}
}

// A member with no model uses the request's DefaultModel (the session model).
func TestDeliberateDefaultModel(t *testing.T) {
	var sawModel string
	c := New(func(string) port.LLMProvider {
		return fakeLLM{reply: func(r port.ChatRequest) string {
			sawModel = r.Model
			return `{"decision":"done"}`
		}}
	}, "fallback")
	c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "x", DefaultModel: "session-model",
		Members: []council.Member{{Name: "A", Lens: "correctness"}},
	})
	if sawModel != "session-model" {
		t.Fatalf("model = %q, want session-model (req.DefaultModel)", sawModel)
	}
}

func TestFirstJSONObject(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"prose before {\"a\":1} and after", `{"a":1}`},
		{"```json\n{\"a\":{\"b\":2}}\n```", `{"a":{"b":2}}`},
		{`{"s":"has } brace"}`, `{"s":"has } brace"}`},
		// Escaped quotes inside a string: the \" must NOT end the string, so the } that
		// follows stays string-interior and is not counted — exercises the esc state path.
		{`{"s":"he said \"hi\" }"}`, `{"s":"he said \"hi\" }"}`},
		// Unbalanced: an object that never closes yields "" (no balanced match).
		{`{"a":1`, ""},
		{"no json here", ""},
	}
	for _, tc := range cases {
		if got := firstJSONObject(tc.in); got != tc.want {
			t.Errorf("firstJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEvidenceRendersSignals(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task:    "fix the bug",
		Report:  "fixed it",
		Signals: []port.Signal{{Source: "verify", Kind: "test", Status: "fail", Detail: "--- FAIL: TestX"}},
	})
	if !strings.Contains(got, "[verify/test] fail") {
		t.Fatalf("evidence missing signal header:\n%s", got)
	}
	if !strings.Contains(got, "--- FAIL: TestX") {
		t.Fatalf("evidence missing signal detail:\n%s", got)
	}
}

func TestParseReplyRequiresDecision(t *testing.T) {
	if _, ok := parseReply(`{"rationale":"no decision field"}`); ok {
		t.Fatal("reply without a decision should not parse")
	}
	if r, ok := parseReply(`{"decision":"DONE"}`); !ok || decisionOf(string(r.Decision)) != council.Done {
		t.Fatalf("uppercase DONE should parse to done, got ok=%v r=%+v", ok, r)
	}
}

// Debate: a split independent vote triggers one rebuttal round. Here Melchior is
// shown the majority's done votes and flips to done → consensus. The rebuttal reply
// is detectable by the peer-digest section in the prompt.
func TestDeliberateDebateResolvesSplit(t *testing.T) {
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		rebuttal := strings.Contains(textOf(r), "Council disagreement")
		if memberIn(r, "Melchior") {
			if rebuttal { // reconsiders and joins the majority
				return `{"decision":"done","rationale":"peers are right, tests do cover it"}`
			}
			return `{"decision":"continue","rationale":"looks incomplete"}`
		}
		return `{"decision":"done","rationale":"tests pass"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: true,
	})
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done after debate", d.Decision)
	}
}

// Debate off (or unanimous) never triggers a rebuttal: a member that would flip on
// rebuttal keeps its independent vote, so a genuine split stands under the rule.
func TestDeliberateNoDebateKeepsSplit(t *testing.T) {
	// Members poll concurrently, so the reply callback runs in parallel goroutines:
	// use atomics, never touch *testing.T from inside it (that is itself a data race).
	var calls, rebuttals int64
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		atomic.AddInt64(&calls, 1)
		if strings.Contains(textOf(r), "Council disagreement") {
			atomic.AddInt64(&rebuttals, 1)
		}
		if memberIn(r, "Melchior") {
			return `{"decision":"continue","rationale":"incomplete","feedback":"more"}`
		}
		return `{"decision":"done","rationale":"ok"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: false,
	})
	if n := atomic.LoadInt64(&rebuttals); n != 0 {
		t.Errorf("rebuttal round ran %d time(s) with Debate=false", n)
	}
	if d.Decision != council.Done { // 2 done / 1 continue → majority done, no debate
		t.Fatalf("decision = %q, want done (majority, no debate)", d.Decision)
	}
	if calls := atomic.LoadInt64(&calls); calls != 3 {
		t.Fatalf("want exactly 3 polls (no rebuttal), got %d", calls)
	}
}

// Debate is skipped when the independent tally is already Continue: the dissent can't
// change the outcome, and debate must never be used to talk a hesitant council into
// done. Melchior+Casper continue, Balthasar done → continue-majority → no rebuttal,
// exactly 3 polls.
func TestDeliberateSkipDebateOnContinueMajority(t *testing.T) {
	var calls, rebuttals int64
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		atomic.AddInt64(&calls, 1)
		if strings.Contains(textOf(r), "Council disagreement") {
			atomic.AddInt64(&rebuttals, 1)
		}
		if memberIn(r, "Balthasar") {
			return `{"decision":"done","rationale":"looks fine"}`
		}
		return `{"decision":"continue","rationale":"incomplete","feedback":"more"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: true,
	})
	if n := atomic.LoadInt64(&rebuttals); n != 0 {
		t.Errorf("debate must be skipped on a continue-majority, ran %d time(s)", n)
	}
	if n := atomic.LoadInt64(&calls); n != 3 {
		t.Errorf("want exactly 3 polls (no rebuttal), got %d", n)
	}
	if d.Decision != council.Continue {
		t.Errorf("decision = %q, want continue", d.Decision)
	}
	if d.Debate != nil {
		t.Errorf("no DebateOutcome expected when skipped, got %+v", d.Debate)
	}
}

func isDevil(r port.ChatRequest) bool { return strings.Contains(r.System, "devil's advocate") }
func isDevilReview(r port.ChatRequest) bool {
	return strings.Contains(textOf(r), "judge it CRITICALLY")
}

// Devil as a critically-reviewed input: on a UNANIMOUS done the devil raises a concern, the
// members RE-JUDGE it, and if a member AGREES the concern is a real defect the turn continues.
func TestDeliberateDevilConcernUpheld(t *testing.T) {
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if isDevil(r) {
			return `{"decision":"continue","rationale":"server never started","feedback":"run the server and show it binds :5328"}`
		}
		if isDevilReview(r) { // members review the concern and agree it's real
			return `{"decision":"continue","rationale":"right, no run shown","feedback":"actually run it"}`
		}
		return `{"decision":"done","rationale":"looks complete"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Devil: true,
	})
	if d.Decision != council.Continue {
		t.Fatalf("decision = %q, want continue (members upheld the devil's real concern)", d.Decision)
	}
}

// The key regression fix: a SPURIOUS devil concern (int32→int64 that the grader does not require)
// is REJECTED on critical review — the members hold done, so a working solution is not overturned.
func TestDeliberateDevilConcernRejected(t *testing.T) {
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if isDevil(r) {
			return `{"decision":"continue","rationale":"could be int64","feedback":"change int32 to int64"}`
		}
		if isDevilReview(r) { // members judge critically: int32 satisfies the task → hold done
			return `{"decision":"done","rationale":"int32 meets the spec; the devil overreaches"}`
		}
		return `{"decision":"done","rationale":"works"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Devil: true,
	})
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done (spurious devil concern rejected on review)", d.Decision)
	}
}

// A devil that finds no real defect abstains → no review round → the unanimous done stands.
func TestDeliberateDevilAbstainKeepsDone(t *testing.T) {
	var reviews int64
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if isDevilReview(r) {
			atomic.AddInt64(&reviews, 1)
		}
		if isDevil(r) {
			return `{"decision":"abstain","rationale":"tried to break it, deliverable is genuinely met"}`
		}
		return `{"decision":"done","rationale":"complete"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Devil: true,
	})
	if d.Decision != council.Done {
		t.Errorf("decision = %q, want done (devil abstained)", d.Decision)
	}
	if n := atomic.LoadInt64(&reviews); n != 0 {
		t.Errorf("no review round should run when the devil abstains, got %d", n)
	}
}

// The devil never runs when disabled, and never on a SPLIT (that is the rebuttal's territory):
// a 2-done/1-continue majority-done stays done with no devil poll.
func TestDeliberateDevilSkippedOffAndOnSplit(t *testing.T) {
	var devilCalls int64
	reply := func(r port.ChatRequest) string {
		if isDevil(r) {
			atomic.AddInt64(&devilCalls, 1)
			return `{"decision":"continue","rationale":"x","feedback":"y"}`
		}
		if memberIn(r, "Melchior") {
			return `{"decision":"continue","rationale":"incomplete","feedback":"more"}`
		}
		return `{"decision":"done","rationale":"ok"}`
	}
	// Devil OFF: even the unanimous-done path must not poll a devil.
	cOff := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if isDevil(r) {
			atomic.AddInt64(&devilCalls, 1)
		}
		return `{"decision":"done","rationale":"ok"}`
	}}), "m")
	if d, _ := cOff.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "x", Rule: council.RuleMajority, Devil: false}); d.Decision != council.Done {
		t.Fatalf("devil-off decision = %q, want done", d.Decision)
	}
	// Devil ON but a SPLIT (2 done / 1 continue → majority done): devil must NOT fire.
	cSplit := New(only(fakeLLM{reply: reply}), "m")
	d, _ := cSplit.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "x", Rule: council.RuleMajority, Devil: true})
	if d.Decision != council.Done {
		t.Errorf("split majority-done decision = %q, want done (devil skipped on split)", d.Decision)
	}
	if n := atomic.LoadInt64(&devilCalls); n != 0 {
		t.Errorf("devil must not be polled when off or on a split, got %d call(s)", n)
	}
}

// A member whose first reply cannot be parsed as JSON (a verbose model wrapping the
// object in prose) must be re-polled once with a JSON-only reminder before abstaining —
// otherwise its vote silently drops from quorum and skews the tally. On a valid retry the
// member's real verdict is adopted, not lost to abstention.
func TestPollRetriesUnparseableThenAdopts(t *testing.T) {
	var sawReminder atomic.Bool
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if strings.Contains(textOf(r), "ONLY the JSON") {
			sawReminder.Store(true)
			return `{"decision":"done","rationale":"complete on retry"}`
		}
		return "Sure, I'd say this looks finished, but let me explain at length with no JSON at all."
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round:   1,
		Task:    "do x",
		Members: []council.Member{{Name: "Solo", Lens: "correctness"}},
		Rule:    council.RuleUnanimous,
	})
	if !sawReminder.Load() {
		t.Fatal("first unparseable reply must trigger a retry carrying the JSON-only reminder")
	}
	var got council.Verdict
	for _, v := range d.Verdicts {
		if v.Member == "Solo" {
			got = v
		}
	}
	if got.Decision != council.Done {
		t.Fatalf("Solo verdict = %+v, want done adopted from the parseable retry", got)
	}
}

// If BOTH the initial poll and the JSON-only retry are unparseable, the member abstains
// (unchanged fallback) — the retry adds a second chance, never a third.
func TestPollBothUnparseableAbstains(t *testing.T) {
	var calls atomic.Int32
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		calls.Add(1)
		return "no json here at all, just musing about the task"
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round:   1,
		Task:    "do x",
		Members: []council.Member{{Name: "Solo", Lens: "correctness"}},
		Rule:    council.RuleUnanimous,
	})
	for _, v := range d.Verdicts {
		if v.Member == "Solo" && (v.Decision != council.Abstain || v.Rationale != "unparseable council reply") {
			t.Fatalf("Solo verdict = %+v, want abstain/unparseable after both attempts fail", v)
		}
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected exactly 2 poll attempts (initial + one retry), got %d", n)
	}
}

// A contract re-round must be told that the draft it is looking at is one the council itself just
// rewrote. Judged cold, a member cannot tell a criterion the council deliberately dropped from one
// the author forgot, so it re-raises the settled item — observed live: three members agreed to drop
// a criterion in round 1, one re-added it in round 2, and round 3 spent itself removing it again.
func TestContractEvidenceCarriesRevision(t *testing.T) {
	rev := "The council's own feedback that produced this revision:\nRemove the performance criterion — out of scope\n" +
		"The contract as it stood BEFORE this revision:\n- builds clean\n- performance preserved"
	e := evidence(port.DeliberationRequest{Phase: "contract", Task: "fix the crash", Plan: "- builds clean", Revision: rev})
	if !strings.Contains(e, "REVISED") {
		t.Errorf("contract evidence must label the revision section:\n%s", e)
	}
	if !strings.Contains(e, "Do NOT re-introduce") {
		t.Errorf("the section must forbid re-raising a removed criterion:\n%s", e)
	}
	if !strings.Contains(e, "out of scope") {
		t.Errorf("the council's own prior feedback must reach the members:\n%s", e)
	}
	// A first round has no history and must not be told about a revision that never happened.
	if first := evidence(port.DeliberationRequest{Phase: "contract", Task: "fix the crash", Plan: "- builds clean"}); strings.Contains(first, "REVISED") {
		t.Errorf("a first-round contract must carry no revision section:\n%s", first)
	}
	for _, banned := range []string{"grpcio", "kv-store", "cobol", "1.73"} {
		if strings.Contains(e, banned) {
			t.Errorf("contract revision section leaks eval-set token %q", banned)
		}
	}
}

// A substitution RE-round must be shown the objection the previous round raised. The agent is
// handed that critique to act on, but the council was re-convened knowing nothing about it, so a
// member could not check whether its own concern had been met and was free to raise a different
// one each round — the same amnesia the contract and plan re-rounds had.
func TestSubstitutionEvidenceCarriesPriorObjection(t *testing.T) {
	e := evidence(port.DeliberationRequest{
		Phase: "substitution", Task: "the port check could not run",
		Plan:     "curl the port instead",
		Revision: "The previous round REJECTED the substitution with this objection:\nreaching the port is not the same as serving a request",
	})
	if !strings.Contains(e, "PREVIOUS round objected") {
		t.Errorf("substitution evidence must label the prior objection:\n%s", e)
	}
	if !strings.Contains(e, "not the same as serving a request") {
		t.Errorf("the prior objection's text must reach the members:\n%s", e)
	}
	if !strings.Contains(e, "already answered") {
		t.Errorf("members must be told not to swap in a fresh objection:\n%s", e)
	}
	// First round: nothing to remember, so no section.
	if first := evidence(port.DeliberationRequest{Phase: "substitution", Task: "t", Plan: "p"}); strings.Contains(first, "PREVIOUS round") {
		t.Errorf("a first substitution round must carry no prior-objection section:\n%s", first)
	}
}

// An unparsed verdict is NOT a neutral outcome: the member is recorded as abstaining, which the
// tally cannot tell apart from "my lens has nothing to add". So the reply must survive what model
// output normally carries — a stray brace in the reasoning ahead of the real object, and a raw
// newline inside the multi-line rationale/feedback prose.
func TestParseReplySurvivesModelJSONDefects(t *testing.T) {
	cases := []struct{ name, text, wantDecision string }{
		{"clean", `{"decision":"done","rationale":"looks right"}`, "done"},
		{"stray brace before the verdict",
			"I considered `{a, b}` first.\n{\"decision\":\"continue\",\"rationale\":\"missing a step\"}", "continue"},
		{"raw newline inside rationale",
			"{\"decision\":\"continue\",\"rationale\":\"first point\nsecond point\",\"severity\":\"warn\"}", "continue"},
		{"trailing comma", `{"decision":"abstain","rationale":"nothing to add",}`, "abstain"},
		{"fenced", "```json\n{\"decision\":\"done\",\"rationale\":\"ok\"}\n```", "done"},
	}
	for _, c := range cases {
		r, ok := parseReply(c.text)
		if !ok || string(r.Decision) != c.wantDecision {
			t.Errorf("%s: parseReply → ok=%v decision=%q, want %q", c.name, ok, r.Decision, c.wantDecision)
		}
	}
	// Genuinely unusable replies still fail — leniency must not manufacture a vote.
	for _, bad := range []string{"", "I think it looks fine.", `{"rationale":"no decision field"}`} {
		if _, ok := parseReply(bad); ok {
			t.Errorf("parseReply(%q) must not produce a vote", bad)
		}
	}
}

// A member's verdict is one document: any field typed strictly cost the whole VOTE, recorded as an
// abstain the tally cannot tell from "no opinion". These lock the shapes a model actually emits.
func TestParseReplyTolerantShapes(t *testing.T) {
	t.Run("checks as a single object", func(t *testing.T) {
		r, ok := parseReply(`{"decision":"continue","rationale":"needs a check","checks":{"step":1,"deliverable":"d","command":"make"}}`)
		if !ok {
			t.Fatal("verdict lost over a single check object")
		}
		if len(r.Checks) != 1 || r.Checks[0].Command != "make" {
			t.Fatalf("checks = %+v", r.Checks)
		}
		if decisionOf(string(r.Decision)) != council.Continue {
			t.Fatalf("decision = %q", r.Decision)
		}
	})
	t.Run("decision wrapped in a list", func(t *testing.T) {
		r, ok := parseReply(`{"decision":["done"],"confidence":"0.9","rationale":"it builds"}`)
		if !ok || decisionOf(string(r.Decision)) != council.Done {
			t.Fatalf("ok=%v decision=%q", ok, r.Decision)
		}
		if float64(r.Confidence) != 0.9 {
			t.Fatalf("confidence = %v", r.Confidence)
		}
	})
	t.Run("severity as a list still tiers the vote", func(t *testing.T) {
		r, ok := parseReply(`{"decision":"continue","severity":["critical"],"feedback":"add the build step"}`)
		if !ok || string(r.Severity) != "critical" {
			t.Fatalf("ok=%v severity=%q", ok, r.Severity)
		}
	})
	t.Run("an unreadable checks field costs the checks, not the vote", func(t *testing.T) {
		r, ok := parseReply(`{"decision":"done","rationale":"fine","checks":"none"}`)
		if !ok || decisionOf(string(r.Decision)) != council.Done {
			t.Fatalf("vote lost: ok=%v r=%+v", ok, r)
		}
		if len(r.Checks) != 0 {
			t.Fatalf("checks = %+v, want none", r.Checks)
		}
	})
}

// A member states its decision in the FIRST field and then malforms a later array. Losing the whole
// verdict over that turns a stated vote into an abstain the tally cannot tell from "no opinion" —
// and when the vote was a critical continue, it silently disables the veto it was cast to trigger.
func TestMalformedVerdictKeepsTheDecisionItStated(t *testing.T) {
	const bad = `{"decision":"continue","confidence":0.9,"rationale":"verification is missing",` +
		`"severity":"critical","criteria":["the change is exercised, not just compiled","checks":[]]}`
	r, ok := parseReply(bad)
	if !ok {
		t.Fatalf("the verdict was discarded: %s", bad)
	}
	if decisionOf(string(r.Decision)) != council.Continue {
		t.Errorf("decision = %q, want continue", r.Decision)
	}
	if string(r.Severity) != "critical" {
		t.Errorf("severity = %q, want critical (it gates blocking vs advisory)", r.Severity)
	}
	if len(r.Criteria) != 1 {
		t.Errorf("criteria = %q, want the one that arrived whole", r.Criteria)
	}
}

// The salvage is bounded by what it can honestly claim: with the decision itself behind the defect
// there is no vote to keep, and inventing one would be worse than the abstain.
func TestMalformedVerdictWithNoReadableDecisionStillAbstains(t *testing.T) {
	if _, ok := parseReply(`{"rationale":"the plan is fine","criteria":["a","checks":[]]}`); ok {
		t.Error("a reply with no decision must not be read as a verdict")
	}
	if _, ok := parseReply("I think it is probably fine, hard to say."); ok {
		t.Error("prose must not be read as a verdict")
	}
}

// A salvaged verdict must survive the whole Deliberate path, not just parseReply.
func TestDeliberateReadsAMalformedVerdict(t *testing.T) {
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		return `{"decision":"done","confidence":0.9,"rationale":"all deliverables exist and ran",` +
			`"criteria":["the output matches the requested format","checks":[]]}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done (the members voted done in readable prefixes)", d.Decision)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Done {
			t.Errorf("member %s = %q, want done", v.Member, v.Decision)
		}
	}
}

// The retry reminder has to name the defect that actually occurred. Telling a model that emitted a
// bare-but-malformed object to "strip the prose" is advice about a different failure, and the
// observed result is the identical malformation on the retry and the vote lost anyway.
func TestRetryReminderNamesTheDefectThatOccurred(t *testing.T) {
	syntax := councilRetryReminder(`{"decision":"continue","criteria":["a","checks":[]]}`)
	if strings.Contains(syntax, "no prose") || !strings.Contains(syntax, "malformed") {
		t.Errorf("a syntax defect was reported as prose wrapping:\n%s", syntax)
	}
	if !strings.Contains(syntax, "syntax error at offset") || !strings.Contains(syntax, "⟪HERE⟫") {
		t.Errorf("the reminder withholds the location magi already computed:\n%s", syntax)
	}

	schema := councilRetryReminder(`{"verdict":"done","why":"looks fine"}`)
	if !strings.Contains(schema, "`decision` ") || !strings.Contains(schema, "well-formed") {
		t.Errorf("a well-formed reply missing the decision was not named as such:\n%s", schema)
	}

	prose := councilRetryReminder("I think it is probably fine, hard to say.")
	if !strings.Contains(prose, "ONLY the JSON object") {
		t.Errorf("prose must still get the JSON-only reminder:\n%s", prose)
	}
}

// Same floor on the prompt that AUTHORS the checks in the first place: an exit-0 configure/build with
// the right flag on its command line proves the flag was accepted, not that it took effect, and an
// effect that landed somewhere the task does not look is not the deliverable either.
func TestPlanMemberPromptRejectsACommandThatMerelySucceeded(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "build something with a compiler flag", false, false)
	for _, want := range []string{"COMMAND THAT SUCCEEDED", "ACCEPTED, not", "took EFFECT", "AT THE LOCATION the task names"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must reject a command-exit-code proxy for an effect (missing %q)", want)
		}
	}
}

// RECORD AND READ is the DEFAULT shape a check must take, not a fallback for expensive runs: the
// STEP performs whatever has to be run and saves its real output, and the check READS that file.
// It is what makes a check un-refusable by the read-only shell, non-repeating across gate cycles,
// and honest in its exit status — so the authoring prompt must state it as the default and must
// still forbid hand-writing the file it reads.
func TestPlanMemberPromptMakesRecordAndReadTheDefault(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "build the project and run its test suite", false, false)
	for _, want := range []string{
		"RECORD AND READ IS THE ONLY SHAPE",
		"that run belongs to the STEP",
		"REDIRECTS its real output to a fixed path",
		"never hand-written",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("check-authoring prompt must make record-and-read the default (missing %q)", want)
		}
	}
	// The behavioural floor must survive the shift: moving the RUN onto the step may not weaken
	// WHAT is asserted down to an existence probe.
	if !strings.Contains(p, "EXERCISE, DO NOT MERELY REACH") || !strings.Contains(p, "is a PRECONDITION, not proof") {
		t.Errorf("moving the run onto the step must not license a weaker assertion:\n%s", p)
	}
}

// magi is never told that a scorer exists — nothing in a live run supplies one, and a benchmark's
// grader is information the harness deliberately withholds. A prompt that asserts one anyway teaches
// the council to reason about what an imagined judge wants instead of what the TASK states, which is
// exactly the over-fit that produced spurious demands. Every council prompt must therefore be free of
// harness framing; the force those clauses carried is kept in task-relative wording ("acceptance",
// "the task requires").
func TestCouncilPromptsCarryNoHarnessFraming(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	prompts := map[string]string{
		"devil":    devilSystem,
		"plan":     memberSystem(m, "plan", "build a service and run its tests", false, false),
		"contract": memberSystem(m, "contract", "build a service and run its tests", false, false),
		"subst":    memberSystem(m, "substitution", "build a service and run its tests", false, false),
		"finish":   memberSystem(m, "", "build a service and run its tests", true, true),
	}
	for name, p := range prompts {
		for _, banned := range []string{"grader", "benchmark", "leaderboard", "reward", "eval set"} {
			if strings.Contains(strings.ToLower(p), banned) {
				t.Errorf("%s prompt asserts a harness artifact %q — keep acceptance task-relative", name, banned)
			}
		}
	}
}
