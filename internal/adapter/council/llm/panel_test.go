package llm

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// The panel exists to stop paying for the same evidence three times, and everything downstream of
// it must be unable to notice. These pin both halves: ONE request goes out, and THREE ordinary
// verdicts come back with their members, lenses and fields intact.

type panelProvider struct {
	mu    sync.Mutex
	reqs  []port.ChatRequest
	reply func(int) string
}

func (p *panelProvider) StreamChat(_ context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	p.mu.Lock()
	n := len(p.reqs)
	p.reqs = append(p.reqs, r)
	p.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: p.reply(n)}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func replyWith(vs ...[3]string) string {
	type v struct {
		Member    string   `json:"member"`
		Lens      string   `json:"lens"`
		Checks    []string `json:"checks"`
		Decision  string   `json:"decision"`
		Rationale string   `json:"rationale"`
		Cite      string   `json:"cite"`
	}
	out := struct {
		Verdicts []v `json:"verdicts"`
	}{}
	for _, x := range vs {
		out.Verdicts = append(out.Verdicts, v{Member: x[0], Lens: x[1], Decision: x[2],
			Checks:    []string{"the file exists - SATISFIED - \"wrote 12 bytes\""},
			Rationale: "because", Cite: "wrote 12 bytes"})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func TestPanelAsksOnceAndReturnsEveryLens(t *testing.T) {
	p := &panelProvider{reply: func(int) string {
		return replyWith([3]string{"Melchior", "correctness", "done"},
			[3]string{"Balthasar", "verification", "continue"},
			[3]string{"Casper", "completeness", "done"})
	}}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "wrote hello.txt", Members: council.DefaultMembers()})
	if err != nil {
		t.Fatal(err)
	}
	// The saving IS the call count: three members used to mean three requests, each opening its own
	// backend session and re-sending the same evidence to read nothing from cache. It is now one,
	// plus a closing call that EXTENDS it — same system, same evidence, the verdicts, then one more
	// question — so a backend that resumes reads all of that from cache instead of re-sending it.
	if len(p.reqs) != 2 {
		t.Fatalf("want the panel call and the closing call, got %d", len(p.reqs))
	}
	first, second := p.reqs[0], p.reqs[1]
	if second.System != first.System {
		t.Fatal("the closing call changed the system prompt — it is then a new prefix, not an extension")
	}
	if len(second.Messages) <= len(first.Messages) {
		t.Fatal("the closing call must EXTEND the first exchange, not replace it")
	}
	for i := range first.Messages {
		if second.Messages[i].Parts[0].Text != first.Messages[i].Parts[0].Text {
			t.Fatalf("the closing call diverges at message %d — everything before the new question "+
				"must be byte-identical or none of it comes from cache", i)
		}
	}
	if len(d.Verdicts) != 3 {
		t.Fatalf("want three verdicts, got %d", len(d.Verdicts))
	}
	want := map[string]council.Decision{"Melchior": council.Done, "Balthasar": council.Continue, "Casper": council.Done}
	for _, v := range d.Verdicts {
		if v.Decision != want[v.Member] {
			t.Errorf("%s: got %q, want %q — a verdict was cast and did not survive the mapping",
				v.Member, v.Decision, want[v.Member])
		}
		if v.Lens == "" || v.Cite == "" {
			t.Errorf("%s: lost its lens or its grounds on the way back", v.Member)
		}
	}
	// The tally is reached exactly as a split round reaches it.
	if d.Decision != council.Done {
		t.Errorf("2-1 done under majority should tally done, got %q", d.Decision)
	}
}

// A reply that speaks for only some of the lenses must cost only those lenses. Before this, one
// short reply would have been three abstentions at once — a round decided by nobody.
func TestPanelMissingLensAbstainsAloneAndSaysWhy(t *testing.T) {
	p := &panelProvider{reply: func(int) string {
		return replyWith([3]string{"Melchior", "correctness", "continue"})
	}}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "x", Members: council.DefaultMembers()})
	var abst int
	for _, v := range d.Verdicts {
		if v.Member == "Melchior" {
			if v.Decision != council.Continue {
				t.Fatalf("the lens that DID answer lost its vote: %q", v.Decision)
			}
			continue
		}
		if v.Decision != council.Abstain {
			t.Fatalf("%s did not answer and must abstain, got %q", v.Member, v.Decision)
		}
		if !strings.Contains(v.Rationale, "did not return a verdict") {
			t.Errorf("%s abstained with no reason on it — a reader cannot tell silence from "+
				"'my lens has nothing to add'", v.Member)
		}
		abst++
	}
	if abst != 2 {
		t.Fatalf("want two abstentions, got %d", abst)
	}
}

