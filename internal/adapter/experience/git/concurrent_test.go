package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The global tier is shared by every companion of one person, so two of them learning at the same
// moment is ordinary — and retrieval reads every file in the directory while they do.
//
// os.WriteFile truncates and then writes, so a reader landing between those two got a skill with no
// content: the retrieval that was supposed to carry a lesson forward carries an empty string
// instead, silently, into a prompt. This is the same defect that was found for real in the plugin
// materialiser, one directory over.
func TestASkillIsNeverReadableHalfWritten(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	ctx := context.Background()
	long := strings.Repeat("a lesson worth keeping, written out at length so a torn write is visible. ", 60)

	write := func(body string) {
		if err := s.Propose(ctx, port.Contribution{
			Skills: []port.Skill{{Name: "run-the-tests", Description: "run them first", Body: body}},
		}); err != nil {
			t.Error(err)
		}
	}
	write(long)
	path := filepath.Join(dir, "skills", "skill-run-the-tests.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the skill did not land where this test looks: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			write(long + strings.Repeat("x", i%17))
		}
	}()

	var bad string
	for i := 0; i < 4000 && bad == ""; i++ {
		b, err := os.ReadFile(path)
		if err != nil {
			bad = "the skill vanished mid-write: " + err.Error()
			break
		}
		if !strings.Contains(string(b), "a lesson worth keeping") {
			bad = "read a skill with no lesson in it: " + strings.TrimSpace(string(b))
		}
	}
	close(stop)
	<-done
	if bad != "" {
		t.Error(bad)
	}
	// And nothing is left behind: a store directory filling with dot-files is its own defect, and
	// the loader walks this directory.
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
}
