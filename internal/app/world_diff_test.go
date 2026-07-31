package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wsWrite writes one file under root, making its directory as needed.
func wsWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Reading mtimes answers "what looks new", which is a different question from "what did this turn
// do". The turn now indexes the workspace when it starts and the snapshot is the difference, so
// the two things mtime cannot see become visible — and the one thing only mtime sees is kept.
//
// Each case below is a separate failure if the comparison drops its half.
func TestTheSnapshotReportsWhatTheTurnActuallyDid(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "run.py", "original content here")
	wsWrite(t, root, "keep.txt", "untouched")
	wsWrite(t, root, "doomed.c", "will be deleted")
	base := indexWorkspace(root)
	since := time.Now()
	time.Sleep(1100 * time.Millisecond) // filesystem mtime granularity

	wsWrite(t, root, "run.py", "edited content here!!") // SAME size, new mtime
	wsWrite(t, root, "new.go", "brand new file")
	if err := os.Remove(filepath.Join(root, "doomed.c")); err != nil {
		t.Fatal(err)
	}
	// A write that PRESERVED its mtime — tar -p, cp -p, a restore from a copy. Only the size
	// against the baseline can see it.
	wsWrite(t, root, "restored.dat", "0123456789")
	old := since.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "restored.dat"), old, old); err != nil {
		t.Fatal(err)
	}
	base["restored.dat"] = 3 // the turn started with a 3-byte copy of it

	got := worldDiff(root, since, base)
	for _, want := range []string{
		"run.py",       // same size, new mtime — the ordinary edit, and what size alone would miss
		"new.go",       // created
		"restored.dat", // mtime preserved, size changed — what mtime alone would miss
		"doomed.c",     // deleted — what neither could see before
		"GONE",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the snapshot does not report %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "keep.txt") {
		t.Errorf("a file the turn never touched is reported as part of what it did:\n%s", got)
	}
}

// A session with no baseline must not read an empty index as "everything was deleted". It falls
// back to the mtime-only snapshot, which is what a resumed session or a path that never entered
// the loop should get.
func TestNoBaselineFallsBackRatherThanClaimingDeletions(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "a.txt", "one")
	wsWrite(t, root, "b.txt", "two")
	got := worldDiff(root, time.Now().Add(-time.Hour), nil)
	if strings.Contains(got, "GONE") {
		t.Errorf("a missing baseline was read as a workspace that lost its files:\n%s", got)
	}
	for _, want := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("the fallback lost %q:\n%s", want, got)
		}
	}
}

// A turn that deleted everything says exactly that, rather than the sentence it used to produce —
// "no file in the workspace has been modified since this task started" — which was the report a
// wiped workspace got.
func TestAWipedWorkspaceIsNotAQuietOne(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "deliverable.py", "the whole point")
	base := indexWorkspace(root)
	if err := os.Remove(filepath.Join(root, "deliverable.py")); err != nil {
		t.Fatal(err)
	}
	got := worldDiff(root, time.Now().Add(-time.Hour), base)
	if strings.Contains(got, "no file in the workspace has been modified") {
		t.Errorf("a workspace whose deliverable was deleted reports nothing happened:\n%s", got)
	}
	if !strings.Contains(got, "deliverable.py") || !strings.Contains(got, "GONE") {
		t.Errorf("the deletion is not named:\n%s", got)
	}
}

// Deletions survive the trim. The listing keeps its tail, and a removed file is the one thing here
// that nothing else in the record reports — so it is written last on purpose.
func TestDeletionsSurviveTheListingCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < snapshotFileCap*2; i++ {
		wsWrite(t, root, filepath.Join("d"+string(rune('a'+i%20)), "f.txt"), "x")
	}
	wsWrite(t, root, "gone.py", "bye")
	base := indexWorkspace(root)
	if err := os.Remove(filepath.Join(root, "gone.py")); err != nil {
		t.Fatal(err)
	}
	got := worldDiff(root, time.Now().Add(-time.Hour), base)
	if !strings.Contains(got, "gone.py") {
		t.Errorf("the deletion was trimmed away by the listing cap:\n%s", got)
	}
}

// The block is read by a model that pays for every token of it, so a size is rendered short. The
// unit has to survive: "1.2" alone says nothing.
func TestSizesAreRenderedShortAndKeepTheirUnit(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{{0, "0 B"}, {999, "999 B"}, {2048, "2.0 KB"}, {5 * 1024 * 1024, "5.0 MB"}} {
		if got := fmtBytes(c.n); got != c.want {
			t.Errorf("fmtBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// The index obeys the same skip rules the snapshot renders with. If they drifted, every file under
// a skipped tree would be indexed at turn start and then read as deleted at the finish.
func TestTheIndexSkipsWhatTheSnapshotSkips(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "run.py", "kept")
	wsWrite(t, root, "vendor/lib.c", "skipped")
	wsWrite(t, root, ".cache/blob", "skipped")
	idx := indexWorkspace(root)
	if _, ok := idx["run.py"]; !ok {
		t.Error("the index is missing an ordinary file")
	}
	for _, p := range []string{filepath.Join("vendor", "lib.c"), filepath.Join(".cache", "blob")} {
		if _, ok := idx[p]; ok {
			t.Errorf("the index entered %s, which the snapshot's walk does not", p)
		}
	}
	// …and the round trip is quiet: nothing changed, nothing reported.
	if got := worldDiff(root, time.Now().Add(time.Hour), idx); strings.Contains(got, "GONE") {
		t.Errorf("an unchanged workspace reports deletions:\n%s", got)
	}
}
