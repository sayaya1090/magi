// Package council holds the pure domain of magi's signature feature: a consensus
// council that takes the loop's termination decision away from a single model
// (D14). At the point the agent loop would naturally finish, a council of members
// votes "done" or "continue"; a consensus rule tallies the votes into one
// decision. A "continue" injects the members' aggregated feedback back into the
// loop instead of stopping (closing loop-engineering pains #1 termination-monopoly
// and #3 feedback-wiring).
//
// This package is pure domain — it imports nothing outside the standard library.
// The fan-out that actually asks each member (over an LLMProvider) is an adapter;
// the consensus logic here is deterministic and unit-tested in isolation. That
// split is what lets "the council decides, not one model" be a tested invariant
// rather than a prompt.
package council

import (
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/text"
)

// Decision is a member's vote and, in the aggregate, the council's outcome.
// Members may also Abstain; the council outcome is only Done or Continue.
type Decision string

const (
	Done     Decision = "done"     // the member/council considers the task finished
	Continue Decision = "continue" // more work is needed (carries feedback)
	Abstain  Decision = "abstain"  // the member declines to vote (excluded from the denominator)
)

// Member is a council seat: a theme-name label (the MAGI — Melchior/Balthasar/
// Casper) with a judging lens as its attribute. Model/Weight are optional; an
// empty Model means "use the session model" and a zero Weight counts as 1.
type Member struct {
	Name     string  `json:"name"`               // label, e.g. "Melchior"
	Lens     string  `json:"lens"`               // attribute, e.g. "correctness"
	Model    string  `json:"model,omitempty"`    // empty = the request's default model
	Provider string  `json:"provider,omitempty"` // named LLM backend/profile; empty = default backend
	Weight   float64 `json:"weight,omitempty"`   // 0 = 1
}

// OnePanel says whether these members are judged in a single call.
//
// It is one call when they share a backend, and one per member when they do not — a weak model and
// a strong one judging the same work is the point of that configuration, and folding it into one
// request would answer with whichever backend came first. The adapter decides the shape from this,
// and a reader telling somebody what the council is doing has to describe the same shape, so the
// predicate lives here rather than in either of them.
func OnePanel(members []Member) bool {
	if len(members) < 2 {
		return false
	}
	for _, m := range members[1:] {
		if m.Provider != members[0].Provider || m.Model != members[0].Model {
			return false
		}
	}
	return true
}

// Verdict is one member's evaluation at the termination gate.
type Verdict struct {
	Member     string   `json:"member"`               // the member's label
	Lens       string   `json:"lens,omitempty"`       // the member's lens
	Decision   Decision `json:"decision"`             // done | continue | abstain
	Confidence float64  `json:"confidence,omitempty"` // 0..1, self-reported
	Rationale  string   `json:"rationale,omitempty"`  // why
	Feedback   string   `json:"feedback,omitempty"`   // actionable, used when Continue
	Keep       string   `json:"keep,omitempty"`       // what the report already gets right (advisory, MAGI_COUNCIL_KEEP)
	// Cite is the fragment of the record this verdict rests on, copied verbatim by the member, or
	// the token NO-EVIDENCE when it rests on the report's substance rather than on anything
	// observed. Recorded and shown, not checked: magi used to look each fragment up and downgrade
	// a member whose grounds it could not find, and that check produced two false abstentions in
	// thirty verdicts and caught nothing — see the adapter's cite.go for why it is gone. What is
	// here is what the member said it was reading.
	Cite   string  `json:"cite,omitempty"`
	Weight float64 `json:"weight,omitempty"` // 0 = 1
	// Silent marks a verdict nobody gave: the backend was down, the round ran out of time, the
	// reply could not be read, or a panel answer never spoke for this lens. It carries Abstain
	// because the tally must not count it as a vote — but "declined to vote" and "never answered"
	// are different facts about a round, and a reader who cannot tell them apart reads an
	// unreachable council as a council that considered the work and shrugged.
	Silent bool `json:"silent,omitempty"`
}

