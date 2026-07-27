package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A note saying "this name matched nothing" and a plan that uses that name are BOTH in the planner's
// window, and putting them there is all magi did: the reconciling was left to the model. What that
// is worth on its own was measured on the run this pass was written for — the plan named the symbol
// through three council revisions, every check was built on it, and none of them could ever pass.
//
// The model is not ignoring the note. A replay of that exact call, with the affirmative mention
// stripped so the absence note was the only thing speaking about the name, produced a step titled
// "… (<name> likely renamed or in <dir>/*.c)": it ABSORBED the note, doubted the name in writing,
// and then planned around the dead name anyway — because the planner is a single tool-free call and
// cannot settle the question it just raised.
//
// magi can settle it, because the two sides are not peers. The absence is magi's own record of a
// call it granted, paired from the call's arguments and its own result (specmine_negatives.go); the
// naming is model prose from the mining pass. What magi is missing is only the SCOPE — the miss was
// recorded under whatever path the explorer happened to search, which may have been too narrow —
// and the real name. Both are one read-only grep away.
//
// So this asks, using the same read-only spawn the exploration already uses. The difference that
// matters is not the tools but the QUESTION: "find the real things the plan builds on" is an open
// survey with no stopping condition, which is why the loop guard cuts it off and its prose has to be
// thrown away; "does this name exist, and if not what is the nearest real one" ends when it is
// answered.
//
// The reply is split by provenance rather than trusted whole. The confirming child's OWN search
// record decides present-or-absent (searchOutcomesIn again, on the child's log); its prose supplies
// only a SUGGESTED name, labelled as such in the note, because handing over a confidently wrong
// name is the exact failure the negatives salvage exists to prevent.

// specMineConfirmEnabled gates the contradiction check (MAGI_SPECMINE_CONFIRM, default ON). Off
// restores the prior behavior: the absence and the plan that contradicts it are both injected and
// nothing reconciles them.
func specMineConfirmEnabled() bool { return !envOff("MAGI_SPECMINE_CONFIRM") }

// confirmCap bounds how many doubted names one plan sends to confirmation. A plan resting on a dozen
// invented names is not a scoping problem to be solved one grep at a time — the first few settled
// names already tell the planner the shape of its mistake, and the council rounds that follow are
// where the rest gets caught.
const confirmCap = 3

// confirmSystem instructs the confirming child. It is deliberately narrower than the exploration's:
// there is exactly one question per name, and the reply shape is fixed, so there is nothing to
// survey and no reason to keep going once every name has a line.
const confirmSystem = "You are a READ-ONLY repository searcher. You are given names that an earlier search did not " +
	"find, and your ONLY job is to settle whether each one really is absent and, if it is, what real name was " +
	"probably meant. Use grep/glob/read. Search the WHOLE workspace for each name first — the earlier search may " +
	"have looked under too narrow a path. Do not propose changes, do not write anything, do not comment on the " +
	"plan. Report ONLY what your own searches returned: if you did not find a nearest real name, say so rather " +
	"than inventing one, because a wrong name here is worse than no name — the steps that read your answer are " +
	"told to reuse it verbatim. Stop as soon as every name has a line."

// planText adapts a plan to the claim text the detector reads. It is ONE adapter, not the detector's
// input type, deliberately: the same contradiction exists wherever a reader of the absence note goes
// on to use a missed name — a council member's criteria, a worker's report — and those seams differ
// only in how their claim is rendered to a string. Keeping the claim a string is what lets the next
// one be an adapter instead of a copy of contradictedNames.
//
// renderSteps is the human view and omits a delegate's `task` and a parallel group's `question`,
// which is precisely where a long instruction carrying an invented symbol lives — so this reads the
// fields directly.
func planText(steps []planStep) string {
	var b strings.Builder
	for _, st := range steps {
		for _, f := range []string{st.Title, st.Agent, st.Discover, st.Each, st.Task} {
			b.WriteString(f)
			b.WriteString("\n")
		}
		for _, g := range st.Groups {
			b.WriteString(g.Agent + "\n" + g.Focus + "\n" + g.Question + "\n")
		}
	}
	return b.String()
}

