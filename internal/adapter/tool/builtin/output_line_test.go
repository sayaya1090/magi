package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The line naming the capture file says how much of the output is in the message above it, and that
// sentence has to be about THIS result.
//
// It used to be one sentence for every result: "N bytes — the full output; this message shows the
// head and tail". Observed live (fix-ocaml-gc, 2026-07-30): two `grep -n "^#define Make_header" …`
// runs, both exit 1, both answered "0 bytes — the full output; this message shows the head and
// tail" — announcing a head-and-tail view of nothing. Clipping is real and worth naming when it
// happens; claiming it when it did not tells the agent a middle was withheld that never existed.
func TestTheOutputLineSaysHowMuchOfTheOutputIsAbove(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// The live case: a command that matched nothing. No elision to announce.
	empty := write("empty.log", "")
	got := outputLine(empty, false, "")
	if !strings.Contains(got, "the command wrote nothing") {
		t.Errorf("an empty capture says so plainly:\n%s", got)
	}
	if strings.Contains(got, "head and tail") {
		t.Errorf("nothing was clipped out of nothing:\n%s", got)
	}

	// The same empty file, from a command that was KILLED, proves nothing about what it wrote.
	// Observed live: `make world.opt -j4 2>&1 | tail -50` timed out at 120s and tail, which holds
	// its output until the input ends, had flushed nothing — so a build that had been talking for
	// two minutes was described as having written nothing.
	got = outputLine(empty, false, "killed at the timeout")
	if strings.Contains(got, "wrote nothing —") {
		t.Errorf("a killed command's empty capture is not proof it was silent:\n%s", got)
	}
	for _, want := range []string{"the capture is empty", "killed at the timeout", "does not say it wrote nothing"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}

	// Shown whole: the file and the message hold the same bytes.
	full := write("full.log", "make: nothing to be done\n")
	got = outputLine(full, false, "")
	if !strings.Contains(got, "all of it is above") {
		t.Errorf("an unclipped result says the message has everything:\n%s", got)
	}
	if strings.Contains(got, "head and tail") {
		t.Errorf("nothing was omitted, so nothing may claim it was:\n%s", got)
	}
	if !strings.Contains(got, "25 bytes") {
		t.Errorf("the size is the file's own:\n%s", got)
	}

	// Clipped: the message is a view, and the file is where the rest is.
	big := write("big.log", strings.Repeat("x", 4096))
	got = outputLine(big, true, "")
	if !strings.Contains(got, "head and tail are above") || !strings.Contains(got, "the file has all of it") {
		t.Errorf("a clipped result names both halves of the fact:\n%s", got)
	}
	if !strings.Contains(got, "4096 bytes") {
		t.Errorf("the size is the whole output's, not the shown part's:\n%s", got)
	}

	// No capture file, or one that cannot be stat'ed: name it and claim nothing about it.
	if outputLine("", false, "") != "" {
		t.Error("no capture file, no line")
	}
	gone := filepath.Join(dir, "not-there.log")
	if got := outputLine(gone, true, ""); got != "output: "+gone+"\n" {
		t.Errorf("an unmeasurable file gets no size claim: %q", got)
	}
}

