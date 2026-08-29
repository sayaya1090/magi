package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The write-approval diff is taken against the file as it is, so a one-line addition reads as one
// added line beside its context — not as the whole file added (the all-`+` view live-QA could not
// find the change in).
func TestWriteApprovalDiffIsAgainstTheRealFile(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "one\ntwo\nthree\n"})
	diff, ok := writeApprovalDiff(wd, args)
	if !ok {
		t.Fatal("an existing readable file must yield an authoritative diff")
	}
	if !strings.Contains(diff, " one") || !strings.Contains(diff, "+three") {
		t.Fatalf("the diff must show context and the one addition, got %q", diff)
	}
	if strings.Contains(diff, "+one") {
		t.Fatalf("an unchanged line must not read as added, got %q", diff)
	}
}

func TestWriteApprovalDiffFallsBackHonestly(t *testing.T) {
	wd := t.TempDir()
	fresh, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hello\n"})
	if _, ok := writeApprovalDiff(wd, fresh); ok {
		t.Error("a file that does not exist yet keeps the args view — all additions IS its truth")
	}
	escape, _ := json.Marshal(map[string]string{"path": "../outside.txt", "content": "x"})
	if _, ok := writeApprovalDiff(wd, escape); ok {
		t.Error("a path that leaves the workdir must not be read for a preview")
	}
	if err := os.WriteFile(filepath.Join(wd, "same.txt"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	noop, _ := json.Marshal(map[string]string{"path": "same.txt", "content": "keep\n"})
	diff, ok := writeApprovalDiff(wd, noop)
	if !ok || diff != "" {
		t.Errorf("an identical rewrite is authoritatively a no-op, got ok=%v diff=%q", ok, diff)
	}
}
