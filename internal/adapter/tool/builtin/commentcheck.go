package builtin

import (
	"regexp"
	"strings"
)

// A recurring weak-model tic is to pad an edit with comments that narrate the
// change itself ("// I've updated this to...") or stand in for missing work
// ("// rest of the code unchanged", "// ... your implementation here"). These add
// no durable value — they address the reviewer, not the next reader, and the
// placeholder ones are often a sign the model elided code it was supposed to
// write. commentNoiseAdvisory flags such lines in freshly-added text and returns a
// one-line, non-blocking note appended to the tool result. It is deliberately
// advisory: a false positive costs one line, so the patterns stay high-signal and
// leave ordinary intent-explaining comments (and bare TODOs) alone.

// noiseCommentRe matches the comment BODY (marker already stripped). Two families:
// reviewer-narration openers, and placeholder/elision phrasing.
var noiseCommentRe = regexp.MustCompile(`(?i)^(i've|i have|i |we've|we |here's|here is|now (we|i|you|the)|let's|added |removed |changed |updated |renamed |refactored |note:|as requested|per your|this (change|edit|fix|update|now|function|method|line))` +
	`|(rest of (the )?(code|file|class|function|method|implementation)|your (code|implementation) (goes )?here|implementation (goes )?here|code (goes )?here|unchanged|as before|remains the same|no changes? (here|needed|required)|omitted for brevity|same as (above|before)|\.\.\.\s*$|…\s*$)`)

// commentBody returns the text of a single-line comment (marker stripped) and
// whether the line is a comment at all. Language-agnostic: it recognizes the
// common leading markers rather than parsing any one grammar, which is enough for
// an advisory signal.
func commentBody(line string) (string, bool) {
	s := strings.TrimSpace(line)
	for _, m := range []string{"//", "/*", "<!--", "--", "#", ";;", ";", "%", "*"} {
		if strings.HasPrefix(s, m) {
			return strings.TrimSpace(strings.TrimLeft(s[len(m):], "/*-!<> \t")), true
		}
	}
	return "", false
}

// commentNoiseAdvisory scans lines added by an edit (present in added, not in
// prior) and returns a trailing advisory naming up to two offending comments, or
// "" when nothing looks like noise.
func commentNoiseAdvisory(added, prior string) string {
	var flagged []string
	for _, raw := range strings.Split(added, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.Contains(prior, line) {
			continue // unchanged or pre-existing line
		}
		body, ok := commentBody(line)
		if !ok || body == "" {
			continue
		}
		if noiseCommentRe.MatchString(body) {
			flagged = append(flagged, clipRef(line, 60))
			if len(flagged) == 2 {
				break
			}
		}
	}
	if len(flagged) == 0 {
		return ""
	}
	return "\nnote: added comment(s) read like change-narration or placeholders — " +
		strings.Join(flagged, " / ") +
		" — comments should capture non-obvious intent, not restate the change or code; consider removing them."
}