// Breakdown is the counted result of a tally — kept on the Deliberation so the
// outcome is observable and replayable.
type Breakdown struct {
	Done       int     `json:"done"`
	Continue   int     `json:"continue"`
	Abstain    int     `json:"abstain"`
	DoneWeight float64 `json:"doneWeight"`
	ContWeight float64 `json:"contWeight"`
	Voters     int     `json:"voters"` // non-abstaining members (the denominator)
	// Silent counts the abstentions that were failures rather than choices (Verdict.Silent).
	// Silent <= Abstain always.
	Silent int  `json:"silent,omitempty"`
	Rule   Rule `json:"rule"`
}

// Deliberation is the record of one council round: the verdicts, the rule applied,
// the decision, its breakdown, and (on Continue) the merged feedback.
type Deliberation struct {
	Round     int       `json:"round"`
	Verdicts  []Verdict `json:"verdicts"`
	Decision  Decision  `json:"decision"`
	Breakdown Breakdown `json:"breakdown"`
	Feedback  string    `json:"feedback,omitempty"`
	// Keep is the merged advisory "what's already correct — don't redo/revert" from the
	// members (MAGI_COUNCIL_KEEP). Purely informational: it never affects the decision or
	// tally, and is surfaced ABOVE the feedback when the turn continues.
	Keep string `json:"keep,omitempty"`
	// Close is what the round's closing call said — the one reader that saw all three walks
	// together, written after them and separate from all three.
	//
	// It is carried on the Deliberation because it is the only voice in the round with no other
	// way to reach the agent. A member that votes continue has its own Feedback rendered under
	// its own name; the closing call has no verdict slot, so before this field existed its words
	// went to stderr and to the event log and nowhere else. Measured on one task: three members
	// voted done three times, the closing call caught a scale error in the written values and
	// turned each round back, and the agent read three blocks all saying everything was satisfied
	// under a heading telling it to address what follows. It spent all three rounds guessing —
	// re-writing the same numbers through a tracked tool, restating its method in prose, then
	// reporting a goodness-of-fit — and never once looked at the thing it was being turned back
	// for. A gate that finds a defect and does not say what it found is a gate that only spends
	// the clock.
	Close string `json:"close,omitempty"`
	// Debate records a disagreement-triggered rebuttal round: nil when it did not run
	// (unanimous vote, or debate disabled), non-nil with the before→after decisions
	// when it did — so the otherwise-internal rebuttal is observable in the transcript.
	Debate *DebateOutcome `json:"debate,omitempty"`
}

// DebateOutcome summarizes one rebuttal round for observability: the pre-debate and
// post-debate decisions, plus how many members changed their vote.
type DebateOutcome struct {
	Before  Decision `json:"before"`  // council decision on the independent vote
	After   Decision `json:"after"`   // council decision after the rebuttal
	Changed int      `json:"changed"` // members whose vote flipped in the rebuttal
}

// DefaultMembers returns the three default council members — the MAGI. The theme
// name is the label; the lens is the attribute (the decision the user confirmed).
func DefaultMembers() []Member {
	return []Member{
		{Name: "Melchior", Lens: "correctness"},
		{Name: "Balthasar", Lens: "verification"},
		{Name: "Casper", Lens: "completeness"},
	}
}

// Lenses maps each lens to a one-line description of what that member judges.
// Pure data, reused by the adapter to build each member's system prompt.
var Lenses = map[string]string{
	"correctness": "Is the work correct? Consider edge cases and regressions.",
	"verification": "Is there evidence it works (build/tests pass)? Don't accept claims without proof. " +
		"When the task's acceptance involves an EXTERNAL event — a signal (Ctrl-C/SIGINT), a kill, a " +
		"disconnect, a restart — demand evidence that the event was delivered for REAL (a subprocess " +
		"receiving the actual signal), not simulated in-process (raising the exception by hand): the " +
		"delivery semantics differ, and a handler that only fires in the simulation is dead code in " +
		"the real scenario. No real-delivery evidence → not done. More generally, an EXECUTABLE " +
		"deliverable (a program, script, or server) claimed done needs evidence it actually RAN at " +
		"least once against its primary scenario — importing or compiling it is not running it.",
	"completeness": "Did it do everything the task/plan asked for? Nothing left unfinished.",
}

