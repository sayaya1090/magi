package builtin

import (
	"strings"
	"testing"
	"time"
)

// A `&` detaches wherever it stands, not only at the end. Observed live (fix-ocaml-gc,
// 2026-07-29): the agent backgrounded a build on one line and ran a probe on the next, so the
// exit 0 it was handed belonged to the probe and the build had no status anywhere — no job entry,
// no exit, no per-stage numbers. The note that exists to say precisely that never fired, because
// the old predicate required the `&` to be the command's last character.
func TestDetachIsFoundWhereverItStands(t *testing.T) {
	const live = "cd /app/ocaml && make world 2>&1 &\nsleep 60 && ps aux | grep \"make\""
	note := backgroundTailNote(0, live, freshSession(t))
	if note == "" {
		t.Fatal("the build was detached and the reported exit is the probe's — that is the whole point")
	}
	if !strings.Contains(note, "FOREGROUND") {
		t.Errorf("say whose exit the agent is holding:\n%s", note)
	}

	for _, c := range []struct {
		what, cmd string
		want      bool
	}{
		{"a trailing detach still counts", "make world &", true},
		{"detached, then more work", "make world & sleep 60 && ps aux", true},
		{"nohup with a tail", "nohup python3 serve.py > log 2>&1 &", true},
		// The three shapes that are not a detach. `&&` is a list operator, `2>&1` names a file
		// descriptor, and anything inside quotes is text the shell never parses as an operator.
		{"a list operator is not a detach", "./configure && make -j4", false},
		{"an fd dup is not a detach", "make world 2>&1 | tail -50", false},
		{"an fd dup with a space", "make 2> &1", false},
		{"a quoted ampersand is data", `grep "foo & bar" notes.txt`, false},
		{"a single-quoted ampersand is data", `python3 -c 'print("a & b")'`, false},
		{"plain work is not a detach", "ls -la /app", false},
		// A heredoc body is data the shell hands to a program verbatim, not shell text. Observed
		// live within an hour of shipping the wider predicate: C++ written through `cat > f <<EOF`
		// was called a detach four times, and the relaunch warning named the program `}`.
		{"C++ in a heredoc is not shell", "cat > /tmp/c.cpp << 'EOF'\nint f(int& x) { return x & 1; }\nEOF", false},
		{"an unquoted heredoc tag too", "cat > /tmp/c.cpp <<EOF\na && b & c\nEOF", false},
		{"<<- strips tabs and is still a heredoc", "cat > /tmp/c.cpp <<-EOF\n\tx & y\nEOF", false},
		{"a real detach AFTER a heredoc still counts", "cat > /tmp/c.cpp << 'EOF'\nint x & 1;\nEOF\nmake world &", true},
		{"an arithmetic shift is not a heredoc intro", "echo $((1<<2)) && ls", false},
		{"…and does not hide a later detach", "echo $((1<<2)); make world &", true},
	} {
		got := backgroundTailNote(0, c.cmd, freshSession(t)) != ""
		if got != c.want {
			t.Errorf("%s: %q → note=%v, want %v", c.what, c.cmd, got, c.want)
		}
	}

	// A nonzero exit says something happened on its own; the note is for the exit that does not.
	if backgroundTailNote(1, "make world &", freshSession(t)) != "" {
		t.Error("a failing command's status is already the news")
	}

	// The duplicate warning names the DETACHED program, not what ran in the foreground after it.
	dup := freshSession(t)
	first := backgroundTailNote(0, "make world & sleep 5", dup)
	second := backgroundTailNote(0, "make world & sleep 5", dup)
	if strings.Contains(first, "ALREADY started") {
		t.Errorf("the first launch is not a duplicate:\n%s", first)
	}
	if !strings.Contains(second, "`make` was ALREADY started") {
		t.Errorf("the second must name make, not sleep:\n%s", second)
	}
}

// The duplicate warning is about something IN FLIGHT, so the memory behind it expires.
//
// It used to be a set that never forgot, while the sentence it produced said "earlier in this
// run" — and a session in a daemon runs for days over many turns. A `make &` on Monday made
// Friday's first `make &` read as a duplicate, and the note tells the agent to free a port and
// not start it. Both halves are checked here, because a guard that only proves the warning stays
// quiet is equally satisfied by a warning that never fires at all.
//
// The record is aged by hand rather than by waiting: the window is maxBashTimeout, and a test
// that waits ten minutes to prove a timer is a test nobody runs.
func TestADetachStopsBeingEvidenceOnceItsWindowPasses(t *testing.T) {
	sid := freshSession(t)
	if n := backgroundTailNote(0, "make world &", sid); strings.Contains(n, "ALREADY") {
		t.Fatalf("nothing was detached before this one:\n%s", n)
	}
	// Seconds old: still the warning this exists for.
	if n := backgroundTailNote(0, "make world &", sid); !strings.Contains(n, "ALREADY") {
		t.Fatalf("a relaunch moments later is the case this warns about:\n%s", n)
	}
	// Aged by an ABSOLUTE amount, not by the window itself. Written as
	// `now.Add(-bgInFlightWindow - time.Minute)` this passes for any window at all, including one
	// set to a thousand years: the planted timestamp slides out with the constant and the record
	// is always just past the edge. Measured — that version of this test let a window of
	// 1000000h through green, which is the defect it exists to catch. A day is the policy: a
	// detach from yesterday is not something in flight, whatever the constant says.
	bgLaunched.mu.Lock()
	bgLaunched.m[string(sid)]["make"] = time.Now().Add(-24 * time.Hour)
	bgLaunched.mu.Unlock()
	if n := backgroundTailNote(0, "make world &", sid); strings.Contains(n, "ALREADY") {
		t.Errorf("a detach from a day ago is not evidence that anything is in flight:\n%s", n)
	}

	// And the map does not become a place sessions go and never leave. A session whose every
	// record has expired is gone from it, not merely holding an empty set.
	gone := freshSession(t)
	backgroundTailNote(0, "sleepy &", gone)
	bgLaunched.mu.Lock()
	bgLaunched.m[string(gone)]["sleepy"] = time.Now().Add(-24 * time.Hour)
	bgLaunched.mu.Unlock()
	backgroundTailNote(0, "make world &", freshSession(t)) // any call sweeps
	bgLaunched.mu.Lock()
	_, still := bgLaunched.m[string(gone)]
	bgLaunched.mu.Unlock()
	if still {
		t.Errorf("session %q kept its key after everything in it expired", gone)
	}
}
