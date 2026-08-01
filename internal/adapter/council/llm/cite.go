package llm

import (
	"os"
	"regexp"
	"strings"
	"unicode"
)

// A member's verdict is only worth what its grounds are worth, and the grounds arrive as prose.
// Measured live (cancel-async-tasks, 2026-08-01, 3-0 done on work that failed its test): a member
// judging through the "verification" lens wrote
//
//	"…as verified by the test output showing 'Cleanup code ran!' when interrupted"
//
// and that string appears nowhere in the record it was shown. It is in the BODY of a script the
// agent wrote and never ran — the actual output of that call was a sentence the agent printed
// claiming success. The evidence block was neither starved nor mislabelled; it separates each
// command from its output. The member read the script as its own result.
//
// Prose cannot fix this. The prompt already tells members that a restated claim with no tool
// result behind it is a claim and not proof, and this went through anyway. What is new here is
// that the claim is made CHECKABLE: a member that rests its verdict on something it saw must
// quote the record, and magi looks the quote up. A quotation that is not in the record is not a
// weak argument, it is an observation that did not happen — and unlike a judgement about
// reasoning, that is a substring test magi can run itself.
//
// Two things are checked, both against the exact text the member was shown:
//
//   - `cite`, the field the member fills with the fragment its verdict rests on. Optional, and
//     the token NO-EVIDENCE says plainly that the verdict rests on the report's substance rather
//     than on anything observed — many turns legitimately produce nothing to quote, and demanding
//     a quote from them is the reflexive over-demand this council has been burned by before.
//   - every QUOTED span in the rationale and the feedback. This is the one that catches the case
//     above: a member that invents an observation tends to quote it, and a quotation is a claim
//     about the record's literal contents.
//
// A failure is not a vote against the agent. It is a re-ask naming the miss, and then an abstain
// if the member stands by grounds that are not there — the member's lens genuinely cannot judge
// from what it has, which is what abstain means.

// citeNoEvidence is what a member sends when its verdict rests on the report's substance rather
// than on anything it observed. Recorded rather than punished: a done that names nothing observed
// is a fact about that verdict, and one a reader should be able to see.
const citeNoEvidence = "NO-EVIDENCE"

// citeMinLen is the shortest quoted span worth checking, in normalized characters. Below it a
// quotation is a word or two — a lens name, a status word, an emphasis — and matching it proves
// nothing either way, while failing it would reject verdicts for their prose style.
const citeMinLen = 12

// citeEnabled gates the whole check. Default on; MAGI_COUNCIL_CITE=0 restores the prior behaviour
// for an A/B.
func citeEnabled() bool {
	v := strings.TrimSpace(os.Getenv("MAGI_COUNCIL_CITE"))
	return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "off")
}

// quotedSpans pulls the quoted fragments out of a member's prose. Backticks are deliberately not
// read: models write identifiers in them, not claims about what they saw.
var quotedSpans = regexp.MustCompile(`"([^"\n]{1,400})"|“([^”\n]{1,400})”`)

// singleQuoted is the same for '…', which needs care English punctuation does not: an apostrophe
// is also a possessive and a contraction. "the tasks' finally blocks … showing 'Cleanup code ran!'"
// has THREE apostrophes, and reading them left to right pairs the possessive with the opening of
// the real quotation — which flags a span the member never claimed and misses the one it did.
// So an opening ' may not follow a word character and a closing ' may not precede one. RE2 has no
// lookaround, so the neighbours are captured and thrown away.
var singleQuoted = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])'([^'\n]{1,400})'(?:$|[^\p{L}\p{N}])`)

