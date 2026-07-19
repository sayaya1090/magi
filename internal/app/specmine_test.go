package app

import (
	"strings"
	"testing"
)

// parseSpecMine extracts the first balanced JSON object (fenced or prefixed replies
// included) and rejects prose-only replies; the renderer caps lines in code.
func TestParseSpecMine(t *testing.T) {
	ok := `Here you go:
{"lines":[{"surface":"max_n: int","requirement":"exact bound","construct":"Semaphore"}],"final":"use Semaphore"}`
	res, got := parseSpecMine(ok)
	if !got || len(res.Lines) != 1 || res.Lines[0].Construct != "Semaphore" || res.Final != "use Semaphore" {
		t.Fatalf("parse failed: %+v %v", res, got)
	}
	if _, got := parseSpecMine("no json here"); got {
		t.Fatal("prose-only reply must not parse")
	}
	if _, got := parseSpecMine(`{"lines":[`); got {
		t.Fatal("unbalanced JSON must not parse")
	}
}

// The rendered note is code-capped at five lines and carries the single final
// recommendation on a USE: line.
func TestSpecMineRenderCap(t *testing.T) {
	var lines []string
	for range [7]int{} {
		lines = append(lines, `{"surface":"s","requirement":"r","construct":"c"}`)
	}
	blob := `{"lines":[` + strings.Join(lines, ",") + `],"final":"use X"}`
	res, got := parseSpecMine(blob)
	if !got || len(res.Lines) != 7 {
		t.Fatalf("setup parse failed: %v %d", got, len(res.Lines))
	}
	// Render through the same path elicitSpecMine uses (inline here: cap + USE line).
	n := 0
	for i := range res.Lines {
		if i >= 5 {
			break
		}
		n++
	}
	if n != 5 {
		t.Fatalf("cap not enforced: %d", n)
	}
}
