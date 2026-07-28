package council

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// v builds a verdict with a member name and decision; weight defaults to 1.
func v(member string, d Decision) Verdict { return Verdict{Member: member, Decision: d} }

// vw builds a weighted verdict.
func vw(member string, d Decision, w float64) Verdict {
	return Verdict{Member: member, Decision: d, Weight: w}
}

func TestTallyMajority(t *testing.T) {
	cases := []struct {
		name string
		vs   []Verdict
		rule Rule
		want Decision
	}{
		// SPEC F-COUNCIL examples.
		{"unanimous-not-all-done", []Verdict{v("a", Done), v("b", Done), v("c", Continue)}, RuleUnanimous, Continue},
		{"unanimous-all-done", []Verdict{v("a", Done), v("b", Done)}, RuleUnanimous, Done},
		{"majority-2of3", []Verdict{v("a", Done), v("b", Done), v("c", Continue)}, RuleMajority, Done},
		{"majority-tie", []Verdict{v("a", Done), v("b", Continue)}, RuleMajority, Continue},
		{"abstain-excluded", []Verdict{v("a", Done), v("b", Abstain), v("c", Continue)}, RuleMajority, Continue}, // 1 of 2 voters → tie
		{"abstain-majority", []Verdict{v("a", Done), v("b", Done), v("c", Abstain)}, RuleMajority, Done},         // 2 of 2 voters
		{"empty-rule-defaults-majority", []Verdict{v("a", Done), v("b", Done), v("c", Continue)}, "", Done},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := Tally(tc.vs, tc.rule)
			if got != tc.want {
				t.Fatalf("Tally(%s) = %q, want %q", tc.rule, got, tc.want)
			}
		})
	}
}

func TestTallyQuorum(t *testing.T) {
	vs := []Verdict{v("a", Done), v("b", Done), v("c", Continue)}
	if got, _ := Tally(vs, "quorum:2"); got != Done {
		t.Fatalf("quorum:2 with 2 done = %q, want done", got)
	}
	if got, _ := Tally(vs, "quorum:3"); got != Continue {
		t.Fatalf("quorum:3 with 2 done = %q, want continue", got)
	}
	// A malformed quorum parameter falls back to k=1, not an error/panic.
	if got, _ := Tally([]Verdict{v("a", Done)}, "quorum:abc"); got != Done {
		t.Fatalf("quorum:abc with 1 done = %q, want done (default k=1)", got)
	}
}

func TestTallyWeighted(t *testing.T) {
	// Two done at weight 2 (=4) vs one continue at weight 1 → share 4/5 = 0.8.
	vs := []Verdict{vw("a", Done, 2), vw("b", Done, 2), vw("c", Continue, 1)}
	if got, _ := Tally(vs, "weighted:0.6"); got != Done {
		t.Fatalf("weighted:0.6 share 0.8 = %q, want done", got)
	}
	if got, _ := Tally(vs, "weighted:0.9"); got != Continue {
		t.Fatalf("weighted:0.9 share 0.8 = %q, want continue", got)
	}
	// A heavy dissenter outweighs two light approvals.
	heavy := []Verdict{vw("a", Done, 1), vw("b", Done, 1), vw("c", Continue, 5)}
	if got, _ := Tally(heavy, "weighted:0.6"); got != Continue {
		t.Fatalf("weighted:0.6 share 2/7 = %q, want continue", got)
	}
}

func TestTallyVeto(t *testing.T) {
	// Balthasar vetoes despite a done majority.
	vs := []Verdict{v("Melchior", Done), v("Casper", Done), v("Balthasar", Continue)}
	if got, _ := Tally(vs, "veto:Balthasar"); got != Continue {
		t.Fatalf("veto:Balthasar continuing = %q, want continue", got)
	}
	// No veto cast → falls back to majority (all done here).
	allDone := []Verdict{v("Melchior", Done), v("Casper", Done), v("Balthasar", Done)}
	if got, _ := Tally(allDone, "veto:Balthasar"); got != Done {
		t.Fatalf("veto:Balthasar all done = %q, want done", got)
	}
	// Veto is case-insensitive.
	if got, _ := Tally(vs, "veto:balthasar"); got != Continue {
		t.Fatalf("veto:balthasar (lowercase) = %q, want continue", got)
	}
	// A non-listed member's continue does not veto; majority still decides.
	other := []Verdict{v("Melchior", Done), v("Casper", Done), v("Balthasar", Continue)}
	if got, _ := Tally(other, "veto:Nobody"); got != Done {
		t.Fatalf("veto:Nobody with 2/3 done = %q, want done (majority)", got)
	}
}

