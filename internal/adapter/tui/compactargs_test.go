package tui

import "testing"

// compactArgs must render multi-key args in a STABLE order — Go map iteration is
// randomized, which previously reshuffled the line every frame (flicker, e.g.
// "status=done" appearing and disappearing).
func TestCompactArgsStableOrder(t *testing.T) {
	args := `{"status":"done","summary":"read README","details":"ok"}`
	first := compactArgs(args)
	for i := 0; i < 50; i++ {
		if got := compactArgs(args); got != first {
			t.Fatalf("compactArgs not stable: %q vs %q", first, got)
		}
	}
	// Sorted: details, status, summary.
	want := "details=ok status=done summary=read README"
	if first != want {
		t.Errorf("compactArgs = %q, want %q", first, want)
	}
}
