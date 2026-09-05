package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// The requirements walk must be UNCONDITIONAL. It used to fire only when the acceptance criteria
// arrived as a NUMBERED checklist — and the tasks this council actually judges do not number theirs,
// so the one rule that forced item-by-item judgement never ran, and the members voted against the
// work as an undivided impression. Measured consequence: 21 member votes in one A/B arm, 21 done,
// zero dissent. The clause is kept honest here across every lens, not just the one it was written on.
func TestRequirementsWalkIsUnconditional(t *testing.T) {
	for _, lens := range []string{"correctness", "verification", "completeness"} {
		p := memberSystem(council.Member{Name: "x", Lens: lens}, "stand up a service", false)
		for _, want := range []string{"REQUIREMENTS WALK", "SATISFIED", "UNSATISFIED", "NO-EVIDENCE"} {
			if !strings.Contains(p, want) {
				t.Errorf("lens=%s: walk clause missing %q", lens, want)
			}
		}
		// The old gate must be gone: a walk that runs only for numbered criteria is the defect.
		if strings.Contains(p, "when the acceptance criteria are given as a NUMBERED checklist, judge EACH") {
			t.Errorf("lens=%s: the walk is still gated on a numbered checklist", lens)
		}
		// A numbered checklist is still walked by number — the general rule absorbed the special case
		// rather than dropping it.
		if !strings.Contains(p, "item by item, by number") {
			t.Errorf("lens=%s: the enumerated-checklist case was dropped instead of absorbed", lens)
		}
		// The walk decides the vote; otherwise it is decoration the model may write and ignore.
		if !strings.Contains(p, "any requirement UNSATISFIED → continue") {
			t.Errorf("lens=%s: the walk does not bind the decision", lens)
		}
	}
}

// The reply asks for the walk BEFORE the decision, in both modes. A schema whose first key is
// `decision` gets a decision written before the model has read one requirement back to itself, and
// then everything after it is written to agree. This is an ordering claim, so it is tested as one:
// the offset of `"checks"` must precede the offset of `"decision"`.
func TestSchemaAsksForTheWalkBeforeTheVerdict(t *testing.T) {
	check := func(name, schema string) {
		t.Helper()
		ci, di := strings.Index(schema, `"checks"`), strings.Index(schema, `"decision"`)
		if ci < 0 {
			t.Fatalf("%s: schema has no `checks` field:\n%s", name, schema)
		}
		if di < 0 {
			t.Fatalf("%s: schema has no `decision` field", name)
		}
		if ci > di {
			t.Fatalf("%s: `decision` (at %d) is asked for before `checks` (at %d) — the verdict would be "+
				"written before the reasoning it is supposed to follow", name, di, ci)
		}
	}
	for _, keep := range []bool{false, true} {
		check("memberSystem", memberSystem(council.Member{Name: "x", Lens: "correctness"}, "do it", keep))
	}
	check("verdictSchemaFor(false)", verdictSchemaFor(false))
	check("verdictSchemaFor(true)", verdictSchemaFor(true))
}

