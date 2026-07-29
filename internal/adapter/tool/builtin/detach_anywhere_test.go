package builtin

import (
	"strings"
	"testing"
)

// A `&` detaches wherever it stands, not only at the end. Observed live (fix-ocaml-gc,
// 2026-07-29): the agent backgrounded a build on one line and ran a probe on the next, so the
// exit 0 it was handed belonged to the probe and the build had no status anywhere — no job entry,
// no exit, no per-stage numbers. The note that exists to say precisely that never fired, because
// the old predicate required the `&` to be the command's last character.
func TestDetachIsFoundWhereverItStands(t *testing.T) {
	const live = "cd /app/ocaml && make world 2>&1 &\nsleep 60 && ps aux | grep \"make\""
	note := backgroundTailNote(0, live, "s1")
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
		got := backgroundTailNote(0, c.cmd, "s2") != ""
		if got != c.want {
			t.Errorf("%s: %q → note=%v, want %v", c.what, c.cmd, got, c.want)
		}
	}

	// A nonzero exit says something happened on its own; the note is for the exit that does not.
	if backgroundTailNote(1, "make world &", "s3") != "" {
		t.Error("a failing command's status is already the news")
	}

	// The duplicate warning names the DETACHED program, not what ran in the foreground after it.
	first := backgroundTailNote(0, "make world & sleep 5", "dup")
	second := backgroundTailNote(0, "make world & sleep 5", "dup")
	if strings.Contains(first, "ALREADY started") {
		t.Errorf("the first launch is not a duplicate:\n%s", first)
	}
	if !strings.Contains(second, "`make` was ALREADY started") {
		t.Errorf("the second must name make, not sleep:\n%s", second)
	}
}
