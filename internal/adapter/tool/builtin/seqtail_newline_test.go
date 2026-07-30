package builtin

import (
	"strings"
	"testing"
)

// A NEWLINE separates commands exactly as `;` does, and a list's exit status is its LAST command's
// either way. Only `;` used to count, so the same masking written across two lines was invisible.
//
// Observed live (cancel-async-tasks, 2026-07-30): the command below came back `exit 0` while its
// own output carried `Exit code: 130`. Written with a `;` it was flagged; written with a newline
// it was not.
func TestANewlineSeparatesCommandsJustAsASemicolonDoes(t *testing.T) {
	live := "timeout 5 python3 << 'EOF'\nimport asyncio\nprint(1)\nEOF\necho \"Exit code: $?\""

	note := sequencedTailNote(0, live)
	if note == "" {
		t.Fatalf("the reported exit is the echo's, not python's:\n%s", live)
	}
	if !strings.Contains(note, `echo "Exit code: $?"`) {
		t.Errorf("the note names whose exit code this is:\n%s", note)
	}
	// It must not claim a `;` the command does not contain.
	if strings.Contains(note, "`;` segment") {
		t.Errorf("there is no `;` in this command:\n%s", note)
	}
	if !strings.Contains(note, "the last line") {
		t.Errorf("name the separator that was actually used:\n%s", note)
	}
	if !ExitCodeMasked(live) {
		t.Error("magi's own churn accounting must not take this exit 0 as the command's")
	}

	// The `;` spelling keeps its own wording.
	semi := `make world > log 2>&1; echo "exit=$?"`
	if n := sequencedTailNote(0, semi); !strings.Contains(n, "the last `;` segment") {
		t.Errorf("the semicolon form still names a segment:\n%s", n)
	}

	for _, c := range []struct {
		name string
		cmd  string
		want bool
	}{
		// The shape [[exit-masked-churn-reset]] was written about, one newline instead of one `;`.
		{"build then echo on its own line", "make world > log 2>&1\necho \"exit=$?\"", true},
		{"build then tail", "cargo build 2> e.log\ntail -20 e.log", true},
		{"trailing blank lines", "make world\n\necho done\n", true},
		// A real command after the reporter means the exit is THAT command's. Excluding `\n` from
		// the segment body is what keeps this false — before, the body ran past the newline and
		// named the echo while `./run-tests` was the one that actually set the status.
		{"reporter then a real command", "make world\necho done\n./run-tests", false},
		{"semicolon reporter then a real line", "make world; echo done\n./run-tests", false},
		// A newline is `;`, not `&&`: a short-circuiting tail keeps the primary's failure visible.
		{"&& is control flow", "make world && echo ok", false},
		// Multi-line scripts that end in real work are not masked.
		{"plain two-line build", "cd /app\nmake world", false},
		{"heredoc that ends the command", "cat > s.sh <<'EOF'\necho hi\nEOF", false},
	} {
		if got := ExitCodeMasked(c.cmd); got != c.want {
			t.Errorf("%s: ExitCodeMasked(%q) = %v, want %v", c.name, c.cmd, got, c.want)
		}
	}
}

// ExitCodeMasked is the guard-facing half of the same judgement, and it was the one reader still
// matching RAW text. Every note was moved onto maskNonShell after
// `python3 -c "print('done | tail -3')"` was read as a pipeline; this predicate kept the bug, so
// that command told magi's churn accounting its exit 0 belonged to a pager.
func TestTheGuardPredicateReadsShellTextLikeTheNotesDo(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  string
		want bool
	}{
		{"a pipe inside a quoted string", `python3 -c "print('done | tail -3')"`, false},
		{"a reporter inside a quoted string", `python3 -c "x=1; echo done"`, false},
		{"a mask inside a heredoc body being written", "cat > s.sh <<'EOF'\nmake x || true\nEOF", false},
		{"a mask in a trailing comment", "make world  # fallback: make x || true", false},
		// Still true where the operator is real shell text.
		{"a real masking tail", "make world || true", true},
		{"a real swallowing pipe", "make world 2>&1 | tail -50", true},
		{"a real sequenced reporter", `make world > log 2>&1; echo "exit=$?" >> log`, true},
	} {
		if got := ExitCodeMasked(c.cmd); got != c.want {
			t.Errorf("%s: ExitCodeMasked(%q) = %v, want %v", c.name, c.cmd, got, c.want)
		}
	}
}
