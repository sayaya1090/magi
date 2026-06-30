// Package change reconstructs a compact, human-readable view of a file edit from either
// a tool call's arguments (EditDiff) or a before/after content pair (LineDiff). It is the
// shared source for the TUI's tool-result diff blocks and the council's evidence of what
// the agent actually changed this turn — derived from the agent's own edits, not git, so
// it never mis-attributes a human/external/bash change to the agent.
package change

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxDiffLines caps a single diff so one large edit can't dominate the view/evidence.
const maxDiffLines = 40

// maxDiffInputLines bounds the LCS inputs: the diff is O(n·m) in lines, so a many-line
// file (e.g. a minified or generated file read up to the byte cap) would allocate a huge
// matrix. Past this we summarize instead of diffing.
const maxDiffInputLines = 1000

// EditDiff renders a write/edit tool call (by its JSON args) as a unified diff: an edit
// shows old→new; a write shows its content as added lines. "" for other tools or bad args.
func EditDiff(name, args string) string {
	switch name {
	case "edit":
		var a struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if json.Unmarshal([]byte(args), &a) != nil || (a.Old == "" && a.New == "") {
			return ""
		}
		return LineDiff(a.Old, a.New)
	case "write":
		var a struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(args), &a) != nil || a.Content == "" {
			return ""
		}
		lines := strings.Split(strings.TrimRight(a.Content, "\n"), "\n")
		return clampDiff(prefixLines("+", lines))
	}
	return ""
}

// LineDiff returns a unified line diff (context " ", removals "-", additions "+") of two
// texts, capped via clampDiff. It's an LCS diff (O(n·m)); callers feed bounded inputs.
func LineDiff(oldStr, newStr string) string {
	if oldStr == newStr {
		return "" // no change — don't emit a (possibly "large change") summary for a no-op rewrite
	}
	o := strings.Split(strings.TrimRight(oldStr, "\n"), "\n")
	n := strings.Split(strings.TrimRight(newStr, "\n"), "\n")
	la, lb := len(o), len(n)
	if la > maxDiffInputLines || lb > maxDiffInputLines {
		return fmt.Sprintf("≈ large change: %d → %d lines (diff omitted)", la, lb)
	}
	// lcs[i][j] = length of the longest common subsequence of o[i:] and n[j:].
	lcs := make([][]int, la+1)
	for i := range lcs {
		lcs[i] = make([]int, lb+1)
	}
	for i := la - 1; i >= 0; i-- {
		for j := lb - 1; j >= 0; j-- {
			if o[i] == n[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < la && j < lb {
		switch {
		case o[i] == n[j]:
			out = append(out, " "+o[i])
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "-"+o[i])
			i++
		default:
			out = append(out, "+"+n[j])
			j++
		}
	}
	for ; i < la; i++ {
		out = append(out, "-"+o[i])
	}
	for ; j < lb; j++ {
		out = append(out, "+"+n[j])
	}
	return clampDiff(out)
}

// clampDiff joins diff lines, truncating past maxDiffLines with a summary note.
func clampDiff(lines []string) string {
	if len(lines) > maxDiffLines {
		more := len(lines) - maxDiffLines
		lines = append(lines[:maxDiffLines:maxDiffLines], fmt.Sprintf("… (%d more lines)", more))
	}
	return strings.Join(lines, "\n")
}

func prefixLines(p string, lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = p + ln
	}
	return out
}
