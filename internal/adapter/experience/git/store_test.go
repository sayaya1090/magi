package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRetrieveAndPropose(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "memories"), 0o755)
	os.WriteFile(filepath.Join(dir, "memories", "tabs.md"), []byte("Always use tabs for indentation in this repo"), 0o644)
	os.WriteFile(filepath.Join(dir, "memories", "ports.md"), []byte("The metrics server listens on port 9090"), 0o644)

	s := New(dir)
	ctx := context.Background()

	// Retrieve by keyword overlap.
	mems, _, err := s.Retrieve(ctx, "what indentation should I use, tabs or spaces?")
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) == 0 || !contains(mems[0].Text, "tabs") {
		t.Fatalf("expected tabs memory first, got %+v", mems)
	}

	// Propose lands in pending/.
	if err := s.Propose(ctx, port.Contribution{
		Memories: []port.Memory{{Text: "Run make build before committing", Tags: []string{"build"}}},
		Source:   "agent",
	}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "pending"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 pending file, got %d", len(entries))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
