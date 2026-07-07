package builtin

import (
	"strings"
	"testing"
)

// An edit whose `old` equals `new` changes nothing. Left to run it would rewrite
// the file with identical bytes yet report success — a phantom "progress" signal
// that can refresh stall/loop defenses (observed in a bench run where the agent
// re-issued the same no-op edit). It must be rejected up front instead.
func TestEditNoChangeRejected(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, Edit{}, editArgs{Path: "f.txt", Old: "alpha", New: "alpha"}, seed)
	if !isErr {
		t.Fatalf("old==new should be an error, got success: %q", got)
	}
	if !strings.Contains(got, "no change") {
		t.Fatalf("old==new should say 'no change': %q", got)
	}
}

// The same guard applies per hunk inside a multiedit batch, named by index.
func TestMultiEditNoChangeRejected(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
		{Old: "alpha", New: "A"},
		{Old: "beta", New: "beta"},
	}}, seed)
	if !isErr {
		t.Fatalf("old==new hunk should be an error, got success: %q", got)
	}
	if !strings.Contains(got, "no change") {
		t.Fatalf("multiedit old==new should say 'no change': %q", got)
	}
}
