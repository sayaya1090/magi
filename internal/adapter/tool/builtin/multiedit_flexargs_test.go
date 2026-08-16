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

// A weaker model sometimes double-encodes the edits array — a JSON string holding the JSON.
// Observed twice in one validation day: the call was rejected on the type mismatch and the model
// fell back to nine sequential single edits, a model round trip per hunk. The intent is
// unambiguous, so the string is unwrapped and read.
func TestMultiEditAcceptsADoubleEncodedEditsArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha beta alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := `[{"old":"beta","new":"gamma"},{"old":"alpha","new":"delta","replaceAll":true}]`
	raw, _ := json.Marshal(map[string]any{"path": "f.txt", "edits": inner}) // edits as a STRING
	res, err := (MultiEdit{}).Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil || res.IsError {
		t.Fatalf("double-encoded edits were refused: err=%v res=%s", err, res.Content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "delta gamma delta" {
		t.Errorf("edits did not apply: %q", got)
	}
}

// The tolerance unwraps exactly one layer of honest mistake — a string that does not hold an edit
// array still fails, and says which shape it wanted.
func TestMultiEditStillRefusesGarbageEdits(t *testing.T) {
	for _, edits := range []any{"not json at all", 42} {
		raw, _ := json.Marshal(map[string]any{"path": "f.txt", "edits": edits})
		res, err := (MultiEdit{}).Execute(context.Background(), raw, port.ToolEnv{Workdir: t.TempDir()})
		if err != nil {
			t.Fatalf("tool errors are results, not errors: %v", err)
		}
		if !res.IsError {
			t.Errorf("edits=%v must be refused, got %s", edits, res.Content)
		}
	}
}

// The runner-found-nothing note: an exit 0 above "Ran 0 tests" is the emptiest success there is,
// and it is hoisted where it cannot be clipped away.
func TestZeroTestsNote(t *testing.T) {
	if n := zeroTestsNote(0, "----\nRan 0 tests in 0.000s\n\nOK\n"); !strings.Contains(n, "discovered no tests") {
		t.Errorf("unittest's empty run must be annotated, got %q", n)
	}
	if n := zeroTestsNote(0, "===== no tests ran in 0.01s ====="); n == "" {
		t.Error("pytest's empty run must be annotated")
	}
	if n := zeroTestsNote(1, "Ran 0 tests"); n != "" {
		t.Errorf("a nonzero exit already says something happened, got %q", n)
	}
	if n := zeroTestsNote(0, "collected 10 items\n10 passed"); n != "" {
		t.Errorf("a real run must not be annotated, got %q", n)
	}
}
