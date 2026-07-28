package app

import (
	"regexp"
	"strings"
)

// Three things stand between a request's exact words and the worker that has to honour them — the
// spec-mine ⟨hard⟩ lines, the curator's `literals`, and the planner's step task — and all three are
// fields a MODEL fills in. There is no deterministic path at all, so one weak reply anywhere in that
// chain drops an identifier and nothing downstream can tell it ever existed. Observed as a brief
// whose `verbatim:` clause was absent entirely: zero literals, on a task whose acceptance turned on
// specific names.
//
// But a request's literals are not a judgement. A fenced block, a backticked span, a quoted string,
// a path — those are LEXICAL facts about the text magi was given, readable without asking anyone.
// So magi reads them itself and puts them under the curator's answer as a floor. The model's list is
// kept and comes first: it knows which literal MATTERS, which this cannot. What changes is that it
// no longer decides alone whether a literal survives.
//
// This is not extra context: these strings were already in the request the curator was shown. It
// only makes the ones that must not be reworded reach the section that says so.

// requestLiteralCap bounds what the floor contributes, and requestLiteralMax bounds each entry. A
// request that pins more than this is one where the brief is not the right carrier anyway, and an
// unbounded floor would push the sections it protects off the end of the window.
const (
	requestLiteralCap = 24
	requestLiteralMax = 200
)

var (
	fenceRe    = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\n(.*?)```")
	tickRe     = regexp.MustCompile("`([^`\n]{2,})`")
	quotedRe   = regexp.MustCompile(`"([^"\n]{3,})"|'([^'\n]{3,})'`)
	pathLikeRe = regexp.MustCompile(`(?:^|[\s(\[{,;:])((?:/|\./|\.\./)?[\w.-]+(?:/[\w.+-]+)+(?:\.[A-Za-z0-9]+)?)`)
)

// requestLiterals reads the spans of text a request pins by writing them out: fenced blocks (line by
// line, because a command is a line), backticked spans, quoted strings, and path-shaped tokens.
//
// Over-collecting is the safe direction. A string that did not need preserving costs a line in a
// section the worker is told not to reword; a string that needed preserving and was dropped costs
// the acceptance. What it must not do is invent — every entry here is a substring of the input.
func requestLiterals(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		// TRAILING only. A leading dot is part of the literal — `./configure`, `../build`, `.env` —
		// and trimming it produced `/configure`, which is not a path that exists anywhere.
		s = strings.TrimRight(s, ".,;:!?")
		if len(s) < 2 || len(s) > requestLiteralMax || seen[s] || len(out) >= requestLiteralCap {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	// Fenced blocks first: a request that shows a command block is pinning that block, and it is the
	// span a paraphrase damages most. Line by line — a worker runs lines, not blocks.
	body := text
	for _, m := range fenceRe.FindAllStringSubmatch(text, -1) {
		for _, ln := range strings.Split(m[1], "\n") {
			add(ln)
		}
		body = strings.Replace(body, m[0], " ", 1) // already harvested; don't re-read it below
	}
	for _, m := range tickRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	for _, m := range quotedRe.FindAllStringSubmatch(body, -1) {
		add(m[1] + m[2]) // exactly one group matched
	}
	for _, m := range pathLikeRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	return out
}

// withRequestLiterals returns the model's literals with the ones magi read out of the request
// appended — the model's first, since it knows which of them matters and this does not.
//
// An entry already covered by one the model listed is skipped in BOTH directions: repeating
// `make world opt` beside `make world` adds nothing but noise to a section whose whole weight comes
// from being short enough to read.
func withRequestLiterals(model []string, sources ...string) []string {
	out := append([]string(nil), model...)
	covered := func(s string) bool {
		for _, m := range out {
			if strings.Contains(m, s) || strings.Contains(s, m) {
				return true
			}
		}
		return false
	}
	for _, src := range sources {
		for _, lit := range requestLiterals(src) {
			if len(out) >= len(model)+requestLiteralCap {
				return out
			}
			if !covered(lit) {
				out = append(out, lit)
			}
		}
	}
	return out
}
