package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/atomicfile"
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
		// Read through the door the store reads through, not around it. On Windows a read landing
		// mid-replacement sees neither version — it fails with a sharing violation — and
		// atomicfile.ReadFile is where that window is waited out. os.ReadFile here would measure
		// the platform instead of the write, and did: this test failed on Windows for a reason
		// that had nothing to do with the truncate-then-write it was written for.
		b, err := atomicfile.ReadFile(path)
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

// A retrieval never loses a skill that is there, while somebody else is learning.
//
// ⚠ **This is the defect the test above only half covers, and it is invisible by construction.**
// Pool skips a document whose text reads back empty — `if text == "" { continue }` — and readFile
// turns any read error into "". So a read that fails is not an error anywhere: the skill is simply
// not among the candidates, the answer is built without a lesson the store is holding, and nothing
// says so. There is no log line, no empty body to notice, no failure to retry.
//
// On Windows that read fails for an ordinary reason. Every writer in this package is
// atomicfile.Write, so these files are replaced under readers by design — the global tier is
// shared by every companion of one person, so one learning while another retrieves is the shape,
// not a race — and a read landing in the replacement window gets ERROR_SHARING_VIOLATION.
//
// Measured 2026-09-11 before the fix: 89 of 3000 retrievals, 3.0%, came back with the skill gone.
// After: 0.
//
// Asked at the PRODUCT surface deliberately. A test that reads the file proves the file can be
// read; this asks the question the store is for — is the lesson among the candidates — and that is
// the one that was answering wrong.
func TestARetrievalNeverLosesASkillWhileAnotherCompanionLearns(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	ctx := context.Background()
	long := strings.Repeat("run the tests before you claim it works. ", 40)
	write := func(body string) error {
		return s.Propose(ctx, port.Contribution{
			Skills: []port.Skill{{Name: "run-the-tests", Description: "run them first", Body: body}},
		})
	}
	if err := write(long); err != nil {
		t.Fatal(err)
	}

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := write(long + strings.Repeat("x", i%17)); err != nil {
				t.Errorf("propose: %v", err)
				return
			}
		}
	}()

	var missing, empty int
	const rounds = 1500
	for i := 0; i < rounds; i++ {
		_, skills, err := s.Pool(ctx, "run the tests", nil)
		if err != nil {
			t.Fatalf("pool: %v", err)
		}
		switch {
		case len(skills) == 0:
			missing++
		case !strings.Contains(skills[0].V.Body, "run the tests"):
			empty++
		}
	}
	close(stop)
	<-done
	if missing > 0 {
		t.Errorf("%d of %d retrievals lost a skill that was there — a read that failed became a "+
			"document that does not exist, and the answer was built without it", missing, rounds)
	}
	if empty > 0 {
		t.Errorf("%d of %d retrievals carried the skill with no lesson in it", empty, rounds)
	}
}