// Routes says where each lens walks FIRST through the same evidence.
//
// The lens alone did not differentiate the members. Measured over one A/B arm: three members, one
// line of lens apiece and every other instruction identical, voted done 21 times out of 21 with no
// dissent — three samples of one opinion rather than three opinions. A route does not partition
// JURISDICTION (that would be worse: a defect living in one member's slice would draw one continue
// against two uninformed dones, and a majority rule would wave it through). It partitions the ORDER
// OF SEARCH, so all three still judge the whole task and a defect missed on one path can still be
// met on another.
var Routes = map[string]string{
	"correctness": "Walk the task's LITERAL words first — the exact values, formats, names, spellings, " +
		"locations, and types it dictates — and compare each against what the turn actually produced, token " +
		"by token. Then the premises: a fact the work rests on that was recalled or assumed rather than " +
		"looked up or tested. Then the VALUES THEMSELVES, which is yours alone and is not the same question " +
		"as where they came from: for every number the work reports, ask whether it is one the task's " +
		"SUBJECT admits — a measured constant far outside its known range, a count that cannot be that " +
		"large, a duration that cannot be that short, a converged fit whose parameters sit nowhere near the " +
		"thing being fitted. A number can be produced by exactly the right command, on the real input, and " +
		"still be wrong, and the tool result that carries it will look like evidence for it. Two answers " +
		"will be offered for why a suspect number is fine, and both are traps. The first is CONSISTENCY: " +
		"values derived from the same input agree with each other whether that input was read correctly or " +
		"not, so their agreement is not evidence of anything: if two of them are off from what the subject admits by the SAME factor, that agreement is " +
		"the SYMPTOM of one common cause upstream — a unit, a column, an index read as a coordinate — and " +
		"the more exactly they agree the stronger the case that something is wrong. Never write that a value " +
		"is fine because it is self-consistent or because a ratio between two suspect values comes out " +
		"right. The second is the AGENT'S OWN EXPLANATION of why the number looks wrong — that the axis is " +
		"not what it appears to be, that the units are implicit, that the scale is arbitrary. That is part " +
		"of the claim under examination, not a resolution of it; accept it only where a TOOL RETURNED " +
		"something showing it is true.",
	"verification": "Walk the BEHAVIORS first — for each thing the task says must work, find the moment it " +
		"was actually run and the real output that came back. A requirement whose only support is the agent's " +
		"account of it has not been walked at all; find the tool result or say NO-EVIDENCE.",
	"completeness": "Walk the task's PARTS first — enumerate every distinct thing it asked for, including " +
		"the ones named once in passing and never mentioned again, and find where each was delivered. The " +
		"part that disappeared quietly between the plan and the report is the one to look for.",
}

// SuiteWalkClause tightens the verification route: a suite SUMMARY is not per-requirement
// evidence.
//
// The route already asks for the moment each behavior was run. What slipped through is the line
// that answers for all of them at once. Measured on headless-terminal, 2026-08-23: three lenses
// voted done 3-0 citing "7 passed, 5 warnings in 16.36s", and the graded suite failed
// test_background_commands. Every citation was a real tool result — the count was true — and it
// still covered a requirement nothing had asserted.
//
// Deliberately NOT a rule about who wrote the test. The doctrine already refuses to dismiss an
// agent's own passing exercise as "mere simulation", and that clause was earned: demanding a
// harder reproduction than the task asked for is the churn the council was over-doing before. The
// question here is not whose assertion it is, it is WHICH REQUIREMENT it speaks to. The closing
// sentence says so outright, because a member reading this while looking for a reason to vote
// continue is exactly the reader who would turn it into "write more tests".
const SuiteWalkClause = " A SUITE SUMMARY is a count, not a walk: \"N passed\", \"all tests green\", " +
	"a coverage percentage — these say how many assertions held, never WHICH requirement each one " +
	"covers. Cite the assertion, or the output line, that shows THIS requirement holding. When the " +
	"only support for a requirement is a summary line, it is NO-EVIDENCE for that requirement, " +
	"however green the suite is — and that is not a demand for more tests, it is a demand to name " +
	"which existing one speaks to it."

