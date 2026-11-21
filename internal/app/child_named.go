package app

import "regexp"

// childLine matches the start of a line the core writes for one child: "[child s_…" then ", " or
// "]". Only at a line's start, so a tool that merely quotes such a line mid-sentence names nobody.
var childLine = regexp.MustCompile(`(?m)^\[child (s_[0-9A-Za-z_-]+)[,\]]`)

// ChildrenNamed returns the child sessions a tool result names, in order and each once. The core ends
// the result of a call that ran children with one line per child, "[child <id>, …]": childAccount for
// a spawned subagent, openChildAccount for a child a tool drives. Screens read it to open a child from
// the row of the call that ran it. A line with "(no session)" names nothing.
func ChildrenNamed(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range childLine.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
