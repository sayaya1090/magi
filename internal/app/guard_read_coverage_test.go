package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The stall window is reset by a read that gathers NEW information. "New" was decided by the
// call's fingerprint, and a read's fingerprint carries its offset — so re-opening the same
// twelve lines two lines earlier was first-seen, and bought another full window.
//
// Replayed from the live sequence (fix-ocaml-gc, one file, 49 minutes, no edit): offsets
// 640/642 with limits 30–50, over a region already delivered in full at the very first read.
func TestReReadingADeliveredWindowIsNotNewInformation(t *testing.T) {
	g := newRunGuard()
	const p = "runtime/shared_heap.c"

	if !g.noteReadCoverage(p, 540, 200) {
		t.Fatal("the first read of a region is new information")
	}
	// Every one of these lies inside 540..739, which the agent has already been shown.
	for _, w := range [][2]int{{642, 30}, {640, 35}, {640, 35}, {642, 50}, {643, 12}} {
		if g.noteReadCoverage(p, w[0], w[1]) {
			t.Errorf("read{offset:%d,limit:%d} was already delivered — not new information", w[0], w[1])
		}
	}
	// Paging FORWARD is the case this credit exists for and must keep working.
	if !g.noteReadCoverage(p, 740, 200) {
		t.Error("the next page of a file is new information")
	}
	// A window that only clips the edge of what was shown is still mostly new.
	if !g.noteReadCoverage(p, 900, 200) {
		t.Error("a window mostly past what was delivered is new information")
	}
	// …and one that only clips the edge of what is NEW is still mostly a re-read: 700..759 is
	// 40 lines already seen against 20 that are not.
	if g.noteReadCoverage(p, 700, 60) {
		t.Error("a window mostly inside delivered content is a re-read")
	}
	// Another file is another file, however much of this one has been read.
	if !g.noteReadCoverage("runtime/major_gc.c", 540, 200) {
		t.Error("a different path is new information")
	}
}

// A read with no offset/limit is the whole navigable window, so repeating it is a re-read even
// though neither call names a line.
func TestABareReadCoversItsDefaultWindow(t *testing.T) {
	g := newRunGuard()
	if !g.noteReadCoverage("notes.txt", 0, 0) {
		t.Fatal("the first bare read is new information")
	}
	if g.noteReadCoverage("notes.txt", 0, 0) {
		t.Error("a second bare read delivers exactly what the first did")
	}
	// And it covers what an explicit window inside it would have delivered.
	if g.noteReadCoverage("notes.txt", 300, 50) {
		t.Error("a window inside the default page was already delivered")
	}
}

// Editing a file makes its contents new: what magi showed of it no longer describes it, so the
// next read is information again. Without this the guard would count a post-edit re-read as
// circling and climb toward a nudge for exactly the behavior it wants.
func TestChangingAFileMakesItWorthReadingAgain(t *testing.T) {
	g := newRunGuard()
	const p = "runtime/shared_heap.c"
	g.noteReadCoverage(p, 640, 40)
	if g.noteReadCoverage(p, 640, 40) {
		t.Fatal("unchanged, the same window is not new information")
	}
	g.dropReadCoverage(p)
	if !g.noteReadCoverage(p, 640, 40) {
		t.Error("after the file changed, reading it again IS information")
	}
	// Only that path — a sibling's coverage is untouched by an edit it did not receive.
	g.noteReadCoverage("runtime/major_gc.c", 1, 100)
	g.dropReadCoverage(p)
	if g.noteReadCoverage("runtime/major_gc.c", 1, 100) {
		t.Error("dropping one path must not forget another")
	}
}

// Coverage is stored as disjoint regions, so a run that pages one file a hundred times holds a
// handful of spans rather than a hundred — the check runs on every read.
func TestCoverageStaysCompact(t *testing.T) {
	g := newRunGuard()
	const p = "big.c"
	for i := 0; i < 100; i++ {
		g.noteReadCoverage(p, 1+i*10, 10) // contiguous pages, in order
	}
	if n := len(g.readSpans[p]); n != 1 {
		t.Errorf("contiguous pages must merge into one region, got %d", n)
	}
	g.noteReadCoverage(p, 5000, 10) // a gap → a second region, and no more
	if n := len(g.readSpans[p]); n != 2 {
		t.Errorf("a disjoint page is a second region, got %d", n)
	}
}

// The unit above tests the counter; this one tests the SEAM. The defect lived in what
// noteToolOutcome asked, not in what the guard could answer, so a test that calls the guard
// directly would have passed before the fix.
//
// Argument shapes are the live ones: the model sends `"offset": "642.0"`, and a strict re-parse
// of those fields fails the whole struct — seeing an unset offset where the read saw line 642,
// and reporting fresh coverage for a window already delivered.
func TestReReadThroughTheSeamDoesNotResetTheStallWindow(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := newRunGuard()
	read := func(args string) {
		tc := &session.ToolCall{CallID: "c" + args, Name: "read", Args: json.RawMessage(args)}
		_, n, _ := g.check(tc.Name, tc.Args)
		a.noteToolOutcome(sid, g, toolOutcome{
			tc: tc, res: &session.ToolResult{Content: json.RawMessage(`"…"`)}, workdir: "/app",
			fp: "fp" + args, novel: n == 1, toolOK: true,
		})
	}

	read(`{"path":"/app/runtime/shared_heap.c","offset":"540.0","limit":"200.0"}`)
	base := g.sinceProgress - g.lastStallAt
	if base != 0 {
		t.Fatalf("the first read of a region is information and resets the window, got %d", base)
	}
	// Four more reads of lines already inside 540..739, each at its own offset — four distinct
	// fingerprints, so `novel` is true for every one of them.
	for _, args := range []string{
		`{"path":"/app/runtime/shared_heap.c","offset":"642.0","limit":"30.0"}`,
		`{"path":"/app/runtime/shared_heap.c","offset":"640.0","limit":"35.0"}`,
		`{"path":"/app/runtime/shared_heap.c","offset":"643.0","limit":"12.0"}`,
		`{"path":"/app/runtime/shared_heap.c","offset":"641.0","limit":"25.0"}`,
	} {
		read(args)
	}
	if w := g.sinceProgress - g.lastStallAt; w != 4 {
		t.Errorf("four re-reads of a delivered window must climb the stall window, got %d", w)
	}
	// Reading somewhere it has not been still buys a window — the credit is not simply gone.
	read(`{"path":"/app/runtime/shared_heap.c","offset":"1580.0","limit":"60.0"}`)
	if w := g.sinceProgress - g.lastStallAt; w != 0 {
		t.Errorf("a genuinely new region must still reset the window, got %d", w)
	}
	// A path outside the workdir has no relative form; it must key on itself, not collapse.
	read(`{"path":"/etc/hosts"}`)
	if w := g.sinceProgress - g.lastStallAt; w != 0 {
		t.Errorf("a first read of another file is information, got %d", w)
	}
	read(`{"path":"/etc/hosts"}`)
	if w := g.sinceProgress - g.lastStallAt; w != 1 {
		t.Errorf("reading it again is not, got %d", w)
	}
}