// RouteWith returns the lens's route, with the suite-walk clause appended when asked for. The
// clause only means anything to the lens that walks behaviors; the others are unchanged.
func RouteWith(lens string, suiteWalk bool) string {
	r := RouteFor(lens)
	if suiteWalk && lens == "verification" {
		return r + SuiteWalkClause
	}
	return r
}

// RouteFor returns the lens's route, or a neutral one for an unrecognized lens.
func RouteFor(lens string) string {
	if r := Routes[lens]; r != "" {
		return r
	}
	return "Walk the task's requirements in the order the task states them."
}

// Deliberate tallies the verdicts under the rule and assembles a Deliberation,
// including the aggregated feedback when the decision is Continue. This is the
// pure entry point the council adapter calls after collecting verdicts.
func Deliberate(round int, vs []Verdict, rule Rule) Deliberation {
	dec, b := Tally(vs, rule)
	d := Deliberation{Round: round, Verdicts: vs, Decision: dec, Breakdown: b}
	if dec == Continue {
		d.Feedback = AggregateFeedback(vs)
		d.Keep = AggregateKeep(vs) // advisory; empty unless MAGI_COUNCIL_KEEP asked for it
	}
	return d
}

// Tally applies a consensus rule to the verdicts and returns the council decision
// with its breakdown. It is a pure function: same input, same output, no I/O.
//
// Invariant that makes the council safe against early termination: an unmet
// quorum, no voters, or an unrecognized rule all resolve to Continue, and under
// the default majority rule a count-tie does too (strict majority: 50/50 →
// Continue). The one deliberate exception is weighted:θ — an explicit threshold
// where a done-weight share of exactly θ affirmatively finishes (>= θ), so a
// weighted:0.5 tie is Done by design (see TestTallyWeightedExactTie). The council
// never finishes the loop unless its rule is affirmatively satisfied.
func Tally(vs []Verdict, rule Rule) (Decision, Breakdown) {
	b := tallyVotes(vs)
	b.Rule = rule
	name, param := rule.parse()

	switch name {
	case string(RuleUnanimous):
		// Every voter must say done, and there must be at least one voter.
		if b.Voters > 0 && b.Done == b.Voters {
			return Done, b
		}
	case string(RuleQuorum):
		// At least k members voted done. A non-positive/garbage k would let an
		// all-continue vote finish (Done >= 0 is always true), breaking the
		// never-finish-unless-affirmed invariant, so clamp k to >= 1.
		k := atoi(param, 1)
		if k < 1 {
			k = 1
		}
		if b.Done >= k {
			return Done, b
		}
	case string(RuleWeighted):
		// Weighted share of "done" meets the threshold θ. A non-positive θ would
		// always pass (DoneWeight >= 0), so treat it as the default.
		theta := atof(param, 0.5)
		if theta <= 0 {
			theta = 0.5
		}
		total := b.DoneWeight + b.ContWeight
		if total > 0 && b.DoneWeight >= theta*total {
			return Done, b
		}
	case string(RuleVeto):
		// Any designated member voting non-done vetoes a finish; otherwise the
		// rest is a plain majority. An empty veto list degrades to majority.
		for _, v := range vs {
			if v.Decision == Abstain {
				continue
			}
			if param != "" && memberListed(param, v.Member) && v.Decision != Done {
				return Continue, b
			}
		}
		if isMajority(b) {
			return Done, b
		}
	case "", string(RuleMajority):
		// Strict majority of non-abstaining voters. A tie ([done,continue]) is
		// NOT a majority → Continue.
		if isMajority(b) {
			return Done, b
		}
	default:
		// Unknown rule → never finish on it.
	}
	return Continue, b
}