// normalizeForCite folds a quote and the record onto one shape so a match is about the CONTENT and
// not about how the record was rendered. The evidence block joins a tool result's lines with a ⏎
// glyph and marks its cuts with …; a member quoting across either would otherwise fail a check it
// should pass. Case is kept — a member that reports FAILED where the record says failed is
// reporting something the record does not say, and this is the one place that distinction is
// cheap to preserve.
func normalizeForCite(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '⏎' || r == '…' || r == '`':
			return ' '
		case r == '\'' || r == '"' || r == '“' || r == '”' || r == '‘' || r == '’':
			return ' '
		case unicode.IsSpace(r):
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// nonCommandRecord is the record with every command BODY cut out, leaving what came BACK: the
// outputs, the report, the task, the plan, the workspace listing.
//
// This is the distinction the live failure turned on, and the reason an existence check alone was
// not enough. "Cleanup code ran!" WAS in the material the member was shown — inside the heredoc of
// a script the agent sent and never ran. Asking "is this text present" answered yes; the honest
// question is "did this come back", and the evidence block already carries the answer in its
// shape: an entry renders as `- tool bash [ok] <command>: exit N ⏎ output: <path> (…) ⏎ <output>`.
// Everything up to the output marker is what was SENT.
//
// An entry with no output marker (a write's "wrote N bytes", a read's contents) is a result
// already and is kept whole. The `commands:` summary line goes too — it is the same command text
// in shorter form, and a quotation matching there is matching a command.
func nonCommandRecord(record string) string {
	const outMark = "⏎ output: "
	var b strings.Builder
	for i, entry := range strings.Split(record, "\n- tool ") {
		if i > 0 {
			if j := strings.Index(entry, outMark); j >= 0 {
				// Cut at the exit status, not at the output marker: the exit code is something
				// that came BACK, and a member grounding a verdict in "exit 1" is grounding it
				// in a result. The marker locates the boundary; the status sits just before it.
				cut := j
				if k := strings.LastIndex(entry[:j], ": exit "); k >= 0 {
					cut = k
				}
				entry = entry[cut:] // drop the command; keep the exit, the path and the output
			}
			b.WriteString("\n")
			b.WriteString(entry)
			continue
		}
		// The head of the block: keep it, minus the commands summary (one logical line that runs
		// until the next blank line, because a heredoc in it spans several physical ones).
		lines := strings.Split(entry, "\n")
		skipping := false
		for _, l := range lines {
			switch {
			case strings.HasPrefix(l, "commands: "):
				skipping = true
			case skipping && strings.TrimSpace(l) == "":
				skipping = false
			}
			if !skipping {
				b.WriteString(l)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// citeMiss is one quotation that is not in the record.
type citeMiss struct {
	quote string // as the member wrote it, for the re-ask
	field string // "cite", "rationale" or "feedback"
	// sentNotReturned distinguishes the two ways a citation fails, because they call for
	// different corrections: text that is nowhere in the record was invented, while text found
	// only inside a command body is real but is the thing that was SENT.
	sentNotReturned bool
}

// checkCites returns every quotation in the verdict that the record does not contain. An empty
// result means nothing the member claimed to be reading back is missing — NOT that the verdict is
// right, only that its quotations are real.
func checkCites(record, cite, rationale, feedback string) []citeMiss {
	rec := normalizeForCite(record)
	came := normalizeForCite(nonCommandRecord(record))
	var out []citeMiss
	// A quoted span in prose only has to EXIST. A member may legitimately quote a command it is
	// pointing at ("`make test` was never run"), and rejecting that would be an over-demand.
	check := func(q, field string) {
		n := normalizeForCite(q)
		if len([]rune(n)) < citeMinLen {
			return // too short to mean anything either way
		}
		if !strings.Contains(rec, n) {
			out = append(out, citeMiss{quote: q, field: field})
		}
	}
	// The cite is the GROUNDS, and grounds are what came back. Text found ONLY inside a command
	// body is what was sent — the case this whole file exists for.
	//
	// "Only" is the word that matters, and getting it wrong is expensive. Measured live
	// (kv-store-grpc, 2026-08-01): two members copied a whole evidence entry verbatim — the
	// command, its exit, its output path — which is the most cooperative thing a member can do,
	// and the first version of this check rejected both because the span STARTS in a command
	// body. They abstained, and a three-member council decided done on one vote. Over-rejection
	// here costs more than the fabrication it guards against.
	//
	// So a cite passes when its TAIL is in what came back, not only when the whole span is. A
	// member that quoted a record line reaches the output at the end of it; a member that quoted
	// only a command reaches more command.
	if c := strings.TrimSpace(cite); c != "" && !strings.EqualFold(c, citeNoEvidence) {
		if n := normalizeForCite(c); len([]rune(n)) >= citeMinLen && !citeHolds(rec, came, c) {
			m := citeMiss{quote: c, field: "cite"}
			// Diagnosed by the same fragments the check uses. Asking whether the WHOLE span is in
			// the record answers no for every elided citation, so an elided quotation of a command
			// body was reported as invented when it is real and merely was not returned — the one
			// distinction the re-ask exists to draw.
			if citeFragmentsPresent(rec, c) {
				m.sentNotReturned = true
			}
			out = append(out, m)
		}
	}
	for _, src := range []struct{ text, field string }{{rationale, "rationale"}, {feedback, "feedback"}} {
		for _, m := range quotedSpans.FindAllStringSubmatch(src.text, -1) {
			check(m[1]+m[2], src.field) // exactly one group matched
		}
		for _, m := range singleQuoted.FindAllStringSubmatch(src.text, -1) {
			check(m[1], src.field)
		}
	}
	return out
}

// citeElision matches an author's own JOIN: the places a member stitched a quotation together
// out of pieces rather than copying one contiguous span. An ellipsis is one ("I cut here" — the
// record uses the single character for its own cuts, a model usually types three dots); a newline
// or an arrow is the other, and it is what a member writes when it quotes a command and then the
// output that came back from it.
//
// Both downgrades this check produced in its first thirty verdicts were this shape, and both were
// wrong. Splitting on the joiner is what makes them one rule instead of two.
var citeElision = regexp.MustCompile(`…|\.\.\.+|\. \. \.|\n|→|=>`)

// citeHolds reports whether a citation is grounded, allowing for the member having ELIDED or
// JOINED the pieces of what it quoted.
//
// Measured live (build-pmars, 2026-08-01): two members cited the same evidence entry. One copied
// it whole and passed. The other wrote
//
//	"pmars -b -r 50 -f …/flashpaper.red …/rave.red | tail -n 1: exit 0 ⏎ output: ... Results: 12 32 6"
//
// — the same entry with the log path cut out and an ellipsis marking the cut, which is the most
// ordinary thing anyone does when quoting — and was downgraded to abstain. The council then split
// 1-1 with one abstention. Its grounds were real; only the shape of the quotation was not a
// contiguous substring.
//
// The second round of the same task produced the same failure in the other shape: a member quoted
// a command, an arrow, and the two lines that came back from it. Every piece was in the record.
// Measured across both waves, those two are the ONLY downgrades this check has produced in thirty
// verdicts, and neither was a fabrication — which is why the rule is about the joiner rather than
// about any one punctuation mark.
//
// So an elided citation is read as what it is: several fragments, each of which must be in the
// record, with the LAST one having to be in what came BACK. That keeps the sent-versus-returned
// distinction this whole file exists for — a member quoting only command bodies still fails,
// because its last fragment is command too — while accepting an honest cut. Fragments too short
// to mean anything are skipped rather than failed, and a citation whose fragments are all short
// falls back to being checked whole.
func citeHolds(rec, came, cite string) bool {
	segs := citeFragments(cite)
	if len(segs) < 2 {
		return citedWhatCameBack(came, normalizeForCite(cite))
	}
	for _, s := range segs[:len(segs)-1] {
		if !strings.Contains(rec, s) {
			return false
		}
	}
	return citedWhatCameBack(came, segs[len(segs)-1])
}

// citeFragments splits a citation at its author's elisions and returns the normalized pieces long
// enough to mean anything.
func citeFragments(cite string) []string {
	var segs []string
	for _, part := range citeElision.Split(cite, -1) {
		if n := normalizeForCite(part); len([]rune(n)) >= citeMinLen {
			segs = append(segs, n)
		}
	}
	return segs
}

// citeFragmentsPresent reports whether every fragment of a citation is somewhere in the record —
// the question behind "this IS here, but only as something that was sent".
func citeFragmentsPresent(rec, cite string) bool {
	segs := citeFragments(cite)
	if len(segs) == 0 {
		return strings.Contains(rec, normalizeForCite(cite))
	}
	for _, s := range segs {
		if !strings.Contains(rec, s) {
			return false
		}
	}
	return true
}

// citeTailLen is how much of a citation's end has to be in what came back. It is long enough that
// a stray word cannot carry a command-body quote through, and short enough that a member quoting
// one output line is not asked to quote more of it.
const citeTailLen = 30

// citedWhatCameBack reports whether a normalized citation reaches material that came back — the
// whole span, or its tail. Both are already normalized.
func citedWhatCameBack(came, n string) bool {
	if strings.Contains(came, n) {
		return true
	}
	r := []rune(n)
	if len(r) <= citeTailLen {
		return false // short spans get no second chance; the whole of it already missed
	}
	return strings.Contains(came, string(r[len(r)-citeTailLen:]))
}

// citeRetryReminder is the one focused re-ask. It names each missing quotation and says the only
// two honest ways out: quote what is actually there, or say there is nothing to quote. It does not
// suggest a vote — a member told which way to vote is not a member.
func citeRetryReminder(misses []citeMiss) string {
	var b strings.Builder
	b.WriteString("\n\n# Your last verdict quoted things that are not in the record\n")
	b.WriteString("Each line below was quoted in your reply. magi searched the exact material you were " +
		"given — the task, the plan, the report, and every tool result above:\n")
	for _, m := range misses {
		why := ""
		if m.sentNotReturned {
			why = "  ← this IS in the record, but only inside a command that was sent; it is not in anything that came back"
		}
		b.WriteString("- in `" + m.field + "`: " + quoteFragment(m.quote) + why + "\n")
	}
	b.WriteString("A command's TEXT is not its output. A script that prints something is not a run of that " +
		"script, and the body of a heredoc above a tool result is the thing that was sent, not the thing " +
		"that came back.\n" +
		"Send your verdict again. Either quote material that is actually in what you were given — copy it " +
		"verbatim into `cite` — or put " + citeNoEvidence + " in `cite` and judge the report on its substance. " +
		"Do not repeat a quotation you cannot find. Your vote is yours; only the grounds have to be real.")
	return b.String()
}

// quoteFragment renders a fragment for the re-ask, short enough that one long quotation cannot
// push the rest of the reminder out of view.
func quoteFragment(s string) string {
	const max = 160
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return "\"" + s + "\""
}