func TestTallyDegenerateParamsStaySafe(t *testing.T) {
	// quorum:0 and weighted:0 must NOT let an all-continue vote finish (they would
	// if Done>=0 / DoneWeight>=0 were taken literally).
	allContinue := []Verdict{v("a", Continue), v("b", Continue)}
	for _, rule := range []Rule{"quorum:0", "quorum:-1", "weighted:0", "weighted:-0.5"} {
		if got, _ := Tally(allContinue, rule); got != Continue {
			t.Fatalf("Tally(all-continue, %q) = %q, want continue", rule, got)
		}
	}
	// A legitimate quorum:1 with one done still finishes.
	if got, _ := Tally([]Verdict{v("a", Done), v("b", Continue)}, "quorum:1"); got != Done {
		t.Fatalf("quorum:1 with 1 done = %q, want done", got)
	}
}

func TestTallyNeverFinishesWithoutAffirmation(t *testing.T) {
	// No voters, all abstain, and unknown rules must all resolve to Continue —
	// the council never stops the loop unless its rule is affirmatively met.
	if got, _ := Tally(nil, RuleMajority); got != Continue {
		t.Fatalf("empty verdicts = %q, want continue", got)
	}
	if got, _ := Tally([]Verdict{v("a", Abstain), v("b", Abstain)}, RuleUnanimous); got != Continue {
		t.Fatalf("all abstain = %q, want continue", got)
	}
	if got, _ := Tally([]Verdict{v("a", Done), v("b", Done)}, "nonsense"); got != Continue {
		t.Fatalf("unknown rule = %q, want continue", got)
	}
}

func TestTallyIsPure(t *testing.T) {
	// Same input twice → identical output (determinism), and the input slice is
	// not mutated.
	vs := []Verdict{v("a", Done), v("b", Continue), v("c", Done)}
	d1, b1 := Tally(vs, RuleMajority)
	d2, b2 := Tally(vs, RuleMajority)
	if d1 != d2 || b1 != b2 {
		t.Fatalf("Tally not deterministic: (%v,%v) vs (%v,%v)", d1, b1, d2, b2)
	}
	if vs[0].Member != "a" || vs[1].Decision != Continue {
		t.Fatalf("Tally mutated its input: %+v", vs)
	}
}

func TestBreakdownCounts(t *testing.T) {
	vs := []Verdict{v("a", Done), v("b", Continue), v("c", Abstain), v("d", Done)}
	_, b := Tally(vs, RuleMajority)
	if b.Done != 2 || b.Continue != 1 || b.Abstain != 1 || b.Voters != 3 {
		t.Fatalf("breakdown = %+v, want done=2 continue=1 abstain=1 voters=3", b)
	}
	if b.Rule != RuleMajority {
		t.Fatalf("breakdown rule = %q, want %q", b.Rule, RuleMajority)
	}
}

func TestAggregateFeedback(t *testing.T) {
	vs := []Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: Continue, Feedback: "handle the nil case"},
		{Member: "Balthasar", Lens: "verification", Decision: Done, Feedback: "ignored — voted done"},
		{Member: "Casper", Lens: "completeness", Decision: Continue, Feedback: "the CLI flag is still missing"},
		{Member: "X", Decision: Continue, Feedback: "   "}, // blank feedback skipped
	}
	got := AggregateFeedback(vs)
	if !strings.Contains(got, "handle the nil case") || !strings.Contains(got, "the CLI flag is still missing") {
		t.Fatalf("aggregate missing continuing feedback:\n%s", got)
	}
	if strings.Contains(got, "ignored") {
		t.Fatalf("aggregate included a done member's feedback:\n%s", got)
	}
	if strings.Contains(got, "Melchior (correctness)") == false {
		t.Fatalf("aggregate missing labeled member:\n%s", got)
	}
	// Nothing to say when no member continues with feedback.
	if AggregateFeedback([]Verdict{v("a", Done), v("b", Done)}) != "" {
		t.Fatalf("expected empty feedback when all done")
	}
}

