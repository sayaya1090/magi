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
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
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
	// Severity is how serious the member says its revise (continue) vote is, on a plan-audit
	// round. Nothing gates on it — the plan gate that read it, and its absent-means-warn /
	// unrecognized-means-critical rules, went with the audit — so this is carried to the
	// transcript for a reader to weigh and for the panel to label. Unused in the termination
	// phase.
	Severity string `json:"severity,omitempty"`
}

// Severity tiers a member may put on its verdict. Nothing gates on them any more — the plan audit
// that did is gone — so they are what the member said, carried through to the transcript for a
// reader to weigh.
const (
	SeverityCritical = "critical"
	SeverityWarn     = "warn"
	SeverityInfo     = "info"
)

// Breakdown is the counted result of a tally — kept on the Deliberation so the
// outcome is observable and replayable.
type Breakdown struct {
	Done       int     `json:"done"`
	Continue   int     `json:"continue"`
	Abstain    int     `json:"abstain"`
	DoneWeight float64 `json:"doneWeight"`
	ContWeight float64 `json:"contWeight"`
	Voters     int     `json:"voters"` // non-abstaining members (the denominator)
	Rule       Rule    `json:"rule"`
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

// flexText is a check's free-text field, tolerant of the shapes a model actually emits where the
// schema says string: a plain string, a NUMBER, or a LIST of either. Go's decoder aborts the whole
// document on the first type mismatch, so one such field discarded every OTHER check in the array
// — leaving a run with no executable contract at all while the model had authored a usable one.
// (This package stays stdlib-only, so the shared repair ladder in internal/jsonx is not imported
// here; the tolerance is the same, applied at the domain boundary.)
type flexText string

func (v *flexText) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*v = flexText(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*v = flexText(n.String())
		return nil
	}
	var list []flexText
	if json.Unmarshal(b, &list) == nil {
		parts := make([]string, 0, len(list))
		for _, e := range list {
			if t := strings.TrimSpace(string(e)); t != "" {
				parts = append(parts, t)
			}
		}
		*v = flexText(strings.Join(parts, "; "))
		return nil
	}
	*v = "" // anything else → unset, never a discarded check
	return nil
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
	case "unanimous":
		// Every voter must say done, and there must be at least one voter.
		if b.Voters > 0 && b.Done == b.Voters {
			return Done, b
		}
	case "quorum":
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
	case "weighted":
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
	case "veto":
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
	case "", "majority":
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
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + " […keep truncated]"
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
