package builtin

import "testing"

// The four shapes the two hand-rolled copies of this scan got wrong. Each of them made a command's
// own text lie about itself: a body read as shell text put operators in front of the model that
// nobody typed, and an opener that never terminated hid the ones that were.
func TestHeredocBodiesAreNotShellText(t *testing.T) {
	for _, c := range []struct {
		name, cmd string
		strip     string // what is left after the bodies come out
		detach    int    // index of the detaching `&`, -1 for none
	}{{
		name:   "two heredocs on one line: the shell reads both bodies",
		cmd:    "cat > a <<'A' && cat > b <<'B'\nalpha & one\nA\nbeta & two\nB\ndone",
		strip:  "cat > a <<'A' && cat > b <<'B'\ndone",
		detach: -1,
	}, {
		name:   "a plain << does not end on an indented terminator",
		cmd:    "cat > f.c <<'EOF'\nint main(){\n  EOF\n  a & b;\n}\nEOF\nmake",
		strip:  "cat > f.c <<'EOF'\nmake",
		detach: -1,
	}, {
		name:   "<<- does, but only for tabs",
		cmd:    "cat > f <<-EOF\nbody & here\n\tEOF\nmake &",
		strip:  "cat > f <<-EOF\nmake &",
		detach: 37,
	}, {
		name:   "a quoted << opens nothing",
		cmd:    "echo \"shift << left\"\nx & y",
		strip:  "echo \"shift << left\"\nx & y",
		detach: 23,
	}, {
		name:   "a commented << opens nothing",
		cmd:    "# see notes << here\nmake world &\nhere",
		strip:  "# see notes << here\nmake world &\nhere",
		detach: 31,
	}, {
		name:   "<<< is a here-string: no body, no terminator",
		cmd:    "grep x <<<word\nmake world &",
		strip:  "grep x <<<word\nmake world &",
		detach: 26,
	}, {
		name:   "an arithmetic left-shift is not an intro",
		cmd:    "x=$((1<<4))\nmake &",
		strip:  "x=$((1<<4))\nmake &",
		detach: 17,
	}, {
		name:   "and a real heredoc still hides its body, with the `&` after it still found",
		cmd:    "cat > s.py <<'PY'\nif a & b: pass\nPY\npython3 s.py &",
		strip:  "cat > s.py <<'PY'\npython3 s.py &",
		detach: 49,
	}, {
		name:   "an unterminated heredoc swallows the rest, as the shell would",
		cmd:    "cat > f <<EOF\nbody & more",
		strip:  "cat > f <<EOF",
		detach: -1,
	}} {
		t.Run(c.name, func(t *testing.T) {
			if got := StripHeredocBodies(c.cmd); got != c.strip {
				t.Errorf("strip:\n got %q\nwant %q", got, c.strip)
			}
			if got := detachIndex(c.cmd); got != c.detach {
				t.Errorf("detachIndex = %d, want %d", got, c.detach)
			}
		})
	}
}

// HasHeredoc answers the question "does this command author file content", and the same four
// shapes must not make it say yes.
func TestHasHeredocIsAboutRealHeredocsOnly(t *testing.T) {
	yes := []string{"cat > f <<EOF\nx\nEOF", "cat > f <<-'TAG'\nx\n\tTAG", "tee f <<EOF\nx\nEOF"}
	no := []string{"x=$((1<<4))", "grep x <<<word", "echo \"a << b\"", "# note << here", "ls -la"}
	for _, c := range yes {
		if !HasHeredoc(c) {
			t.Errorf("%q opens a heredoc", c)
		}
	}
	for _, c := range no {
		if HasHeredoc(c) {
			t.Errorf("%q does not open a heredoc", c)
		}
	}
}