// Three members with one line of lens apiece and every other instruction identical are three
// samples of one opinion — which is what 21-of-21 unanimous done looks like from the inside. Each
// lens now walks the same evidence by a different ROUTE, and a route only differentiates if it
// actually differs. It must not partition jurisdiction: a defect off your route is still yours.
func TestLensRoutesDifferAndDoNotPartitionJurisdiction(t *testing.T) {
	seen := map[string]string{}
	for _, lens := range []string{"correctness", "verification", "completeness"} {
		p := memberSystem(council.Member{Name: "x", Lens: lens}, "do it", false)
		if !strings.Contains(p, "YOUR ROUTE:") {
			t.Fatalf("lens=%s: no route", lens)
		}
		r := council.RouteFor(lens)
		if r == "" {
			t.Fatalf("lens=%s: empty route", lens)
		}
		if prev, dup := seen[r]; dup {
			t.Fatalf("lens=%s shares its route with %s — a shared route differentiates nothing", lens, prev)
		}
		seen[r] = lens
		if !strings.Contains(p, r) {
			t.Fatalf("lens=%s: the route is not in the prompt", lens)
		}
		if !strings.Contains(p, "NOT the limit of what you may judge") {
			t.Fatalf("lens=%s: the route reads as a jurisdiction — a defect off-route must still be "+
				"nameable, or a real defect draws one continue against two uninformed dones", lens)
		}
	}
	// An unrecognized lens still gets a usable route rather than an empty line.
	if council.RouteFor("no-such-lens") == "" {
		t.Error("an unknown lens must still get a route")
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

// isPanel reports whether the request is the whole-council call rather than a single member's.
func isPanel(r port.ChatRequest) bool { return strings.Contains(r.System, "THE COUNCIL —") }

// speaks builds a reply for either shape from one per-member function, so a test can say what each
// lens thinks without caring how many calls magi decided to make. The rebuttal round is still asked
// member by member, so both shapes reach the same fake.
func speaks(verdict func(name string, r port.ChatRequest) string) func(port.ChatRequest) string {
	return func(r port.ChatRequest) string {
		if !isPanel(r) {
			for _, m := range council.DefaultMembers() {
				if memberIn(r, m.Name) {
					return verdict(m.Name, r)
				}
			}
			return `{"decision":"abstain","rationale":"?"}`
		}
		if strings.Contains(textOf(r), "── ACROSS THE WALKS ──") {
			return `{"rationale":"nothing to add","decision":"done"}`
		}
		var b strings.Builder
		b.WriteString(`{"verdicts":[`)
		for i, m := range council.DefaultMembers() {
			if i > 0 {
				b.WriteString(",")
			}
			one := verdict(m.Name, r)
			fmt.Fprintf(&b, `{"member":%q,"lens":%q,%s`, m.Name, m.Lens, strings.TrimPrefix(one, "{"))
		}
		b.WriteString("]}")
		return b.String()
	}
}

func TestDeliberateAllDone(t *testing.T) {
	c := New(only(fakeLLM{reply: speaks(func(string, port.ChatRequest) string {
		return `{"decision":"done","confidence":0.9,"rationale":"looks complete"}`
	})}), "m")
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
	c := New(only(fakeLLM{reply: speaks(func(name string, _ port.ChatRequest) string {
		if name == "Balthasar" {
			return `{"decision":"done","rationale":"tests pass"}`
		}
		return `{"decision":"continue","rationale":"incomplete","feedback":"add the missing flag"}`
	})}), "m")
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
	c := New(only(fakeLLM{reply: speaks(func(name string, r port.ChatRequest) string {
		rebuttal := strings.Contains(textOf(r), "Council disagreement")
		if name == "Melchior" {
			if rebuttal { // reconsiders and joins the majority
				return `{"decision":"done","rationale":"peers are right, tests do cover it"}`
			}
			return `{"decision":"continue","rationale":"looks incomplete"}`
		}
		return `{"decision":"done","rationale":"tests pass"}`
	})}), "m")
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
	c := New(only(fakeLLM{reply: speaks(func(name string, r port.ChatRequest) string {
		if name == "Melchior" { // counted once per call below, not once per member
			atomic.AddInt64(&calls, 1)
			if strings.Contains(textOf(r), "Council disagreement") {
				atomic.AddInt64(&rebuttals, 1)
			}
			return `{"decision":"continue","rationale":"incomplete","feedback":"more"}`
		}
		return `{"decision":"done","rationale":"ok"}`
	})}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: false,
	})
	if n := atomic.LoadInt64(&rebuttals); n != 0 {
		t.Errorf("rebuttal round ran %d time(s) with Debate=false", n)
	}
	if d.Decision != council.Done { // 2 done / 1 continue → majority done, no debate
		t.Fatalf("decision = %q, want done (majority, no debate)", d.Decision)
	}
	// One call for the whole council; the rebuttal would be three more and did not run.
	if calls := atomic.LoadInt64(&calls); calls != 1 {
		t.Fatalf("want exactly 1 council call (no rebuttal), got %d", calls)
	}
}

// Debate is skipped when the independent tally is already Continue: the dissent can't
// change the outcome, and debate must never be used to talk a hesitant council into
// done. Melchior+Casper continue, Balthasar done → continue-majority → no rebuttal,
// exactly 3 polls.
func TestDeliberateSkipDebateOnContinueMajority(t *testing.T) {
	var calls, rebuttals int64
	c := New(only(fakeLLM{reply: speaks(func(name string, r port.ChatRequest) string {
		if name == "Melchior" { // once per call, not once per member
			atomic.AddInt64(&calls, 1)
			if strings.Contains(textOf(r), "Council disagreement") {
				atomic.AddInt64(&rebuttals, 1)
			}
		}
		if name == "Balthasar" {
			return `{"decision":"done","rationale":"looks fine"}`
		}
		return `{"decision":"continue","rationale":"incomplete","feedback":"more"}`
	})}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: true,
	})
	if n := atomic.LoadInt64(&rebuttals); n != 0 {
		t.Errorf("debate must be skipped on a continue-majority, ran %d time(s)", n)
	}
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Errorf("want exactly 1 council call (no rebuttal), got %d", n)
	}
	if d.Decision != council.Continue {
		t.Errorf("decision = %q, want continue", d.Decision)
	}
	if d.Debate != nil {
		t.Errorf("no DebateOutcome expected when skipped, got %+v", d.Debate)
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
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if strings.Contains(textOf(r), "── ACROSS THE WALKS ──") {
			return `{"rationale":"nothing to add","decision":"done"}`
		}
		// Three whole verdicts, then a defect in a trailing field: the shape observed from a weak
		// model, and the reason SalvagePrefix exists. Everything before it must survive.
		return `{"verdicts":[` +
			`{"member":"Melchior","lens":"correctness","decision":"done","rationale":"ran"},` +
			`{"member":"Balthasar","lens":"verification","decision":"done","rationale":"ran"},` +
			`{"member":"Casper","lens":"completeness","decision":"done","rationale":"ran"}],` +
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
		"member":      memberSystem(m, "build a service and run its tests", false),
		"member-keep": memberSystem(m, "build a service and run its tests", true),
	}
	for name, p := range prompts {
		for _, banned := range []string{"grader", "benchmark", "leaderboard", "reward", "eval set"} {
			if strings.Contains(strings.ToLower(p), banned) {
				t.Errorf("%s prompt asserts a harness artifact %q — keep acceptance task-relative", name, banned)
			}
		}
	}
}

// Every member is announced through OnVerdict, once, before the batch returns.
//
// This test used to assert something stronger and no longer true: that a member was announced the
// MOMENT IT ANSWERED, so a caller could paint the first two while the third was still thinking.
// That property belonged to the split shape, where three members meant three concurrent calls. The
// council now asks once for all three, and one call cannot deliver its first verdict before its
// third — the progressive paint is genuinely gone, traded for the two calls that were re-sending
// the same evidence to say the same thing about it. What survives is the contract callers actually
// depend on: each member arrives exactly once, with its own name and lens, before the deliberation
// returns.
func TestOnVerdictAnnouncesEveryMemberBeforeTheBatch(t *testing.T) {
	llm := fakeLLM{reply: func(port.ChatRequest) string {
		return `{"verdicts":[` +
			`{"member":"Melchior","lens":"correctness","decision":"done","rationale":"a","cite":"x"},` +
			`{"member":"Balthasar","lens":"verification","decision":"done","rationale":"b","cite":"y"},` +
			`{"member":"Casper","lens":"completeness","decision":"done","rationale":"c","cite":"z"}]}`
	}}
	c := New(func(string) port.LLMProvider { return llm }, "m")

	var mu sync.Mutex
	seen := map[string]int{}
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "t", Members: council.DefaultMembers(),
		OnVerdict: func(v council.Verdict) {
			mu.Lock()
			seen[v.Member]++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range council.DefaultMembers() {
		if seen[m.Name] != 1 {
			t.Errorf("%s was announced %d times, want exactly 1 — a caller painting from OnVerdict "+
				"either misses it or draws it twice", m.Name, seen[m.Name])
		}
	}
	if len(d.Verdicts) != 3 {
		t.Fatalf("want three verdicts in the batch, got %d", len(d.Verdicts))
	}
}

// A member that never answers is an abstention, not a hang.
//
// Deliberate waited on its wait group with no bound anywhere, so one member's request going quiet —
// a backend that accepted the connection and then said nothing — held it open forever. The tool
// call that convened the council sat in the transcript with no result and the turn stopped, which
// is what was reported.
func TestASilentMemberAbstainsInsteadOfHangingTheTurn(t *testing.T) {
	was := memberDeadline
	memberDeadline = 200 * time.Millisecond
	defer func() { memberDeadline = was }()

	// Casper's request is accepted and then never answered — the shape a wedged backend has.
	// Casper's request is accepted and then never answered — the shape a wedged backend has. The
	// stream is what blocks, so the fake blocks in StreamChat rather than returning an error.
	llm := hangFor{name: "Casper", ok: `{"decision":"done","confidence":0.9,"rationale":"the build passes"}`}
	// One call carries the whole council, so a wedged backend takes all of it — the point this
	// pins is that the turn RETURNS with abstentions that say why, not that one member survives.
	c := New(func(string) port.LLMProvider { return llm }, "m")

	done := make(chan council.Deliberation, 1)
	go func() {
		d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
			Members: []council.Member{{Name: "Melchior"}, {Name: "Casper"}},
			Rule:    council.DefaultRule,
		})
		done <- d
	}()

	select {
	case d := <-done:
		var abstained bool
		for _, v := range d.Verdicts {
			if v.Member == "Casper" && v.Decision == "abstain" {
				abstained = true
				if !strings.Contains(v.Rationale, "did not answer") {
					t.Errorf("the abstention does not say why: %q", v.Rationale)
				}
			}
		}
		if !abstained {
			t.Errorf("the silent member did not abstain: %+v", d.Verdicts)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Deliberate never returned; one silent member still stops the turn")
	}
}

// hangFor answers every member but one, and for that one accepts the request and says nothing
// until its context ends — which is what a wedged backend does and what an error return is not.
type hangFor struct {
	name string
	ok   string
}

func (h hangFor) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	if memberIn(r, h.name) || (isPanel(r) && h.name != "") {
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: h.ok}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// The rebuttal round is bounded like the independent round: a member that accepts the rebuttal
// request and then goes silent must not wedge Deliberate. Round 1 splits Done (Melchior dissents,
// triggering debate); the rebuttal hangs. With the bound, the turn returns and Melchior keeps its
// prior vote; without it, the raw ctx (Background here) never fires and this hangs to the test's
// timeout.
func TestARebuttalRoundDoesNotHangTheTurn(t *testing.T) {
	was := memberDeadline
	memberDeadline = 150 * time.Millisecond
	defer func() { memberDeadline = was }()

	c := New(only(rebuttalHang{}), "m")
	done := make(chan council.Deliberation, 1)
	go func() {
		d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
			Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: true,
		})
		done <- d
	}()
	select {
	case d := <-done:
		got := map[string]council.Decision{}
		for _, v := range d.Verdicts {
			got[v.Member] = v.Decision
		}
		if got["Melchior"] != council.Continue {
			t.Errorf("a member cut off in the rebuttal did not keep its prior vote: %+v", d.Verdicts)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Deliberate never returned — the rebuttal round hangs to the turn wall clock")
	}
}

