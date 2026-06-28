package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDumpCorpus writes the shared reviewCorpus to MAGI_DUMP_DIR so an external
// tool audits byte-identical files. Gated.
func TestDumpCorpus(t *testing.T) {
	dir := os.Getenv("MAGI_DUMP_DIR")
	if dir == "" {
		t.Skip("set MAGI_DUMP_DIR to dump the review corpus")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range reviewCorpus {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("wrote %d files to %s", len(reviewCorpus), dir)
}

// TestScoreFile scores an external tool's captured output with the SAME planted-
// issue detector used for magi, so cross-tool coverage is apples-to-apples.
// Gated: MAGI_SCORE_FILE=<path>.
func TestScoreFile(t *testing.T) {
	path := os.Getenv("MAGI_SCORE_FILE")
	if path == "" {
		t.Skip("set MAGI_SCORE_FILE to score a captured reply")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cov, found := coverage(string(b))
	t.Logf("SCORE %s: coverage=%d/10 %v", path, cov, found)
}
