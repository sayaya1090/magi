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

// Inside a multiedit batch the guard is per-BATCH, not per-hunk: a no-op hunk in a
// batch that also carries real changes is skipped (models include unchanged hunks
// "for context", and rejecting the whole batch for one harmless hunk was a live
// friction source) — the phantom-progress concern doesn't apply because real content
// changed. The result names the skip so the model learns the hunk did nothing.
func TestMultiEditNoopHunkSkipped(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
		{Old: "alpha", New: "A"},
		{Old: "beta", New: "beta"},
	}}, seed)
	if isErr {
		t.Fatalf("a no-op hunk beside a real edit must not fail the batch: %q", got)
	}
	if !strings.Contains(got, "applied 1 edits") || !strings.Contains(got, "skipped 1 no-op") {
		t.Fatalf("result should count the applied edit and name the skip: %q", got)
	}
}

// An ALL-no-op batch would rewrite identical bytes yet report success — the same
// phantom-progress signal the single-edit guard rejects. It stays an error.
func TestMultiEditAllNoopRejected(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
		{Old: "alpha", New: "alpha"},
		{Old: "beta", New: "beta"},
	}}, seed)
	if !isErr {
		t.Fatalf("an all-no-op batch must be rejected, got success: %q", got)
	}
	if !strings.Contains(got, "no-op") {
		t.Fatalf("rejection should say every edit was a no-op: %q", got)
	}
}