func TestAggregateKeep(t *testing.T) {
	vs := []Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: Continue, Feedback: "handle nil", Keep: "the parser change is correct"},
		{Member: "Balthasar", Lens: "verification", Decision: Done, Keep: "the tests pass"}, // done member's keep IS included
		{Member: "Casper", Lens: "completeness", Decision: Continue, Keep: "   "},           // blank keep skipped
	}
	got := AggregateKeep(vs)
	if !strings.Contains(got, "the parser change is correct") || !strings.Contains(got, "the tests pass") {
		t.Fatalf("keep aggregate missing entries:\n%s", got)
	}
	if !strings.Contains(got, "do NOT redo or revert") {
		t.Fatalf("keep aggregate missing the don't-revert framing:\n%s", got)
	}
	if !strings.Contains(got, "Melchior (correctness)") {
		t.Fatalf("keep aggregate missing labeled member:\n%s", got)
	}
	// No keep supplied → empty (the MAGI_COUNCIL_KEEP-off case: no member was asked).
	if AggregateKeep([]Verdict{v("a", Continue), v("b", Done)}) != "" {
		t.Fatalf("expected empty keep when none supplied")
	}
}

// A keep points at work the writer is already holding, so a member that transcribes the artifact
// instead of naming it spends the room the critique needs — and, by marking everything as settled,
// says no more than naming nothing would. The cap is per member so one verbose lens cannot crowd
// out the others, and it announces the cut rather than ending mid-sentence.
func TestAggregateKeepCapsOneVerboseMember(t *testing.T) {
	long := strings.Repeat("step one restated at length. ", 60)
	got := AggregateKeep([]Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: Done, Keep: long},
		{Member: "Balthasar", Lens: "verification", Decision: Done, Keep: "step 4 — the round-trip check"},
	})
	if len(got) > 2*keepPerMember {
		t.Errorf("one member's keep was not capped: %d bytes", len(got))
	}
	if !strings.Contains(got, "keep truncated") {
		t.Errorf("a truncated keep must say so, or a cut sentence reads as the whole thought:\n%s", got)
	}
	// The cap is per member: a neighbour's short, useful keep survives intact.
	if !strings.Contains(got, "step 4 — the round-trip check") {
		t.Errorf("a verbose member crowded out a concise one:\n%s", got)
	}
}

// The cut lands on a rune boundary — a keep in a non-ASCII language must not arrive as mojibake.
func TestAggregateKeepClipsOnRuneBoundary(t *testing.T) {
	got := AggregateKeep([]Verdict{{Member: "M", Decision: Done, Keep: strings.Repeat("검증 단계", 200)}})
	if !utf8.ValidString(got) {
		t.Errorf("clipped keep is not valid UTF-8:\n%q", got)
	}
}

// Deliberate populates Keep on a Continue outcome and never on the decision path.
func TestDeliberateCarriesKeep(t *testing.T) {
	d := Deliberate(1, []Verdict{
		{Member: "Melchior", Decision: Continue, Feedback: "fix X", Keep: "module A is done"},
		{Member: "Casper", Decision: Done},
	}, RuleMajority)
	if d.Decision != Continue {
		t.Fatalf("want continue, got %s", d.Decision)
	}
	if !strings.Contains(d.Keep, "module A is done") {
		t.Fatalf("continue should carry keep: %q", d.Keep)
	}
}

func TestDeliberate(t *testing.T) {
	// Continue carries aggregated feedback.
	cont := Deliberate(1, []Verdict{
		{Member: "Melchior", Decision: Continue, Feedback: "fix the test"},
		{Member: "Casper", Decision: Done},
	}, RuleMajority)
	if cont.Decision != Continue {
		t.Fatalf("decision = %q, want continue", cont.Decision)
	}
	if cont.Feedback == "" {
		t.Fatalf("continue deliberation should carry feedback")
	}
	if cont.Round != 1 {
		t.Fatalf("round = %d, want 1", cont.Round)
	}
	// Done carries no feedback.
	done := Deliberate(2, []Verdict{v("a", Done), v("b", Done)}, RuleUnanimous)
	if done.Decision != Done {
		t.Fatalf("decision = %q, want done", done.Decision)
	}
	if done.Feedback != "" {
		t.Fatalf("done deliberation should not carry feedback, got %q", done.Feedback)
	}
}

func TestDefaultMembersAreTheMAGI(t *testing.T) {
	m := DefaultMembers()
	if len(m) != 3 {
		t.Fatalf("got %d members, want 3 (the MAGI)", len(m))
	}
	want := map[string]string{"Melchior": "correctness", "Balthasar": "verification", "Casper": "completeness"}
	for _, mem := range m {
		if want[mem.Name] != mem.Lens {
			t.Fatalf("member %q lens = %q, want %q", mem.Name, mem.Lens, want[mem.Name])
		}
		if _, ok := Lenses[mem.Lens]; !ok {
			t.Fatalf("lens %q has no description in Lenses", mem.Lens)
		}
	}
}
