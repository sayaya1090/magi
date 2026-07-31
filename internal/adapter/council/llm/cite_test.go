package llm

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// The record as a member actually sees it: each command, then its exit and its output. The script
// body is part of the command — that is the whole point of the case below.
const citeRecord = "" +
	"── WHAT MAGI OBSERVED ──\n" +
	"- tool bash [ok] python3 << 'EOF'\n" +
	"import asyncio\n" +
	"async def task_with_cleanup():\n" +
	"    try:\n" +
	"        await asyncio.sleep(10)\n" +
	"    finally:\n" +
	"        print(\"Cleanup code ran!\")\n" +
	"EOF: exit 0 ⏎ output: /tmp/magi-bash-4178576160.log (69 bytes — all of it is above) ⏎ " +
	"Function signature verified - KeyboardInterrupt handling implemented ⏎\n" +
	"- tool write [ok] /app/run.py: wrote 1601 bytes to /app/run.py\n"

// The verdict this exists for. Measured live (cancel-async-tasks, 2026-08-01): a member voted done
// at full confidence "as verified by the test output showing 'Cleanup code ran!' when interrupted".
//
// The string IS in the material the member was shown — inside the heredoc of a script the agent
// sent and never ran. So an existence check answers "yes" and misses it entirely; the question
// that separates the two is whether the text came BACK. Grounding a verdict in a command body is
// grounding it in what was sent.
func TestGroundsFromACommandBodyAreNotSomethingThatCameBack(t *testing.T) {
	misses := checkCites(citeRecord, "print(\"Cleanup code ran!\")", "", "")
	if len(misses) != 1 {
		t.Fatalf("want the misattributed citation, got %d: %+v", len(misses), misses)
	}
	if !misses[0].sentNotReturned {
		t.Error("this text is in the record; the miss must say it was sent rather than returned")
	}
	// …and the re-ask has to make that difference readable, since the correction differs.
	if msg := citeRetryReminder(misses); !strings.Contains(msg, "only inside a command that was sent") {
		t.Errorf("the re-ask does not distinguish sent from returned:\n%s", msg)
	}
	// The same text quoted in PROSE is left alone: a member may point at a command it is talking
	// about, and rejecting that would be the over-demand this council has been burned by.
	if got := checkCites(citeRecord, "", "the script would print 'Cleanup code ran!' if it ran", ""); len(got) != 0 {
		t.Errorf("quoting a command in prose was treated as a false claim: %+v", got)
	}
}

// Invention is the other half, and it is what the prose check is for: text that is nowhere in the
// record at all.
func TestAnInventedQuotationInProseIsCaught(t *testing.T) {
	misses := checkCites(citeRecord, "", "the run reported 'all 7 integration tests passed'", "")
	if len(misses) != 1 {
		t.Fatalf("want the invented quotation, got %d: %+v", len(misses), misses)
	}
	if misses[0].sentNotReturned {
		t.Error("nothing in the record contains this; it was invented, not misattributed")
	}
	if misses[0].field != "rationale" {
		t.Errorf("flagged in %q, want rationale", misses[0].field)
	}
}

// An English possessive is not an opening quote. "tasks\' finally blocks … showing \'X\'" has three
// apostrophes, and pairing them left to right flags a span the member never claimed while missing
// the one it did.
func TestAnApostropheIsNotAQuotation(t *testing.T) {
	rationale := "allowing in-progress tasks' finally blocks to run, and the agent didn't skip them"
	if misses := checkCites(citeRecord, "", rationale, ""); len(misses) != 0 {
		t.Errorf("possessives and contractions were read as quotations: %+v", misses)
	}
}

// Quoting what IS there passes, including across the ⏎ the evidence block uses for a tool result's
// newlines and across a … it cut with. A member that copies the record correctly must never be
// sent back over how the record was rendered.
func TestQuotingTheRecordPasses(t *testing.T) {
	for _, q := range []string{
		"Function signature verified - KeyboardInterrupt handling implemented",
		"wrote 1601 bytes to /app/run.py",
		"exit 0 … output: /tmp/magi-bash-4178576160.log", // quoted across the rendered newline, with a cut
	} {
		if misses := checkCites(citeRecord, q, "", ""); len(misses) != 0 {
			t.Errorf("a real fragment was rejected: %q → %+v", q, misses)
		}
	}
}

// NO-EVIDENCE is a normal answer, not a miss. Many turns — an investigation, an answer, a review —
// legitimately observe nothing to quote, and demanding a quotation from them is the reflexive
// over-demand this council has been burned by before.
func TestNoEvidenceIsAnAnswerNotAMiss(t *testing.T) {
	for _, c := range []string{citeNoEvidence, "no-evidence", " NO-EVIDENCE ", ""} {
		if misses := checkCites(citeRecord, c, "the report answers the question asked", ""); len(misses) != 0 {
			t.Errorf("cite %q was treated as a false quotation: %+v", c, misses)
		}
	}
}

// Short quoted words are left alone. A member writing "done" or 'the parser' in quotes is using
// punctuation, not claiming to read something back, and failing those would reject verdicts over
// prose style.
func TestShortQuotedWordsAreNotClaims(t *testing.T) {
	if misses := checkCites(citeRecord, "", `the "parser" is fine and the vote is "done"`, ""); len(misses) != 0 {
		t.Errorf("punctuation was read as a claim about the record: %+v", misses)
	}
	// …but a full invented sentence is caught wherever it appears, feedback included.
	if misses := checkCites(citeRecord, "", "", `the log says "FAILED 3 of 7 integration tests"`); len(misses) != 1 {
		t.Errorf("an invented quotation in feedback was not caught: %+v", misses)
	}
}

