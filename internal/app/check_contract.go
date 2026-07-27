package app

import (
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The acceptance-check contract, as the EXECUTING agent sees it.
//
// The plan-audit council authors per-step deliverable checks and the gates run them, but for a long
// time only ONE execution path ever showed them to whoever does the work: the delegated worker, via
// workerChecklist in its brief. The solo path — the default, since delegate is off by default — got
// nothing. A check like "the gate reads /tmp/build.log and requires exit=0" then demands an artifact
// nobody ever asked the agent to produce: it never writes the file, the check legitimately fails on
// a missing source, and the box stays unticked while the step advances anyway.
//
// So both paths render from the rules below rather than each spelling out their own obligation —
// the same "one source" discipline the tool-allowlist advertising fixes landed on. They differ only
// in scope (one step vs the whole plan) and in whether a repair tool is reachable.

// checkContractRules states what an acceptance check obliges the executing agent to DO. allowSub
// says whether this agent can reach substitute_check; when it cannot, the repair route is an
// explicit report instead of a tool call it would only be refused for calling.
func checkContractRules(allowSub bool) string {
	b := strings.Builder{}
	b.WriteString("YOU RUN, THE CHECK READS. You never run these items — the gate reads the named file itself and " +
		"applies the assertion, with no shell involved. What each item obliges YOU to do is produce that file as the " +
		"REAL output of the work: actually run the command, actually start the server, and redirect the genuine " +
		"output to exactly the path the item names. Never hand-write the file, and never write in the text the " +
		"assertion looks for — a check reading a fabricated record passes while the deliverable is broken, and " +
		"whoever uses it is not fooled.\n")
	if allowSub {
		b.WriteString("IF AN ITEM NAMES THE WRONG PATH — you recorded the real output somewhere else, or the file it " +
			"names is one nothing in this task produces (the CHECK is wrong, NOT the deliverable) — do not quietly " +
			"leave it unmet: call the substitute_check tool (step; original = the item as given; source = the path " +
			"you actually recorded the real output to; assert = the assertion that proves the same goal; reason = why " +
			"the given one cannot be met). A silent workaround is LOST — substitute_check is what gets the fix " +
			"reviewed by the council and kept for the rest of the run. Only if an item's GOAL genuinely cannot be met " +
			"(a real blocker, not a check you can repair) do you stop and report status blocked/failed, naming WHICH " +
			"item is unmet and WHY, so it can be re-planned rather than silently dropped:")
		return b.String()
	}
	b.WriteString("IF AN ITEM NAMES THE WRONG PATH — you recorded the real output somewhere else, or the file it " +
		"names is one nothing in this task produces (the CHECK is wrong, NOT the deliverable) — do not quietly leave " +
		"it unmet: say so in your reply, naming the item, the path you actually recorded the real output to, and the " +
		"assertion that would prove the same goal. And if an item's GOAL genuinely cannot be met, report status " +
		"blocked/failed naming WHICH item is unmet and WHY, so it can be re-planned rather than silently dropped:")
	return b.String()
}

// checkItemObligation renders one check as what the agent must make true. A check with no
// assertion is stated plainly as ungated rather than printed as an empty obligation: an item the
// agent cannot act on is worse than none, and knowing it gates nothing is what lets it be replaced
// with a real one.
func checkItemObligation(c council.DeliverableCheck, allowSub bool) string {
	as := strings.TrimSpace(c.Assert)
	if as == "" {
		if allowSub {
			return "this item carries no assertion, so the gate cannot verify it — if it matters, " +
				"call substitute_check with a source path and an assertion that proves it"
		}
		return "this item carries no assertion, so the gate cannot verify it — if it matters, say which " +
			"file and assertion would prove it"
	}
	if src := strings.TrimSpace(c.Source); src != "" {
		return "the gate will read " + src + " and require: " + as
	}
	return "the gate will require: " + as
}

// workerChecklist renders the plan-audit's executable deliverable checks as an explicit acceptance
// checklist the delegated worker must satisfy before reporting done. Checks tagged for THIS step
// (by leading step number) are preferred; when none are tagged, all of the turn's checks are shown,
// since over-informing beats letting the worker silently skip a requirement. Empty when no checks
// were derived. A delegated worker always holds substitute_check (curateBaseTools grants it
// unconditionally), so the repair route is the tool.
func workerChecklist(checks []council.DeliverableCheck, stepIdx int) string {
	mine := stepChecks(checks, stepIdx)
	if len(mine) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Acceptance checklist — these are the assertions the gate will make about YOUR step before it " +
		"counts as done. Read them as the definition of done, and do not report done while any of them would fail.\n")
	b.WriteString(checkContractRules(true))
	for i, c := range mine {
		fmt.Fprintf(&b, "\n%d. ", i+1)
		if d := strings.TrimSpace(c.Deliverable); d != "" {
			b.WriteString(d + " — ")
		}
		b.WriteString(checkItemObligation(c, true))
	}
	return b.String()
}

// soloCheckContract renders the SAME contract for an agent working the plan itself, where there is
// no brief to carry a checklist: the whole turn's checks, each labeled with the step it gates, for
// the volatile context. Step labels are printed verbatim (the council writes "3" or a title), so
// the agent can line each item up with the plan it is already shown. Empty when no checks exist.
func soloCheckContract(checks []council.DeliverableCheck, agent AgentSpec) string {
	if len(checks) == 0 {
		return ""
	}
	allowSub := agent.allows("substitute_check")
	var b strings.Builder
	b.WriteString("\n\n# Acceptance checks\nThese are the assertions the completion gate will make about this work " +
		"before the turn can finish. Read them as the definition of done: each names an artifact YOUR work must " +
		"leave behind, and a step is not finished while its item would fail.\n")
	b.WriteString(checkContractRules(allowSub))
	for i, c := range checks {
		fmt.Fprintf(&b, "\n%d. ", i+1)
		if s := strings.TrimSpace(c.Step); s != "" {
			b.WriteString("step " + s + " — ")
		}
		if d := strings.TrimSpace(c.Deliverable); d != "" {
			b.WriteString(d + " — ")
		}
		b.WriteString(checkItemObligation(c, allowSub))
	}
	return b.String()
}
