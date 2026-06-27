package council

import "strings"

// Rule names a consensus rule, optionally with a parameter after a colon:
//
//	"unanimous"          every voter must say done
//	"majority"           done is a strict majority of non-abstaining voters (default)
//	"quorum:2"           at least k members voted done
//	"weighted:0.6"       done's weight share meets threshold θ
//	"veto:Balthasar"     any listed member voting non-done forces continue, else majority
//
// The empty Rule means majority. Parsing is forgiving: a bad parameter falls back
// to that rule's default rather than erroring (the council is configuration, and a
// safe default beats a hard failure at the termination gate).
type Rule string

const (
	RuleUnanimous Rule = "unanimous"
	RuleMajority  Rule = "majority"
	RuleQuorum    Rule = "quorum"   // use "quorum:k"
	RuleWeighted  Rule = "weighted" // use "weighted:θ"
	RuleVeto      Rule = "veto"     // use "veto:Name[,Name...]"
)

// DefaultRule is applied when no rule is configured.
const DefaultRule = RuleMajority

// parse splits a rule into its name and (optional) parameter. The name is
// lower-cased so "Majority" and "majority" are equivalent.
func (r Rule) parse() (name, param string) {
	s := strings.TrimSpace(string(r))
	if s == "" {
		return "majority", ""
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return strings.ToLower(strings.TrimSpace(s[:i])), strings.TrimSpace(s[i+1:])
	}
	return strings.ToLower(s), ""
}
