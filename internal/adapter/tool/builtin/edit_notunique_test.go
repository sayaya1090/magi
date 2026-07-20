package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func editCall(t *testing.T, dir string, args map[string]any) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.Content), res.IsError
}

// An ambiguous old string must name each occurrence's whole-line anchor (a plain
// line number now) so the model can pivot to `at`/`to` in one retry — and the
// suggested anchor must actually work when passed back.
func TestEditNotUniqueSuggestsAnchors(t *testing.T) {
	dir := t.TempDir()
	content := "alpha\nvalue = 1\nbeta\nvalue = 1\ngamma\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, isErr := editCall(t, dir, map[string]any{"path": "f.txt", "old": "value = 1", "new": "value = 2"})
	if !isErr {
		t.Fatalf("ambiguous edit must fail, got %q", out)
	}
	ref2, ref4 := "2", "4"
	if !strings.Contains(out, ref2) || !strings.Contains(out, ref4) {
		t.Fatalf("error must list both occurrence anchors %s and %s, got: %s", ref2, ref4, out)
	}
	if !strings.Contains(out, "`at`") {
		t.Errorf("error should steer to the anchored mode, got: %s", out)
	}

	// The suggested anchor resolves the ambiguity in one step.
	out, isErr = editCall(t, dir, map[string]any{"path": "f.txt", "at": ref4, "new": "value = 2"})
	if isErr {
		t.Fatalf("anchored retry with the suggested ref must succeed, got: %s", out)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	want := "alpha\nvalue = 1\nbeta\nvalue = 2\ngamma\n"
	if string(got) != want {
		t.Errorf("anchored edit applied wrong span:\n got: %q\nwant: %q", got, want)
	}
}

// A multi-line ambiguous old string reports at..to spans (plain line numbers)
// covering the whole block.
func TestEditNotUniqueMultilineSpans(t *testing.T) {
	dir := t.TempDir()
	content := "if x {\n\treturn\n}\nmid\nif x {\n\treturn\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := editCall(t, dir, map[string]any{"path": "f.go", "old": "if x {\n\treturn\n}", "new": "if y {\n\treturn\n}"})
	if !isErr {
		t.Fatalf("ambiguous edit must fail, got %q", out)
	}
	span1, span2 := "1..3", "5..7"
	if !strings.Contains(out, span1) || !strings.Contains(out, span2) {
		t.Errorf("error must list block spans %s and %s, got: %s", span1, span2, out)
	}
}

// The whitespace-tolerant tier reports anchors too (occurrences differ only in
// trailing whitespace from the quoted old).
func TestEditNotUniqueFlexibleTier(t *testing.T) {
	dir := t.TempDir()
	content := "foo()  \nbar\nfoo()\t\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := editCall(t, dir, map[string]any{"path": "f.txt", "old": "foo()", "new": "baz()"})
	if !isErr {
		t.Fatalf("ambiguous flexible edit must fail, got %q", out)
	}
	// Anchors are the ACTUAL file line numbers of each occurrence.
	if !strings.Contains(out, "1") || !strings.Contains(out, "3") {
		t.Errorf("flexible-tier error must list line anchors 1 and 3, got: %s", out)
	}
}
