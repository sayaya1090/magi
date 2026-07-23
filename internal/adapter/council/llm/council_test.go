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
	off := memberSystem(m, "terminate", "fix the bug", false)
	if strings.Contains(off, "\"keep\"") || strings.Contains(off, "must NOT redo or revert") {
		t.Error("keep clause/schema must be absent when keep is off")
	}
	on := memberSystem(m, "terminate", "fix the bug", true)
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
func TestPlanMemberPromptKeepGated(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	off := memberSystem(m, "plan", "build a server", false)
	if strings.Contains(off, "\"keep\"") || strings.Contains(off, "EVEN WHEN YOU APPROVE") {
		t.Error("plan keep clause/schema must be absent when keep is off")
	}
	on := memberSystem(m, "plan", "build a server", true)
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

// The plan-audit member authors executable checks, so its prompt must state that a check is SEPARATE
// from the work: idempotent, no state change, never re-performing the step's own producing action. A
// mutating "check" (tar -czf, scp, rm) re-does the step every gate cycle and traps the run in a redo
// loop — this guards the authoring guidance against a silent regression.
func TestPlanMemberPromptSeparatesWorkFromCheck(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "download and analyze the results", false)
	for _, want := range []string{"WORK AND CHECK ARE SEPARATE", "IDEMPOTENT", "tar -czf"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must forbid mutating checks (missing %q)", want)
		}
	}
}

// The check-authoring prompt must require an INTEGER step label (so the numeric gate matches
// instead of falling back to a flattened union) and state that the per-step checks be jointly
// satisfiable and checklist-driven — the guard against the plexus #224 contradictory checklist.
func TestPlanMemberPromptScopesChecksToSteps(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "compress, extract, analyze, then clean up", false)
	for _, want := range []string{"INTEGER STEP NUMBER", "JOINTLY", "CHECKLIST-DRIVEN"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must scope checks to steps (missing %q)", want)
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
	p := memberSystem(m, "terminate", "stand up a service", false)
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
	p := memberSystem(m, "plan", "install a dependency and run a server", false)
	for _, want := range []string{"do NOT demand MORE than the task states", "over-specification", "minimal condition"} {
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
	p := memberSystem(m, "plan", "install a dependency and run a server", false)
	for _, want := range []string{"PRECONDITION, not proof", "non-functional stub", "weakest input", "real code path"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan check-authoring prompt must reject proxy-only (too-weak) checks (missing %q)", want)
		}
	}
}

// Guard against benchmark overfitting: the check-authoring prompt's examples must be task-agnostic —
// no eval-set task's exact command, filename, or value may be baked into a prompt the model sees.
func TestPlanMemberPromptNoEvalSetSpecifics(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	p := memberSystem(m, "plan", "build the thing", false)
	for _, banned := range []string{"pmars", "flashpaper", "rave.red", "extract-elf", "extract.js", "a.out", "grpcio", "kv-store"} {
		if strings.Contains(p, banned) {
			t.Errorf("check-authoring prompt leaks eval-set-specific token %q — use a task-agnostic example", banned)
		}
	}
}

// that closes the reval3 play-zork / run-pdp11 / fasttext class of false approvals.
func TestMemberPromptRationalizedDone(t *testing.T) {
	m := council.Member{Name: "x", Lens: "verification"}
	s := memberSystem(m, "terminate", "beat the game", false)
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
	if p := memberSystem(m, "plan", "beat the game", false); strings.Contains(p, "RATIONALIZES incompletion") {
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
	s := memberSystem(m, "terminate", "run a server on port 5328", false)

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
	s := memberSystem(m, "terminate", "run a server on port 5328", false)

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
	if p := memberSystem(m, "plan", "run a server", false); strings.Contains(p, "do NOT reissue it") {
		t.Error("contest-adjudication clause leaked into the plan-audit prompt")
	}
}

func TestMemberPromptArtifactGrounding(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	s := memberSystem(m, "terminate", "build a CLI tool", false)
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
	p := memberSystem(m, "plan", "build a CLI tool", false)
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
	s := memberSystem(m, "terminate", "find refactoring candidates", false)
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
	p := memberSystem(m, "plan", "find refactoring candidates", false)
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
	p := memberSystem(m, "plan", "build a small interpreted language", false)

	// Abstractness alone is never a critical revision.
	if !strings.Contains(p, "NEVER critical-revise a refine step for abstractness") {
		t.Error("plan prompt missing the refine 'abstractness is not a flaw' carve-out")
	}
	// …but the carve-out is not a pass for a bad plan: an unsound abstract plan stays critical.
	if !strings.Contains(p, "STILL critical") || !strings.Contains(p, "Reject the absurd, approve the merely abstract") {
		t.Error("plan prompt missing the 'absurd abstract plan is still critical' balance")
	}
	// The refine guidance is plan-audit only — it must not leak into the terminate prompt.
	if s := memberSystem(m, "terminate", "build a small interpreted language", false); strings.Contains(s, "critical-revise a refine step") {
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
	if r, ok := parseReply(`{"decision":"DONE"}`); !ok || decisionOf(r.Decision) != council.Done {
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
