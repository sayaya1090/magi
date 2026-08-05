package council

import "testing"

// The rule a user writes in config.toml is a raw string, and these are the strings the
// manual documents: unanimous | majority | quorum:k | weighted:θ | veto:Name.
//
// Every other test in this package builds its rule from the Rule constants, so all of them
// would keep passing if a constant's VALUE changed — and Tally now switches on those same
// constants, so the change would sail through while every existing config silently fell
// through to majority (parsing is deliberately forgiving and reports nothing). This is the
// one test that spells the config vocabulary out, so the constants stay pinned to it.
func TestTheDocumentedConfigStringsStillSelectTheirRule(t *testing.T) {
	done := []Verdict{{Decision: Done}, {Decision: Done}}
	split := []Verdict{{Decision: Done}, {Decision: Continue}}

	cases := []struct {
		rule string // exactly what a user types
		vs   []Verdict
		want Decision
	}{
		// unanimous: two done finishes, a split does not.
		{"unanimous", done, Done},
		{"unanimous", split, Continue},
		// quorum:k counts done votes, so a 1-1 split satisfies k=1 where unanimous did not.
		{"quorum:1", split, Done},
		{"quorum:2", split, Continue},
		// weighted:θ on the done share — a 1-1 split is exactly 0.5.
		{"weighted:0.5", split, Done},
		{"weighted:0.9", split, Continue},
		// veto:Name — the named member voting non-done forces continue.
		{"veto:Casper", []Verdict{{Member: "Melchior", Decision: Done}, {Member: "Casper", Decision: Continue}}, Continue},
		{"veto:Casper", done, Done},
		// majority, and the empty rule that means it.
		{"majority", done, Done},
		{"majority", split, Continue},
		{"", done, Done},
	}
	for _, c := range cases {
		if got, _ := Tally(c.vs, Rule(c.rule)); got != c.want {
			t.Errorf("rule %q: Tally = %v, want %v — this string no longer reaches the rule it names, "+
				"so a config using it falls through to majority with no error", c.rule, got, c.want)
		}
	}
}