// A model that renames the members has still cast its votes. Losing a round to a label is exactly
// the brittleness this shape must not introduce.
func TestPanelMatchesByLensWhenNamesDrift(t *testing.T) {
	p := &panelProvider{reply: func(int) string {
		return replyWith([3]string{"Member 1", "completeness", "continue"},
			[3]string{"Member 2", "correctness", "done"},
			[3]string{"Member 3", "verification", "done"})
	}}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "x", Members: council.DefaultMembers()})
	for _, v := range d.Verdicts {
		if v.Decision == council.Abstain {
			t.Fatalf("%s (%s) was dropped because the reply used a different name", v.Member, v.Lens)
		}
		if v.Lens == "completeness" && v.Decision != council.Continue {
			t.Errorf("the completeness verdict landed on the wrong lens")
		}
	}
}

// One call writing three verdicts can decide once and dress it three ways — that is the whole risk
// of the shape, and the prompt has to spend words on it or the council becomes one voter with
// three names.
func TestPanelPromptDefendsIndependenceAndCarriesTheSharedInstruction(t *testing.T) {
	members := council.DefaultMembers()
	roster := panelRoster(members, false)
	for _, m := range members {
		if !strings.Contains(roster, m.Name) || !strings.Contains(roster, council.RouteFor(m.Lens)) {
			t.Fatalf("the roster must name %s and its route", m.Name)
		}
	}
	for _, want := range []string{"THREE JUDGEMENTS, not one judgement written three times",
		"has not been applied, it has been echoed", "should abstain, not agree"} {
		if !strings.Contains(panelIndependence, want) {
			t.Errorf("the independence clause must carry %q", want)
		}
	}
	// And the judging instruction itself is the same text the other shapes use — not a third
	// retelling of it.
	if !strings.Contains(panelSchema, `"checks"`) ||
		strings.Index(panelSchema, `"checks"`) > strings.Index(panelSchema, `"decision"`) {
		t.Error("the panel schema must ask for each walk before the decision it supports")
	}
}

// The closing call must ask a DIFFERENT question over DIFFERENT material than the verdicts did.
//
// Its first version did not, and it therefore did nothing: it was handed the same system prompt and
// the same evidence — the agent's report included — and asked to conclude again from what it had
// just concluded from. Eleven convenings, not one disagreement with the tally. What makes it a
// second look is that the report is off the table and the walks are on it.
func TestClosingCallJudgesResultsAndWalksNotTheReport(t *testing.T) {
	// The question must not be phrased as a finishing. The clamp lets this call change an outcome in
	// exactly one direction — toward continue — so a prompt that opens "now close the round" nudges
	// it away from the only thing it can do. Three versions opened that way and ratified 15 rounds
	// out of 15.
	for _, banned := range []string{"Now close the round", "close the round"} {
		if strings.Contains(panelCloseAsk, banned) {
			t.Errorf("the closing question is framed as a finishing (%q) — that is the one direction "+
				"it must not be pushed", banned)
		}
	}
	if !strings.Contains(panelCloseAsk, "Both answers are ordinary") {
		t.Error("the question must name both outcomes as ordinary, or it is asking for one of them")
	}
	for _, want := range []string{
		"what the TOOLS RETURNED",
		"is NOT evidence here",
		"Do not re-read it to decide this",
	} {
		if !strings.Contains(panelCloseAsk, want) {
			t.Errorf("the closing question must carry %q — without it, it re-decides on the same "+
				"material and echoes what it already wrote", want)
		}
	}
	// Reframing was tried twice and ratified both times. What makes this a second look is the VIEW:
	// the three walks side by side, which no member had while writing one of them. Each of these
	// three defects is invisible from inside a single walk.
	for _, want := range []string{"CONTRADICTION", "GAP", "WRONG ON ITS FACE",
		"appears in NO walk", "not one the task's subject admits", "off from what the subject admits by the SAME factor"} {
		if !strings.Contains(panelCloseAsk, want) {
			t.Errorf("the closing call must be given the across-the-walks view: missing %q", want)
		}
	}
	// The plausibility faculty failed its first real test in two specific ways, and both are named
	// so it cannot fail them the same way again: it read two values wrong by the same factor as
	// "self-consistent" and therefore fine, and it accepted the agent's own account of why the
	// numbers looked wrong because a lens had repeated that account inside a walk.
	for _, want := range []string{
		"their agreement is not evidence of anything",
		"Never write that a suspect value is fine because it is self-consistent",
		"AGENT'S OWN EXPLANATION",
		"does not stop being the agent's claim",
	} {
		if !strings.Contains(panelCloseAsk, want) {
			t.Errorf("the two traps the faculty already fell into must be named: missing %q", want)
		}
	}
	// It must not quietly become a fourth vote on the same evidence.
	if strings.Contains(panelCloseAsk, "count votes") && !strings.Contains(panelCloseAsk, "Do not count votes") {
		t.Error("the closing call must not be a tally")
	}
}

