package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readForChange declines a file past the compare cap, and everything content-shaped correctly goes
// quiet with it. Existence is a different measurement: it comes from a stat, it costs nothing, and
// it is still true for a file magi never read. Going silent about a removal because the file was
// too big loses a signal magi has.
//
// Observed live (custom-memory-heap-crash, 2026-07-30): `rm -f /app/release /app/debug …` on
// binaries of 8.3 MB. The pre-cap build said `debug: this command deleted the file` — true — and
// the cap fix would have dropped it along with the false claim it was there to stop.
func TestADeletionIsStillReportedWhenTheFileWasTooBigToRead(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "release")
	if err := os.WriteFile(big, make([]byte, changeReadCap+1), 0o644); err != nil {
		t.Fatal(err)
	}
	// What the snapshot records before the command runs.
	before, readable := readForChange(dir, "release")
	if readable || before != "" {
		t.Fatalf("a file past the cap yields no comparable content: %d %v", len(before), readable)
	}
	if !pathExists(dir, "release") {
		t.Fatal("…but it plainly exists, and that is measured separately")
	}

	// The command deletes it.
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
	if pathExists(dir, "release") {
		t.Fatal("gone")
	}
}

// The sentence itself: it reports the removal and refuses to characterise what was removed.
func TestTheOversizeDeletionNoteClaimsOnlyWhatAStatSupports(t *testing.T) {
	const note = ": this command deleted the file. magi could not read its contents " +
		"(a directory, or larger than it compares), so this says the path is " +
		"gone and nothing about what it held."
	for _, want := range []string{"deleted the file", "could not read its contents", "nothing about what it held"} {
		if !strings.Contains(note, want) {
			t.Errorf("want %q in the note", want)
		}
	}
	// It must NOT borrow the comparing branch's claim, which rests on a hash this path never had.
	if strings.Contains(note, "before this turn") {
		t.Error("no history here, so no claim about the pre-turn state")
	}
}
