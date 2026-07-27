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
	"regexp"
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
	Weight     float64  `json:"weight,omitempty"`     // 0 = 1
	// Severity tiers a plan-audit revise (continue) vote: only "critical" blocks the
	// plan gate; "warn"/"info" are advisory (heeded but non-blocking). An ABSENT severity
	// is treated as "warn" (permissive — don't block on an omitted field), but a
	// present-but-UNRECOGNIZED value is treated as "critical" (fail-safe — block rather
	// than wave through an unknown tier). Unused in the termination phase.
	Severity string `json:"severity,omitempty"`
	// Criteria is a member's proposed completion criteria (expected deliverables /
	// verification guidance), set only in the plan-audit phase where the council
	// derives the contract the turn is later judged against. Empty otherwise.
	Criteria []string `json:"criteria,omitempty"`
	// Checks is a member's proposed per-step executable deliverable checks, set only
	// in the plan-audit phase. Each pairs an expected deliverable with a shell command
	// (and optional expected output substring) that verifies it deterministically —
	// so the contract can be settled by execution at plan time rather than re-litigated
	// by a vote after the work. Empty otherwise.
	Checks []DeliverableCheck `json:"checks,omitempty"`
}

// DeliverableCheck is one plan step's expected deliverable paired with an executable
// verification. Command is run in the task workdir; the deliverable may be a file, a
// build/test result, or program output on screen (stdout/stderr). The check passes
// when the command exits 0 and — if Expect is non-empty — its output MATCHES Expect
// as a regular expression (Expect == "" means exit-code-only). Step is the plan step
// it belongs to (title or ordinal), used to map a passing check back to its todo; a
// step may carry several checks (several deliverables), and its todo completes only
// when all of them pass. Authored at plan time.
//
// Source+Assert are the TYPED shape, and where Command is model-authored shell, these two are DATA:
// the model names a path and picks an assertion from a closed vocabulary, and the runner builds the
// invocation itself (see runTypedCheck). Every defect a model-authored command can carry needs a
// place to put shell in — a `>` that makes the check create the evidence it then asserts, a `sh
// wrapper.sh` that hides the work, a `|| true; test $? -ne 0` that can never pass — and the typed
// shape has no such place. Command stays for the checks already authored that way; a check carrying
// Assert ignores it.
type DeliverableCheck struct {
	Step        string `json:"step,omitempty"`
	Deliverable string `json:"deliverable,omitempty"`
	Command     string `json:"command"`
	Expect      string `json:"expect,omitempty"`
	Source      string `json:"source,omitempty"`
	Assert      string `json:"assert,omitempty"`
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

// UnmarshalJSON reads `step` as either a string or a NUMBER. The authoring prompt asks members to
// "set `step` to that step's 1-based number", so a model that complies emits 5, not "5" — and a
// strict string field then rejected the whole array, discarding every check in the reply. That is
// how a run ends up with no executable contract at all while the model did exactly as instructed.
func (c *DeliverableCheck) UnmarshalJSON(b []byte) error {
	// A shadow type is required: naming DeliverableCheck here would recurse into this method.
	// The text fields are tolerant for the same reason `step` is: `expect` is where a model most
	// often answers with a NUMBER ("expect": 0 for an exit code), and `command` with a LIST (it
	// enumerates the pipeline as separate elements). Either one strictly typed rejected the whole
	// array and discarded every OTHER check with it.
	var raw struct {
		Step        json.RawMessage `json:"step,omitempty"`
		Deliverable flexText        `json:"deliverable,omitempty"`
		Command     flexText        `json:"command"`
		Expect      flexText        `json:"expect,omitempty"`
		Source      flexText        `json:"source,omitempty"`
		Assert      flexText        `json:"assert,omitempty"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		// A check that arrived as a bare string is the model writing a LINE instead of an object —
		// and there is no telling a runnable command ("make test") from a prose deliverable ("the
		// server answers on 5328"). It is read as the DELIVERABLE, never the command: an invented
		// command would be EXECUTED and could fail forever against work that is actually correct,
		// which is worse than no check. With no command it is inert and MergeChecks drops it — but
		// returning the error here would abort the enclosing array and discard every WELL-FORMED
		// check beside it, which is the failure this tolerates.
		var line string
		if json.Unmarshal(b, &line) == nil && strings.TrimSpace(line) != "" {
			*c = DeliverableCheck{Deliverable: strings.TrimSpace(line)}
			return nil
		}
		return err
	}
	*c = DeliverableCheck{Deliverable: string(raw.Deliverable), Command: string(raw.Command), Expect: string(raw.Expect),
		Source: string(raw.Source), Assert: string(raw.Assert)}
	if len(raw.Step) == 0 {
		return nil
	}
	var asString string
	if json.Unmarshal(raw.Step, &asString) == nil {
		c.Step = asString
		return nil
	}
	var asNumber json.Number
	if json.Unmarshal(raw.Step, &asNumber) == nil {
		c.Step = asNumber.String()
	}
	return nil // any other shape leaves Step empty rather than losing the check
}

// Passes reports whether a completed check's command output satisfies the check.
// A non-zero exit always fails. With no Expect it is exit-code-only. Otherwise Expect
// is matched as a regular expression against out; if Expect is not a valid regexp we
// fall back to a literal substring test rather than fail the whole run on a malformed
// pattern (fail-safe: a bad pattern shouldn't strand a genuinely-done step). Pure.
//
// The output is matched TRIMMED, and an anchored pattern is retried line-wise, because
// out is raw command output while Expect is written by an author describing what the
// command PRINTS. Go anchors `^`/`$` to the whole text unless (?m), and shell output
// essentially always ends in a newline — so an anchored pattern like `^9\.8\.7$` against
// "9.8.7\n" never matched and the check could not pass in ANY world state. That is worse
// than a wrong verdict: it is a permanent false failure that re-runs the step forever.
// Observed live: a package-install step whose packages WERE at the pinned version failed
// its own check six consecutive times, forcing re-plans that burned the wall clock.
func (c DeliverableCheck) Passes(out string, code int) bool {
	if code != 0 {
		return false
	}
	// A typed check is already decided: its verdict IS the runner's exit status, because the runner
	// applied the assertion itself. Expect belongs to the shell shape and must not be re-applied here —
	// a leftover pattern from a converted check would be matched against the runner's own diagnostic
	// text and could only ever fail work that the assertion just passed.
	if strings.TrimSpace(c.Assert) != "" {
		return true
	}
	if c.Expect == "" {
		return true
	}
	out = strings.TrimSpace(out)
	re, err := regexp.Compile(c.Expect)
	if err != nil {
		return strings.Contains(out, c.Expect)
	}
	if re.MatchString(out) {
		return true
	}
	// Line-wise retry: with multi-line output an anchored pattern describes ONE line
	// ("a line reading exactly this"), which whole-text anchoring can never satisfy.
	// Only anchored patterns can gain a match here, so an unanchored pattern is
	// unaffected and this cannot loosen a check that already had a verdict.
	if mre, merr := regexp.Compile("(?m)" + c.Expect); merr == nil {
		return mre.MatchString(out)
	}
	return false
}

// Plan-audit severity tiers. Only SeverityCritical blocks the plan gate; warn/info
// are advisory. A missing/unknown severity normalizes to SeverityWarn.
const (
	SeverityCritical = "critical"
	SeverityWarn     = "warn"
	SeverityInfo     = "info"
)

// Breakdown is the counted result of a tally — kept on the Deliberation so the
// outcome is observable and replayable (loop pains #2/#5).
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
	// Criteria is the synthesized completion criteria from a plan-audit round
	// (merged from the members' proposals). Empty in the termination phase.
	Criteria []string `json:"criteria,omitempty"`
	// Checks is the synthesized per-step executable deliverable checks from a
	// plan-audit round (merged from the members' proposals). Empty in the
	// termination phase. A step may carry several checks (several deliverables).
	Checks []DeliverableCheck `json:"checks,omitempty"`
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

// MergeCriteria synthesizes the members' proposed completion criteria into one
// deduped, bounded list (used in the plan-audit phase to derive the contract the
// turn is later judged against). Pure and order-stable: items are trimmed,
// case-insensitive duplicates are dropped, and the list is capped. No I/O.
func MergeCriteria(vs []Verdict) []string {
	const maxItems, maxRunes = 8, 160
	seen := make(map[string]bool)
	var out []string
	for _, v := range vs {
		for _, c := range v.Criteria {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if r := []rune(c); len(r) > maxRunes { // truncate (rune-safe), don't drop
				c = strings.TrimSpace(string(r[:maxRunes]))
			}
			key := strings.ToLower(c)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
			if len(out) == maxItems {
				return out
			}
		}
	}
	return out
}

// MergeChecks synthesizes the members' proposed per-step deliverable checks into one
// deduped, bounded list (plan-audit phase). A check carrying NEITHER a Command nor an
// Assert has nothing to run and is dropped — a typed check legitimately has no command,
// so "no Command" alone must not decide it. Deduplication keys on whatever the check runs
// (case-insensitive): the command plus its expected pattern, or the assertion plus its
// source. Two members proposing the same verification collapse to one while distinct
// deliverables of the same step are all kept. Fields are trimmed and length-bounded;
// order is stable. Pure, no I/O.
func MergeChecks(vs []Verdict) []DeliverableCheck {
	const maxItems, maxRunes = 16, 300
	clip := func(s string) string {
		s = strings.TrimSpace(s)
		if r := []rune(s); len(r) > maxRunes {
			s = strings.TrimSpace(string(r[:maxRunes]))
		}
		return s
	}
	seen := make(map[string]bool)
	var out []DeliverableCheck
	for _, v := range vs {
		for _, c := range v.Checks {
			c.Step = clip(c.Step)
			c.Deliverable = clip(c.Deliverable)
			c.Command = clip(c.Command)
			c.Expect = clip(c.Expect)
			c.Source = clip(c.Source)
			c.Assert = clip(c.Assert)
			if c.Command == "" && c.Assert == "" {
				continue
			}
			key := strings.ToLower(c.Command + "\x00" + c.Expect + "\x00" + c.Assert + "\x00" + c.Source)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
			if len(out) == maxItems {
				return out
			}
		}
	}
	return out
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

// severityOf normalizes a verdict's plan-audit severity. An ABSENT severity (empty —
// the common weak-model omission) → warn, so a bare continue doesn't burn budget. But a
// PRESENT-yet-unrecognized token (e.g. "blocker", "high") → critical: the member tried to
// signal urgency in non-canonical words, so fail safe toward blocking rather than ignore it.
func severityOf(v Verdict) string {
	switch strings.ToLower(strings.TrimSpace(v.Severity)) {
	case SeverityCritical:
		return SeverityCritical
	case SeverityWarn, "": // empty/absent → warn (permissive: don't block on an omitted field)
		return SeverityWarn
	case SeverityInfo:
		return SeverityInfo
	default:
		return SeverityCritical // present but unrecognized → block (fail safe)
	}
}

// HasCriticalRevision reports whether any member raised a BLOCKING concern at the
// plan gate — a continue vote at critical severity. Only these block; warn/info
// revisions are advisory. (A single critical vetoes, so a real plan flaw one member
// catches still stops the agent.)
func HasCriticalRevision(vs []Verdict) bool {
	for _, v := range vs {
		if v.Decision == Continue && severityOf(v) == SeverityCritical {
			return true
		}
	}
	return false
}

// CriticalFeedback merges the feedback of the members that raised a critical,
// blocking concern (what a re-plan must fix). "" if none.
func CriticalFeedback(vs []Verdict) string {
	return mergeFeedback(vs,
		func(v Verdict) bool { return v.Decision == Continue && severityOf(v) == SeverityCritical },
		"The plan has a blocking flaw. Revise it to address:")
}

// AdvisoryFeedback merges the non-blocking (warn/info) revise feedback into one
// advisory note the agent should heed during execution. "" if none.
func AdvisoryFeedback(vs []Verdict) string {
	return mergeFeedback(vs,
		func(v Verdict) bool { return v.Decision == Continue && severityOf(v) != SeverityCritical },
		"Non-blocking review advice — incorporate where it improves the work:")
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
