package app

import "testing"

// splitShellSegments is the shared tokenizer every command classifier (isInspectOnly,
// isWaitCommand, redirectsToFile) is built on, yet had no direct test. It splits on the shell
// control operators, dropping empty segments; multi-char operators must win over their single-char
// prefix (&& is one separator, not two &).
func TestSplitShellSegments(t *testing.T) {
	cases := []struct {
		cmd  string
		want int
	}{
		{"cmd", 1},
		{"a && b", 2},
		{"a || b", 2},          // || before | : one separator
		{"a | b | c", 3},       // pipes
		{"a & b", 2},           // background
		{"a\nb", 2},            // newline
		{"a ; ; b", 2},         // empty middle segment dropped
		{"a && b || c ; d", 4}, // mixed operators
		{"", 0},                // nothing
		{"  ", 0},              // whitespace only
		{"a &&& b", 2},         // && then & → empty middle dropped
	}
	for _, c := range cases {
		if got := len(splitShellSegments(c.cmd)); got != c.want {
			t.Errorf("splitShellSegments(%q) = %d segments, want %d (%v)", c.cmd, got, c.want, splitShellSegments(c.cmd))
		}
	}
}

// redirectsToFile decides whether a bash command writes a file (→ noteBashWrite counts it as
// progress). It must count `>`/`>>`/tee/heredoc but NOT fd-duplication (`2>&1`, `>&1`) or a /dev
// sink, which write no real artifact.
func TestRedirectsToFile(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"echo hi > f.txt", true},
		{"echo hi >> log", true},         // append
		{"make | tee out.log", true},     // tee
		{"cat <<EOF\nbody\nEOF", true},   // heredoc authors content
		{"pytest 2>&1", false},           // fd duplication, not a file
		{"./run >&1", false},             // fd dup
		{"sort data > /dev/null", false}, // /dev sink
		{"grep foo bar", false},          // no redirect
		{"a | b", false},                 // pipe, not a file write
		{"go build ./...", false},        // plain execution
	}
	for _, c := range cases {
		if got := redirectsToFile(c.cmd); got != c.want {
			t.Errorf("redirectsToFile(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}