// The clip predicate the caller feeds it: an unclipped capture is exactly as long as its file, and
// truncateOut leaves the string alone until it is over the limit. Both are what the caller measures.
func TestClippingIsMeasuredNotAssumed(t *testing.T) {
	small := strings.Repeat("y", 1024)
	if got := truncateOut(small); len(got) != len(small) {
		t.Errorf("a small body is passed through whole: %d → %d", len(small), len(got))
	}
	huge := strings.Repeat("z", 64*1024)
	got := truncateOut(huge)
	if len(got) == len(huge) {
		t.Error("an oversized body is clipped")
	}
	if !strings.Contains(got, "bytes omitted") {
		t.Error("and the clip marks itself in the body where it happened")
	}

	// readHeadTail returns the file entire when it fits, so length equality is a sound test for
	// "nothing was dropped at capture".
	p := filepath.Join(t.TempDir(), "cap.log")
	if err := os.WriteFile(p, []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	// The capture reports whether it returned the file whole; the caller does not re-stat, so a
	// log a detached child grows afterwards cannot be mistaken for something having been elided.
	if b, whole := readHeadTail(p, 8192); !whole || len(b) != len(small) {
		t.Errorf("a capture under the cap is the whole file: %d bytes, whole=%v", len(b), whole)
	}
	if b, whole := readHeadTail(p, 256); whole || int64(len(b)) == int64(len(small)) {
		t.Errorf("a capture over the cap is a view of it: %d bytes, whole=%v", len(b), whole)
	}
	if b, whole := readHeadTail(filepath.Join(t.TempDir(), "absent.log"), 8192); whole || b != nil {
		t.Error("a file that could not be read is not a whole capture")
	}
}

// timedOutNote names where the limit came from, and a negative one came from the caller.
func TestTimedOutNoteNamesTheLimitsOrigin(t *testing.T) {
	for _, c := range []struct {
		effective, requested int
		want                 string
	}{
		{120, 0, "the default limit (no `timeout` given)"},
		{600, 9999, "your `timeout` of 9999s capped at the 600s maximum"},
		{600, 600, "your own `timeout` argument"},
		// Given, and unusable — not the same as never given.
		{120, -5, "your `timeout` of -5s is not a usable duration"},
	} {
		got := timedOutNote(c.effective, c.requested)
		if !strings.Contains(got, c.want) {
			t.Errorf("requested=%d: want %q in:\n%s", c.requested, c.want, got)
		}
		if c.requested < 0 && strings.Contains(got, "no `timeout` given") {
			t.Errorf("a timeout WAS given (%d):\n%s", c.requested, got)
		}
		if !strings.Contains(got, "KILLED at that mark") {
			t.Errorf("every shape still says the kill is not a verdict:\n%s", got)
		}
	}
}

// magi kills a command at its own timeout and knows that it did. A kill the SHELL performed
// arrives only as a number, and the sentence that reads an empty capture was gated on magi's own
// kill alone — so the same emptiness got two different answers. Observed live (headless-terminal,
// 2026-07-31): `timeout 15 python3 << EOF` whose first statement is a print came back "exit 124"
// with "the command wrote nothing — the file is empty". It wrote; the output died in its stdio
// buffer when timeout killed it, which is the very thing the killed branch exists to say.
func TestAKillTheShellPerformedCountsAsAKill(t *testing.T) {
	for _, c := range []struct {
		what string
		exit int
		want string
	}{
		{"GNU timeout fired", 124, "killed by `timeout` (exit 124)"},
		{"SIGKILL", 137, "killed by signal 9 (exit 137)"},
		{"SIGTERM", 143, "killed by signal 15 (exit 143)"},
		{"SIGINT", 130, "killed by signal 2 (exit 130)"},
		{"SIGPIPE", 141, "killed by signal 13 (exit 141)"},
	} {
		if got := killedByStatus(c.exit); got != c.want {
			t.Errorf("%s: got %q, want %q", c.what, got, c.want)
		}
	}
	// An ordinary ending is not a kill: claiming one would hedge every empty capture into
	// uselessness, and "the command wrote nothing" is exactly right for a command that finished.
	for _, exit := range []int{0, 1, 2, 127, 128, 160} {
		if got := killedByStatus(exit); got != "" {
			t.Errorf("exit %d is not a kill, got %q", exit, got)
		}
	}
}

// End to end through the line itself: the killed wording names what the status said.
func TestTheEmptyCaptureOfAKilledCommandNamesTheKill(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := outputLine(p, false, killedByStatus(124))
	if strings.Contains(got, "wrote nothing —") {
		t.Errorf("a killed command's empty capture is not proof it was silent:\n%s", got)
	}
	for _, want := range []string{"the capture is empty", "killed by `timeout` (exit 124)", "does not say it wrote nothing"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if got := outputLine(p, false, killedByStatus(1)); !strings.Contains(got, "the command wrote nothing") {
		t.Errorf("a command that ran to completion and wrote nothing still says so plainly:\n%s", got)
	}
}