// The round's own conclusion may only ever make finishing HARDER. Measured twice, in two arms: a
// lens found that a command the TASK named had only been run on a placeholder, and majority rule
// deleted that verdict 2-1 — the task then failed on exactly the value it doubted. So a close that
// says continue overrides a done. The reverse must never happen: this council's measured failure is
// over-approval, and a close free to overrule a blocking tally would be a second road to done.
func TestClosingConclusionOnlyTightens(t *testing.T) {
	run := func(verdicts, closing string) council.Deliberation {
		t.Helper()
		p := &panelProvider{reply: func(n int) string {
			if n == 0 {
				return verdicts
			}
			return closing
		}}
		c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
		d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
			Task: "ship it", Actions: "x", Members: council.DefaultMembers()})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	allDone := replyWith([3]string{"Melchior", "correctness", "done"},
		[3]string{"Balthasar", "verification", "done"}, [3]string{"Casper", "completeness", "done"})
	allCont := replyWith([3]string{"Melchior", "correctness", "continue"},
		[3]string{"Balthasar", "verification", "continue"}, [3]string{"Casper", "completeness", "continue"})

	d := run(allDone, `{"rationale":"Melchior's item 3 is settled only by NO-EVIDENCE",`+
		`"decision":"continue","feedback":"run the command the task names on the real input"}`)
	if d.Decision != council.Continue {
		t.Fatalf("a unanimous done the round itself would not stand behind must not finish, got %q", d.Decision)
	}
	if d.Feedback == "" {
		t.Error("a continue with nothing to act on sends the agent back empty-handed")
	}

	if d := run(allCont, `{"rationale":"looks fine","decision":"done","feedback":""}`); d.Decision != council.Continue {
		t.Fatalf("the closing conclusion talked a BLOCKING tally into done — a second road to done, "+
			"not a check on the first; got %q", d.Decision)
	}
	if d := run(allDone, "I think it is probably fine"); d.Decision != council.Done {
		t.Fatalf("an unreadable close must leave the tally alone, got %q", d.Decision)
	}
}

// The third eye moved to the correctness lens, so the closing call's job there changed from doing
// the check to checking that it was done. If it still claims nobody checks magnitude it is telling
// the reader something that is no longer true, and a prompt that lies about the rest of the system
// teaches the model to discount it.
func TestTheClosingCallBackstopsTheValueCheckInsteadOfOwningIt(t *testing.T) {
	if strings.Contains(panelCloseAsk, "nobody checks magnitude") {
		t.Fatal("the correctness lens checks magnitude now; the closing call must not claim otherwise")
	}
	for _, want := range []string{
		"correctness lens is asked to check this",
		"whether it DID",
		"walked PROVENANCE and stopped",
	} {
		if !strings.Contains(panelCloseAsk, want) {
			t.Fatalf("the closing call must backstop the lens, not replace it (missing %q)", want)
		}
	}
	// The two traps stay here as well: a lens can write one of them INTO its walk, and then the
	// walk is the thing carrying the error.
	for _, want := range []string{"CONSISTENCY", "AGENT'S OWN EXPLANATION", "already written one of them into its walk"} {
		if !strings.Contains(panelCloseAsk, want) {
			t.Fatalf("the closing call lost a trap it still needs (missing %q)", want)
		}
	}
}

// The closing call carries no verdict slot, so it reaches the agent only if the round carries its
// words. Both of what it said travel: the diagnosis and the ask.
func TestTheRoundCarriesWhatTheClosingCallSaid(t *testing.T) {
	got := closeSaid(panelClose{Rationale: "the value cannot be that large", Feedback: "read the units first"})
	for _, want := range []string{"the value cannot be that large", "read the units first"} {
		if !strings.Contains(got, want) {
			t.Fatalf("closeSaid dropped %q: %q", want, got)
		}
	}
	if got := closeSaid(panelClose{Rationale: "same", Feedback: "same"}); got != "same" {
		t.Fatalf("identical rationale and feedback should not be said twice: %q", got)
	}
	if got := closeSaid(panelClose{}); got != "" {
		t.Fatalf("a closing call that never ran says nothing: %q", got)
	}
}