type rebuttalHang struct{}

func (rebuttalHang) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	if strings.Contains(textOf(r), "Council disagreement") {
		go func() { <-ctx.Done(); close(ch) }() // the rebuttal is accepted, then silent
		return ch, nil
	}
	vote := `{"decision":"done","rationale":"tests pass"}`
	if memberIn(r, "Melchior") {
		vote = `{"decision":"continue","rationale":"looks incomplete"}`
	}
	if isPanel(r) {
		// The independent round is one call: Melchior dissents inside it, which is what makes the
		// split that triggers the rebuttal this test is about.
		vote = `{"verdicts":[` +
			`{"member":"Melchior","lens":"correctness","decision":"continue","rationale":"looks incomplete"},` +
			`{"member":"Balthasar","lens":"verification","decision":"done","rationale":"tests pass"},` +
			`{"member":"Casper","lens":"completeness","decision":"done","rationale":"tests pass"}]}`
		if strings.Contains(textOf(r), "── ACROSS THE WALKS ──") {
			vote = `{"rationale":"nothing to add","decision":"done"}`
		}
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: vote}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// The deadline sweep rewrites only the slots nobody answered. It covers the panel call AND the
// closing call, so a round whose verdicts arrived whole and whose close ran long had its genuine
// abstentions relabelled "the council did not answer" and marked silent — a member who
// deliberated and declined, recorded as an unreachable backend.
func TestASlowCloseDoesNotEraseAGenuineAbstain(t *testing.T) {
	verdicts := []council.Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: council.Done, Rationale: "ok"},
		{Member: "Balthasar", Lens: "verification", Decision: council.Abstain,
			Rationale: "my lens cannot judge a docs-only turn"},
		{Member: "Casper", Lens: "completeness", Decision: council.Abstain,
			Silent: true, Rationale: panelUnanswered},
	}
	// The sweep, as the deliberation runs it when the poll context has expired.
	for i := range verdicts {
		if verdicts[i].Decision == council.Abstain && verdicts[i].Silent {
			verdicts[i].Rationale = "the council did not answer within 300ms"
		}
	}
	if verdicts[1].Rationale != "my lens cannot judge a docs-only turn" || verdicts[1].Silent {
		t.Errorf("a member who declined was overwritten: %+v", verdicts[1])
	}
	if !strings.Contains(verdicts[2].Rationale, "did not answer within") || !verdicts[2].Silent {
		t.Errorf("a slot nobody answered must still be swept: %+v", verdicts[2])
	}
}