// plainName reports whether a grep pattern is a bare name — the only shape this pass reasons about.
// A pattern carrying regex structure is not a symbol the plan can "use", and treating one as a
// literal would let `.` or `[` match text that was never searched for. The length floor keeps common
// words out: a three-letter pattern appearing somewhere in a plan's prose is not evidence the plan
// depends on a symbol by that name.
func plainName(s string) bool {
	if len(s) < 4 {
		return false
	}
	letter := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letter = true
		case r >= '0' && r <= '9', r == '_', r == '.', r == '/', r == '-':
		default:
			return false
		}
	}
	return letter
}

// expandAlternation splits a miss recorded for `A|B|C` into its alternatives. The inference is sound
// in this one direction only: the miss exists because the whole alternation matched NOTHING, which
// means every alternative individually matched nothing too. (The reverse would not hold — a HIT says
// only that some alternative matched.) One non-name part and the whole pattern is refused, since a
// pattern with real regex structure cannot be taken apart this way.
func expandAlternation(pat string) []string {
	parts := strings.Split(pat, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !plainName(p) {
			return nil
		}
		out = append(out, p)
	}
	return out
}

// contradictedNames is the detector: the searched-and-not-found names a claim goes on to use anyway.
// Containment is the whole test, and it is enough for this class — the disputed thing is a token (a
// symbol, a path, a file name), and the claim either writes that token or it does not. It does NOT
// generalize to a claim whose APPROACH contradicts a finding; nothing here claims it does.
//
// The claim is a string rather than a plan so this stays the one detector for every seam that reads
// the absence note.
func contradictedNames(claim string, ms []searchMiss) []searchMiss {
	txt := claim
	if strings.TrimSpace(txt) == "" {
		return nil
	}
	var out []searchMiss
	seen := map[string]bool{}
	for _, m := range ms {
		for _, name := range expandAlternation(m.pattern) {
			if seen[name] || !strings.Contains(txt, name) {
				continue
			}
			seen[name] = true
			out = append(out, searchMiss{pattern: name, scope: m.scope})
			if len(out) >= confirmCap {
				return out
			}
		}
	}
	return out
}

// confirmBrief states the closed question. It names the consequence rather than just the rule,
// because the model cannot otherwise know why an absent name matters more here than elsewhere.
func confirmBrief(ms []searchMiss) string {
	var b strings.Builder
	b.WriteString("── NAMES TO SETTLE\nEach name below was searched for during an earlier read-only pass and " +
		"matched NOTHING, yet the plan about to run uses it anyway. One of those two is wrong. Settle each " +
		"name, and nothing else.\n\n")
	for _, m := range ms {
		b.WriteString("- `" + m.pattern + "` — the earlier search covered " + m.scope + "\n")
	}
	b.WriteString("\nFor EACH name: grep the WHOLE workspace for it (no path filter). If it is not there, " +
		"search for the nearest thing that really exists and was probably meant instead.\n\n" +
		"Reply with ONE line per name and nothing else:\n" +
		"  `<name>` — PRESENT at <path>\n" +
		"  `<name>` — ABSENT; closest existing: `<real name>` (<path>)\n" +
		"  `<name>` — ABSENT; closest existing: none found\n")
	return b.String()
}

// suggestedFor pulls the replacement the confirming child proposed for one name out of its reply.
// Lenient by design: the suggestion is garnish on a verdict that was already decided by the child's
// own search record, so an unparseable line costs a hint, not the finding.
func suggestedFor(reply, name string) string {
	for _, ln := range strings.Split(reply, "\n") {
		if !strings.Contains(ln, name) {
			continue
		}
		i := strings.Index(strings.ToLower(ln), "closest existing:")
		if i < 0 {
			continue
		}
		s := strings.TrimSpace(ln[i+len("closest existing:"):])
		if s == "" || strings.HasPrefix(strings.ToLower(s), "none") {
			return ""
		}
		return clipSpec(s, 160)
	}
	return ""
}