// A reply that carries no JSON is still the model's ANSWER, and the round must not throw it away.
//
// The irreversible-command gate asks a plain yes/no — "does the task actually require this, now,
// in this form?" — and decides on the TEXT that comes back. It reaches the members through the
// same panel that the finish gate uses, so a question answered in prose parses as no verdicts at
// all. Before this, that discarded the reply: three abstains and nothing to show, and a caller
// reading prose was handed silence where a decision had in fact been made. Observed on
// cobol-modernization, 2026-08-23: 1,154 bytes that walked every condition the question named and
// closed "no, it should not run in this form", dropped for carrying no braces.
//
// It comes back as the CLOSE's rationale — the field for what the round concluded — with the
// decision left empty, because verdicts nobody could read must never become a vote.
func TestUnreadableVerdictsStillCarryWhatTheModelSaid(t *testing.T) {
	const prose = "No. The task never mentions /tmp/t2, and dropping the rm -rf leaves a way back."
	p := &panelProvider{reply: func(int) string { return prose }}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "port the COBOL program", Actions: "ran the reference binary",
		Members: council.DefaultMembers()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Close, prose) {
		t.Fatalf("the prose the model answered with was dropped; Close = %q", d.Close)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain {
			t.Fatalf("an unread verdict became a vote: %s voted %q", v.Member, v.Decision)
		}
	}
}

// The suite-walk clause reaches the lens that walks behaviors, and only that one.
//
// It exists because a summary line answered for requirements nothing had asserted: headless-
// terminal, 2026-08-23, done 3-0 on "7 passed, 5 warnings in 16.36s" against a graded suite that
// failed test_background_commands. On by default, with an off switch that is the A/B's other arm,
// so both states are pinned here.
func TestSuiteWalkClauseRidesTheVerificationLensOnly(t *testing.T) {
	members := council.DefaultMembers()
	off := panelRoster(members, false)
	on := panelRoster(members, true)
	if strings.Contains(off, "A SUITE SUMMARY is a count") {
		t.Fatal("MAGI_COUNCIL_SUITE_WALK=0 still carried the clause; the off arm is not off")
	}
	if !strings.Contains(on, "A SUITE SUMMARY is a count") {
		t.Fatal("MAGI_COUNCIL_SUITE_WALK did not reach the roster")
	}
	if strings.Count(on, "A SUITE SUMMARY is a count") != 1 {
		t.Fatalf("the clause landed on more than the verification lens:\n%s", on)
	}
	// The clause must not read as "write more tests" — that is the churn the council was already
	// over-doing, and the sentence that forbids it is the reason this is safe to try at all.
	if !strings.Contains(on, "not a demand for more tests") {
		t.Fatal("the no-more-tests guard is missing from the clause")
	}
}

// A verdict somebody actually gave is not silent. The slots start as placeholders marked
// silent:true — a verdict nobody gave — and the merge filled every other field over them while
// leaving the mark, so every panel verdict shipped silent:true and three screens drew spoken
// verdicts as "no answer".
func TestASpokenPanelVerdictIsNotMarkedSilent(t *testing.T) {
	p := &panelProvider{reply: func(int) string {
		return replyWith([3]string{"Melchior", "correctness", "done"},
			[3]string{"Balthasar", "verification", "abstain"})
	}}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "wrote hello.txt", Members: council.DefaultMembers()})
	if err != nil {
		t.Fatal(err)
	}
	var spoke, silent int
	for _, v := range d.Verdicts {
		switch {
		case strings.HasPrefix(v.Rationale, "the council panel did not return"):
			silent++
			if !v.Silent {
				t.Errorf("%s never answered and is not marked silent", v.Member)
			}
		default:
			spoke++
			if v.Silent {
				t.Errorf("%s answered %q and is marked silent", v.Member, v.Decision)
			}
		}
	}
	if spoke != 2 || silent != 1 {
		t.Fatalf("want two spoken verdicts and one unanswered slot, got %d and %d", spoke, silent)
	}
}

// A member who genuinely abstained keeps their own words: the lens fallback used to treat "an
// abstain with a rationale" as an empty slot, so an unrelated verdict sharing the lens could
// overwrite a real one.
func TestAGenuineAbstainIsNotOverwrittenByTheLensFallback(t *testing.T) {
	p := &panelProvider{reply: func(int) string {
		// Melchior answers by NAME with an abstain; a second entry claims the same lens.
		return replyWith([3]string{"Melchior", "correctness", "abstain"},
			[3]string{"someone-else", "correctness", "done"})
	}}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return p }}
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "wrote hello.txt", Members: council.DefaultMembers()})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range d.Verdicts {
		if v.Member == "Melchior" && v.Decision != council.Abstain {
			t.Fatalf("Melchior's own abstain was overwritten with %q", v.Decision)
		}
	}
}
