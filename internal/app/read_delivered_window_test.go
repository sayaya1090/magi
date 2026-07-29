package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The coverage record is a claim about what the agent was SHOWN. The call's offset/limit is a claim
// about what it ASKED for, and the two part company the moment something truncates.
//
// Observed live (fix-ocaml-gc, 2026-07-30): `read{path: /app/ocaml/runtime/major_gc.c}` with no
// window asked for the default 2000 lines; the result came back cut to 49152 bytes — about 1239
// lines — carrying magi's own truncation marker. Recording the request marked lines 1240..2000 as
// delivered when they had never been sent, so a later read of that region would be refused credit
// as a re-read of content the agent had never seen.
func TestTheCoverageWindowIsWhatWasDeliveredNotWhatWasAsked(t *testing.T) {
	// A read result in the tool's own gutter format, cut short of what the call asked for.
	var sb strings.Builder
	for i := 1; i <= 1239; i++ {
		fmt.Fprintf(&sb, "%d\tstatic value caml_alloc_shr(mlsize_t wosize, tag_t tag);\n", i)
	}
	sb.WriteString("\n…[output truncated: showing 49152 of 79198 bytes — re-run with a narrower range]")
	body, _ := json.Marshal(sb.String())

	lo, n, ok := deliveredLineWindow(body)
	if !ok {
		t.Fatal("a numbered read body has a window to count")
	}
	if lo != 1 || n != 1239 {
		t.Fatalf("the delivered window is the gutter's own extent: got offset %d, %d lines", lo, n)
	}

	// What that fixes: the region past the cut is still new information.
	g := newRunGuard()
	if !g.noteReadCoverage("runtime/major_gc.c", lo, n) {
		t.Fatal("the first read of a file is new information")
	}
	if !g.noteReadCoverage("runtime/major_gc.c", 1400, 200) {
		t.Error("lines past the truncation were never delivered — reading them is new information")
	}
	// For contrast, the shape this replaced: booking the request as 2000 lines swallows the 761
	// it never sent, and the read above is refused.
	g2 := newRunGuard()
	g2.noteReadCoverage("runtime/major_gc.c", 1, 2000)
	if g2.noteReadCoverage("runtime/major_gc.c", 1400, 200) {
		t.Error("the contrast is stale: booking the request no longer over-claims")
	}

	// And a re-read INSIDE what was delivered is still refused, which is why the predicate exists.
	if g.noteReadCoverage("runtime/major_gc.c", 600, 200) {
		t.Error("a window already handed over is not new information")
	}
}

// An untruncated read is unchanged by any of this: the gutter and the request agree.
func TestAnUntruncatedReadCountsTheSameEitherWay(t *testing.T) {
	var sb strings.Builder
	for i := 540; i < 740; i++ {
		fmt.Fprintf(&sb, "%d\tcaml_shrink_heap(chunk);\n", i)
	}
	body, _ := json.Marshal(sb.String())
	lo, n, ok := deliveredLineWindow(body)
	if !ok || lo != 540 || n != 200 {
		t.Fatalf("want offset 540 and 200 lines, got %d/%d ok=%v", lo, n, ok)
	}
}

// Nothing to count means keep the caller's own window rather than invent a narrower one.
func TestNoGutterLeavesTheRequestedWindowAlone(t *testing.T) {
	for _, c := range []struct {
		name string
		body any
	}{
		{"empty file", ""},
		{"not a numbered body", "grep: runtime/nope.c: No such file or directory"},
		{"a tab with no number in front", "\tindented line\n"},
		{"a zero gutter", "0\tnot a line number\n"},
	} {
		b, _ := json.Marshal(c.body)
		if _, _, ok := deliveredLineWindow(b); ok {
			t.Errorf("%s: nothing to count, so the caller keeps its own window", c.name)
		}
	}
	// Content that is not a JSON string at all (a structured result) is left alone too.
	if _, _, ok := deliveredLineWindow(json.RawMessage(`{"name":"x"}`)); ok {
		t.Error("a structured result has no read gutter")
	}
}
