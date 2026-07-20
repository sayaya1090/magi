package app

import "testing"

// Reading many DIFFERENT files must not trip the stall nudge: each novel inspection
// advances the stall window, so broad information-gathering is treated as progress
// (the reported false-stall while reading 7 report files). A REPEATED inspection is
// not novel and does not credit progress — the read-loop guard owns that.
func TestNovelInspectDefersStallNudge(t *testing.T) {
	g := newRunGuard()
	// Simulate many distinct read calls: each check() climbs sinceProgress, and a
	// novel result credits inspect progress (advancing lastStallAt).
	for i := 0; i < noProgressNudge+4; i++ {
		g.sinceProgress++ // stands in for check()'s increment
		g.noteInspectProgress(true)
	}
	if g.shouldNudge() == "stalled" {
		t.Fatalf("novel inspections should not trigger the stall nudge (sinceProgress=%d lastStallAt=%d)",
			g.sinceProgress, g.lastStallAt)
	}
	// Now the same call repeated (not novel): no credit → the window climbs and the
	// stall nudge eventually fires.
	base := g.sinceProgress
	for i := 0; i < noProgressNudge; i++ {
		g.sinceProgress++
		g.noteInspectProgress(false) // repeated inspect → no credit
	}
	if g.sinceProgress-g.lastStallAt < noProgressNudge {
		t.Fatalf("repeated inspect must let the window climb (base=%d now=%d lastStallAt=%d)",
			base, g.sinceProgress, g.lastStallAt)
	}
}