// AggregateFeedback merges the feedback of every member that voted to continue
// into one actionable directive for the next loop iteration. Returns "" when no
// continuing member supplied feedback.
func AggregateFeedback(vs []Verdict) string {
	return mergeFeedback(vs,
		func(v Verdict) bool { return v.Decision == Continue },
		"The council did not agree the task is done. Address this feedback, then continue:")
}

// AggregateKeep merges the members' advisory "keep" notes — what the report already gets
// right, that the agent should NOT redo or revert — into one block rendered ABOVE the fix
// feedback. It reads every verdict that supplied a keep regardless of vote: an affirmation of
// correct work is useful even from a member who otherwise voted done. Advisory only — it never
// affects the decision or tally. "" when no member supplied a keep (e.g. MAGI_COUNCIL_KEEP off,
// so no member was asked for one).
func AggregateKeep(vs []Verdict) string {
	var parts []string
	for _, v := range vs {
		k := strings.TrimSpace(v.Keep)
		if k == "" {
			continue
		}
		label := v.Member
		if v.Lens != "" {
			label += " (" + v.Lens + ")"
		}
		parts = append(parts, "- "+label+": "+clipKeep(k, keepPerMember))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Already correct — keep this, do NOT redo or revert it:\n" + strings.Join(parts, "\n")
}

// A keep is a POINTER at work that already exists, not a copy of it: the writer reading this block
// is holding the artifact the keep refers to, so restating it buys nothing and costs the budget the
// critique itself needs. An unbounded keep is also self-defeating — a member that transcribes the
// whole plan makes every step look equally settled, which is the same as naming none. The cap is
// per member so one verbose lens cannot silence the others, and generous enough that a normal
// "step 3 and the build check" is never touched.
const keepPerMember = 400

// clipKeep truncates on a rune boundary and says it was truncated, so a writer never treats a cut
// mid-sentence as the member's whole thought.
func clipKeep(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Trimmed before the marker, unlike the others: this one ends a sentence the members read, and
	// a space before the bracket reads as a typo rather than a cut.
	return strings.TrimSpace(text.Cut(s, n)) + " […keep truncated]"
}

// mergeFeedback joins the feedback of the verdicts matching keep into a bulleted,
// labeled directive under header. Returns "" when none match or none have feedback.
func mergeFeedback(vs []Verdict, keep func(Verdict) bool, header string) string {
	var parts []string
	for _, v := range vs {
		if !keep(v) {
			continue
		}
		fb := strings.TrimSpace(v.Feedback)
		if fb == "" {
			continue
		}
		label := v.Member
		if v.Lens != "" {
			label += " (" + v.Lens + ")"
		}
		parts = append(parts, "- "+label+": "+fb)
	}
	if len(parts) == 0 {
		return ""
	}
	return header + "\n" + strings.Join(parts, "\n")
}

// --- internals ---

func tallyVotes(vs []Verdict) Breakdown {
	var b Breakdown
	for _, v := range vs {
		w := v.Weight
		if w == 0 {
			w = 1
		}
		switch v.Decision {
		case Done:
			b.Done++
			b.DoneWeight += w
		case Abstain:
			b.Abstain++
			if v.Silent {
				b.Silent++
			}
		default:
			// Continue and any unrecognized vote count as "not done" (safe side).
			b.Continue++
			b.ContWeight += w
		}
	}
	b.Voters = b.Done + b.Continue
	return b
}

// isMajority reports whether "done" is a strict majority of the non-abstaining
// voters (so a tie is not a majority).
func isMajority(b Breakdown) bool { return b.Voters > 0 && b.Done*2 > b.Voters }

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

func atof(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return f
	}
	return def
}

// memberListed reports whether name appears in a comma-separated list (the veto
// rule's parameter), case-insensitively.
func memberListed(list, name string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