// Case is kept. A member that reports FAILED where the record says failed is reporting something
// the record does not say, and this is the one place that distinction is cheap to preserve.
func TestTheCheckIsCaseSensitive(t *testing.T) {
	if misses := checkCites("the suite reported 12 tests passed cleanly", "12 TESTS PASSED CLEANLY", "", ""); len(misses) != 1 {
		t.Errorf("a changed-case quotation passed: %+v", misses)
	}
}

// The re-ask names every miss and both honest ways out, and never says which way to vote.
func TestTheReAskNamesTheMissesAndNotTheVote(t *testing.T) {
	msg := citeRetryReminder([]citeMiss{
		{quote: "Cleanup code ran!", field: "rationale"},
		{quote: strings.Repeat("x", 400), field: "cite"},
	})
	for _, want := range []string{"Cleanup code ran!", "rationale", citeNoEvidence, "not its output"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the re-ask does not mention %q:\n%s", want, msg)
		}
	}
	for _, forbidden := range []string{"vote continue", "vote done", "you should vote"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("the re-ask tells the member how to vote (%q):\n%s", forbidden, msg)
		}
	}
	// A long quotation is cut so it cannot push the rest of the reminder out of view.
	if strings.Contains(msg, strings.Repeat("x", 200)) {
		t.Error("a 400-char quotation was echoed whole")
	}
}

// The flag is default-on and turns the whole check off for an A/B.
func TestTheCiteCheckIsOnByDefaultAndCanBeTurnedOff(t *testing.T) {
	t.Setenv("MAGI_COUNCIL_CITE", "")
	if !citeEnabled() {
		t.Error("the check must be on when nothing is set")
	}
	for _, off := range []string{"0", "false", "OFF"} {
		t.Setenv("MAGI_COUNCIL_CITE", off)
		if citeEnabled() {
			t.Errorf("MAGI_COUNCIL_CITE=%q did not turn the check off", off)
		}
	}
}

// End to end through Deliberate: a member whose grounds are not in the record is asked once more,
// and a member that repeats them loses its vote to an abstain rather than carrying the tally.
//
// Abstain, not a flipped vote. magi has measured whether the grounds are real, which is a
// different question from which way the verdict should go, and answering the second with the
// first would be magi voting.
func TestAMemberThatStandsByInventedGroundsAbstains(t *testing.T) {
	var asks atomic.Int32
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		asks.Add(1)
		return `{"decision":"done","confidence":1.0,"rationale":"verified by the output showing 'all 12 checks passed cleanly'","cite":"all 12 checks passed cleanly"}`
	}}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "make the tests pass", Actions: "- tool bash [ok] make test: exit 1 ⏎ output: /tmp/x.log (9 bytes) ⏎ 3 failed ⏎",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := asks.Load(); n != 6 { // three members, each asked twice
		t.Errorf("members were asked %d times, want 6 (one re-ask each)", n)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain {
			t.Errorf("%s kept its %q vote on grounds that are not in the record", v.Member, v.Decision)
		}
		if !strings.Contains(v.Rationale, "not in the record") {
			t.Errorf("%s: the abstain does not say why: %q", v.Member, v.Rationale)
		}
	}
}

// A member that corrects itself keeps its vote — the re-ask is a chance, not a penalty.
func TestAMemberThatCorrectsItsGroundsKeepsItsVote(t *testing.T) {
	// Members are polled in PARALLEL, so "the fourth call" is not "the first re-ask" — the reply
	// keys on the reminder the re-ask carries, which is what actually distinguishes the two.
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		if strings.Contains(textOf(r), "quoted things that are not in the record") {
			return `{"decision":"done","confidence":0.8,"rationale":"the build ran","cite":"exit 0"}`
		}
		return `{"decision":"done","rationale":"the log said 'every check passed cleanly'","cite":"every check passed cleanly"}`
	}}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "build it", Actions: "- tool bash [ok] make: exit 0 ⏎ output: /tmp/x.log (3 bytes) ⏎ ok ⏎",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != council.Done {
		t.Fatalf("decision = %q, want done — the members corrected their grounds", d.Decision)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Done {
			t.Errorf("%s = %q after correcting itself", v.Member, v.Decision)
		}
	}
}

// With the check off, the run is exactly what it was before: one ask per member, grounds unread.
func TestWithTheCheckOffNothingIsReAsked(t *testing.T) {
	t.Setenv("MAGI_COUNCIL_CITE", "0")
	var asks atomic.Int32
	c := New(only(fakeLLM{reply: func(port.ChatRequest) string {
		asks.Add(1)
		return `{"decision":"done","rationale":"the log said 'nothing of the sort happened here'"}`
	}}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if err != nil {
		t.Fatal(err)
	}
	if n := asks.Load(); n != 3 {
		t.Errorf("members were asked %d times with the check off, want 3", n)
	}
	if d.Decision != council.Done {
		t.Errorf("decision = %q, want done", d.Decision)
	}
}
