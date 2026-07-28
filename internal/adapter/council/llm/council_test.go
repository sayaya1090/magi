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

// The keep clause + schema field appear ONLY when keep is requested (MAGI_COUNCIL_KEEP),
// so the baseline prompt is byte-for-byte unchanged when it is off.
func TestMemberPromptKeepGated(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	off := memberSystem(m, "fix the bug", false)
	if strings.Contains(off, "\"keep\"") || strings.Contains(off, "must NOT redo or revert") {
		t.Error("keep clause/schema must be absent when keep is off")
	}
	on := memberSystem(m, "fix the bug", true)
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

// The terminate member prompt must carry the per-item acceptance clause: when the criteria are an
// enumerated checklist, judge each item and land done only if EVERY item is satisfied.
func TestTerminateMemberPromptPerItem(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	p := memberSystem(m, "stand up a service", false)
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
	p := memberSystem(m, "stand up a service", false)
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
	p := memberSystem(m, "make the handler resilient", false)
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
	for _, lens := range []string{"correctness", "verification", "completeness"} {
		m := council.Member{Name: "x", Lens: lens}
		for _, keep := range []bool{false, true} {
			p := memberSystem(m, "build the thing", keep)
			for _, b := range banned {
				if strings.Contains(p, b) {
					t.Errorf("lens=%q keep=%v leaks eval-set-specific token %q — use a task-agnostic example",
						lens, keep, b)
				}
			}
		}
	}
}

// A report that rationalizes incompletion ("impossible, so this is full completion",
// "nothing needed fixing") must be treated as an admission, not a done — the clause
// that closes the reval3 play-zork / run-pdp11 / fasttext class of false approvals.
func TestMemberPromptRationalizedDone(t *testing.T) {
	m := council.Member{Name: "x", Lens: "verification"}
	s := memberSystem(m, "beat the game", false)
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
}

// A council-invented verification must state the OBJECTIVE and leave the method to the agent,
// never prescribe a specific inspection command that may be absent in the container. A passing
// end-to-end exercise satisfies the must-respond/run bar (kv-store-grpc run17: `ps: not found`
// made the council reject a live, working gRPC server across 3 rounds because it demanded a
// process listing instead of crediting the successful client round-trip).
func TestMemberPromptObjectiveNotMethod(t *testing.T) {
	m := council.Member{Name: "x", Lens: "correctness"}
	s := memberSystem(m, "run a server on port 5328", false)

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
	s := memberSystem(m, "run a server on port 5328", false)

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
}

func TestMemberPromptArtifactGrounding(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}
	s := memberSystem(m, "build a CLI tool", false)
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
}

// TestMemberPromptProportionality guards the analysis/survey calibration: neither
// phase may derive or enforce an exhaustive "list ALL N with EXACT lines" contract
// for a large-set analysis task (the '리팩토링 요소 찾아줘' loop, where plan-audit
// approved an impossible contract the completion council then enforced).
func TestMemberPromptProportionality(t *testing.T) {
	m := council.Member{Name: "x", Lens: "completeness"}

	// terminate phase: representative coverage of a large set is done; demanding
	// exhaustive enumeration / atom-level precision is churn, not a defect.
	s := memberSystem(m, "find refactoring candidates", false)
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
		"plan":     memberSystem(m, "build a service and run its tests", false),
		"contract": memberSystem(m, "build a service and run its tests", false),
		"subst":    memberSystem(m, "build a service and run its tests", false),
		"finish":   memberSystem(m, "build a service and run its tests", true),
	}
	for name, p := range prompts {
		for _, banned := range []string{"grader", "benchmark", "leaderboard", "reward", "eval set"} {
			if strings.Contains(strings.ToLower(p), banned) {
				t.Errorf("%s prompt asserts a harness artifact %q — keep acceptance task-relative", name, banned)
			}
		}
	}
}