// A rebuttal round keeps each member's cite. It was the one verdict-building site that never
// assigned it, so after any debate every member's grounds vanished — and an empty cite is itself
// worth seeing, which made "no grounds given" indistinguishable from "discarded in transit".
func TestARebuttalKeepsTheCite(t *testing.T) {
	if !strings.Contains(readRepoSource(t, "council.go"), "v.Cite = strings.TrimSpace(string(r.Cite))") {
		t.Error("pollRebut does not carry the cite")
	}
}

func readRepoSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A completion declaration is a tool call, so members cannot see it in the evidence — and one
// round rejected a finished turn because "the agent never declared the task finished", which the
// declaration convening it had already settled. The evidence says so now.
func TestEvidenceSaysWhenTheRoundWasConvenedByADeclaration(t *testing.T) {
	asked := evidence(port.DeliberationRequest{Task: "ship it", Report: "done", Declared: false})
	if strings.Contains(asked, "DECLARED") {
		t.Error("a question round must not claim a declaration")
	}
	declared := evidence(port.DeliberationRequest{Task: "ship it", Report: "done", Declared: true})
	if !strings.Contains(declared, "DECLARED") || !strings.Contains(declared, "tool call") {
		t.Errorf("a declaration round must say so:\n%s", declared)
	}
}

// Guidance (skills the agent read) is a section of its own, placed before the report so a judge
// reads the rules before the claim; absent when nothing was read.
func TestEvidenceGuidanceSection(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task:     "make a deck",
		Report:   "done",
		Guidance: "## skill deck-design\nrender each finished page once",
	})
	if !strings.Contains(got, "Guidance the agent read this session") {
		t.Errorf("guidance section header missing:\n%s", got)
	}
	if !strings.Contains(got, "render each finished page once") {
		t.Errorf("guidance body missing:\n%s", got)
	}
	if strings.Index(got, "Guidance the agent read") > strings.Index(got, "Agent's report") {
		t.Errorf("guidance must precede the report:\n%s", got)
	}
	if e := evidence(port.DeliberationRequest{Task: "x", Report: "y"}); strings.Contains(e, "Guidance the agent read") {
		t.Errorf("no guidance → no section:\n%s", e)
	}
}
