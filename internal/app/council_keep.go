package app

import (
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
)

// Council feedback is fix-only by construction: a member names the gap, never the parts that are
// already right. Wherever that feedback drives a REWRITE — a re-plan, a contract consolidation, a
// re-declared substitution — the writer is therefore told what to change and nothing about what to
// preserve, and it is free to satisfy the new demand by discarding work an earlier round had just
// gained. `keep` is the other half: each member names, through its own lens, what must survive.
//
// It is advisory in BOTH directions and that is deliberate. A member emits it regardless of how it
// voted (an approving member's blessing is exactly what a rewrite triggered by SOMEONE ELSE's flaw
// would otherwise lose), and the writer may still change a kept item when the fix genuinely
// requires it — a keep that hardened into a constraint would block the very fix it accompanies.

// keepAdvice renders the members' `keep` as the advisory block that rides along with an
// instruction, or "" when the flag is off or no member named anything. Callers append it to the
// feedback they were already sending, so with the flag off the instruction is byte-identical.
func keepAdvice(vs []council.Verdict) string {
	if !councilKeepEnabled() {
		return ""
	}
	keep := strings.TrimSpace(council.AggregateKeep(vs))
	if keep == "" {
		return ""
	}
	return "Already sound through some lens — PREFER to preserve these, but this is advice, not a " +
		"rule: change them if the fix truly requires it.\n" + keep
}

// withKeepAdvice appends that block to an instruction. An empty instruction stays empty: keep alone
// is not an instruction, and sending it as one would read as "you are done".
func withKeepAdvice(instruction string, vs []council.Verdict) string {
	instruction = strings.TrimSpace(instruction)
	adv := keepAdvice(vs)
	if instruction == "" || adv == "" {
		return instruction
	}
	return instruction + "\n\n" + adv
}
