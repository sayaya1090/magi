package builtin

import (
	"strings"
	"testing"
)

// O3: an empty `old` matches at every character boundary, so the uniqueness
// check reported a nonsensical "occurs N times" and told the model to "add
// surrounding context". It must instead be rejected with a clear message.
func TestEditEmptyOldRejected(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, Edit{}, editArgs{Path: "f.txt", Old: "", New: "X"}, seed)
	if !isErr {
		t.Fatalf("empty old should be an error, got success: %q", got)
	}
	if strings.Contains(got, "occurs") || strings.Contains(got, "add surrounding context") {
		t.Fatalf("empty old should not report a uniqueness count: %q", got)
	}
	if !strings.Contains(got, "must not be empty") {
		t.Fatalf("empty old should say it must not be empty: %q", got)
	}
}

// The same guard applies inside a multiedit batch, named by hunk index.
func TestMultiEditEmptyOldRejected(t *testing.T) {
	seed := func(d string) { writeFile(d, "f.txt", "alpha\nbeta\n") }
	got, isErr := run(t, MultiEdit{}, multiEditArgs{Path: "f.txt", Edits: []editHunk{
		{Old: "alpha", New: "A"},
		{Old: "", New: "X"},
	}}, seed)
	if !isErr {
		t.Fatalf("empty old in a hunk should be an error, got success: %q", got)
	}
	if !strings.Contains(got, "must not be empty") {
		t.Fatalf("multiedit empty old should say it must not be empty: %q", got)
	}
}
