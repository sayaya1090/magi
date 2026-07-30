package app

import (
	"reflect"
	"testing"
)

// The redirect scan has now had to learn three forms of "this is not shell text", and it learned
// them one live incident at a time: quoted runs (a Python line reading `data[8:12] if len(data) >=
// 12` made the record claim files named `=` and `12`), heredoc bodies (C++ through `cat > f <<'EOF'`
// produced `heap_size)` and `allocate(size)`), and now a `#` comment.
//
// The cost is the same each time and it is not cosmetic. The paths feed two things: magi's own
// record of what the run wrote — which the council reads as what exists now, and which the
// not-on-disk-now check stats — and noteBashWrite, which books a write as a real mutation, bumping
// the epoch and zeroing the stall window. A command that touched nothing bought a fresh window.
func TestARedirectInACommentIsNotARedirect(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  string
		want []string
	}{
		{"trailing comment names a path", "make world  # writes > out.txt", nil},
		{"comment after an inspect command", "grep -n x f.c   # see notes > /tmp/n.txt", nil},
		{"comment on its own line", "make world\n# capture > log.txt", nil},
		{"a real redirect BEFORE a comment still counts", "make world > log  # writes > out.txt", []string{"log"}},
		{"a real redirect AFTER a commented line still counts", "# note > a.txt\nmake world > log", []string{"log"}},
		// `#` inside a word is not a comment — the shared rule, not a second copy of it.
		{"hash inside a word", "cp file#1.txt out#2.txt > log", []string{"log"}},
		{"hash in a parameter expansion", "echo ${x#y} > log", []string{"log"}},
		// The two forms already learned, kept honest.
		{"quoted run", `python3 -c "print('x > y.txt')"`, nil},
		{"heredoc body", "cat > s.sh <<'EOF'\necho hi > /etc/passwd\nEOF", []string{"s.sh"}},
		{"plain redirect", "make world > log 2>&1", []string{"log"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := redirectTargets(c.cmd); !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
				t.Errorf("redirectTargets(%q) = %v, want %v", c.cmd, got, c.want)
			}
		})
	}

	// The consumer that matters: a phantom write must not be booked as progress.
	g := newRunGuard()
	before := g.callCount()
	_ = before
	if authored, _ := g.noteBashWrite("make world  # writes > out.txt"); authored {
		t.Error("a comment mentioning a path is not a file this command authored")
	}
	if authored, _ := g.noteBashWrite("make world > log 2>&1"); !authored {
		t.Error("a real redirect is still a write")
	}
}