// confirmContradictions settles the names the plan uses that magi's own record says were searched
// for and not found. It returns the note to inject and the set of patterns whose absence was
// RETRACTED — those must be dropped from the miss list before it is rendered, or the same note would
// tell the planner both that a name is missing and that it is there.
//
// Every failure path returns nothing rather than a guess: the flag is off, the plan uses none of the
// missed names, the spawn was refused or budget-exhausted, the child never searched. A confirmation
// that did not happen must read as "no new information", never as a verdict.
func (a *App) confirmContradictions(ctx context.Context, s session.Session, depth int, spec AgentSpec,
	claim string, ms []searchMiss) (string, map[string]bool) {
	if !specMineConfirmEnabled() {
		return "", nil
	}
	doubted := contradictedNames(claim, ms)
	if len(doubted) == 0 {
		return "", nil
	}
	names := make([]string, len(doubted))
	for i, m := range doubted {
		names[i] = m.pattern
	}
	a.emitToolProgress(s.ID, plannerActor, "", "specmine", fmt.Sprintf(
		"spec-mine: the plan uses %d name(s) an earlier search did not find (%s) — settling them with a "+
			"second read-only search rather than leaving the contradiction for the planner to reconcile",
		len(doubted), strings.Join(names, ", ")))

	r := a.spawnResolved(ctx, s, depth, spec, port.SpawnRequest{Agent: "specmine", Prompt: confirmBrief(doubted)})
	if r.Err != "" {
		a.emitToolProgress(s.ID, plannerActor, "", "specmine",
			"spec-mine: the confirming search could not run ("+clipLine(r.Err, 120)+") — leaving the earlier "+
				"absences as they stand")
		return "", nil
	}
	// The verdict comes from the child's OWN record, not its reply: a stopped or rambling child still
	// leaves a truthful trail of what it searched and what came back. Its prose is read only for the
	// suggested name, which the note marks as unverified.
	childMisses, childHit := searchOutcomesIn(a.readEventsBestEffort(ctx, r.SessionID))
	missed := map[string]bool{}
	for _, m := range childMisses {
		missed[m.pattern] = true
	}
	reply := stripReportStatus(r.Text)

	retracted := map[string]bool{}
	var confirmed, kept []string
	for _, m := range doubted {
		switch {
		case childHit[m.pattern]:
			retracted[m.pattern] = true
		case missed[m.pattern]:
			line := "- `" + m.pattern + "` — CONFIRMED ABSENT: searched again across the whole workspace and " +
				"still no match. Do not plan a step or write a check around it; either a step must CREATE it, " +
				"or use the real name below."
			if sug := suggestedFor(reply, m.pattern); sug != "" {
				line += "\n  Probably meant instead: " + sug + " — this is the confirming pass's reading, NOT a " +
					"verified fact; open it and check before you depend on it."
			}
			confirmed = append(confirmed, line)
		default:
			kept = append(kept, m.pattern) // never searched → the earlier miss stands, unwidened
		}
	}

	a.emitToolProgress(s.ID, plannerActor, "", "specmine", fmt.Sprintf(
		"spec-mine: settled — %d confirmed absent, %d retracted (the earlier search had looked too narrowly), "+
			"%d unsettled", len(confirmed), len(retracted), len(kept)))

	var b strings.Builder
	if len(confirmed) > 0 {
		b.WriteString("# Names the plan uses that DO NOT EXIST — settled by a second read-only search of the " +
			"whole workspace, from magi's own record of what that search returned:\n" +
			strings.Join(confirmed, "\n"))
	}
	// A retraction is reported even though nothing downstream depends on it: the earlier absence went
	// into the same note stream, and a reader that saw it must be told it was wrong.
	if len(retracted) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		var back []string // walk `doubted`, not the map: the note must read the same way twice
		for _, m := range doubted {
			if retracted[m.pattern] {
				back = append(back, "`"+m.pattern+"`")
			}
		}
		b.WriteString("# Correction — a name reported above as not found DOES exist: " + strings.Join(back, ", ") +
			". The earlier search had looked under too narrow a path. Use it normally.")
	}
	return strings.TrimSpace(b.String()), retracted
}

// dropRetracted removes the misses a confirming search disproved, so the absence note never carries
// a name the very same injection corrects.
func dropRetracted(ms []searchMiss, retracted map[string]bool) []searchMiss {
	if len(retracted) == 0 {
		return ms
	}
	out := ms[:0]
	for _, m := range ms {
		if !retracted[m.pattern] {
			out = append(out, m)
		}
	}
	return out
}
