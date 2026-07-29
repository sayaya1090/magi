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
	got := outputLine(empty, false)
	if !strings.Contains(got, "the command wrote nothing") {
		t.Errorf("an empty capture says so plainly:\n%s", got)
	}
	if strings.Contains(got, "head and tail") {
		t.Errorf("nothing was clipped out of nothing:\n%s", got)
	}

	// Shown whole: the file and the message hold the same bytes.
	full := write("full.log", "make: nothing to be done\n")
	got = outputLine(full, false)
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
	got = outputLine(big, true)
	if !strings.Contains(got, "head and tail are above") || !strings.Contains(got, "the file has all of it") {
		t.Errorf("a clipped result names both halves of the fact:\n%s", got)
	}
	if !strings.Contains(got, "4096 bytes") {
		t.Errorf("the size is the whole output's, not the shown part's:\n%s", got)
	}

	// No capture file, or one that cannot be stat'ed: name it and claim nothing about it.
	if outputLine("", false) != "" {
		t.Error("no capture file, no line")
	}
	gone := filepath.Join(dir, "not-there.log")
	if got := outputLine(gone, true); got != "output: "+gone+"\n" {
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
