package builtin

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The notes that read a command to say what its LAST stage is used to match the raw string, so an
// operator the model never typed — one inside quotes, a comment, or a heredoc body being written to
// a file — fired them. The worst of the three does not merely add noise: it tells the agent the exit
// code in front of it belongs to a pager and cannot be trusted, about a command that has no
// pipeline at all.
//
// Left column: what fires. Right: what must stay silent, and why it looked like a match.
func TestCommandNotesReadShellTextOnly(t *testing.T) {
	const sid = session.SessionID("shelltext")
	fired := func(cmd string) []string {
		var out []string
		if maskingTailNote(0, cmd, false) != "" {
			out = append(out, "maskingTail")
		}
		if swallowingPipeNote(0, cmd, false) != "" {
			out = append(out, "swallowingPipe")
		}
		if sequencedTailNote(0, cmd) != "" {
			out = append(out, "sequencedTail")
		}
		return out
	}

	for _, c := range []struct{ name, cmd string }{
		{"a pipe inside a quoted python program", `python3 -c "print('done | tail -3')"`},
		{"a pipe inside a quoted path", `grep -r "x" "my | tail dir"`},
		{"a semicolon inside an awk program", `awk '{print; echo}' f.txt`},
		{"a masking tail inside quotes", `python3 -c "print('build || true')"`},
		{"a pipe in the heredoc body being WRITTEN", "cat > s.sh <<'EOF'\nmake x | tail -5\nEOF"},
		{"a semicolon in the heredoc body", "cat > s.sh <<'EOF'\nmake x ; echo done\nEOF"},
		{"a pipe in a trailing comment", "make world\n# then: make x | tail -5"},
	} {
		if got := fired(c.cmd); len(got) != 0 {
			t.Errorf("%s: nothing here is an operator, got %v\n  cmd: %s", c.name, got, c.cmd)
		}
	}

	// The real shapes each still fire, and only their own note.
	for _, c := range []struct {
		name, cmd, want string
	}{
		{"a real masking tail", `make world || true`, "maskingTail"},
		{"a real swallowing pipe", `make world 2>&1 | tail -50`, "swallowingPipe"},
		{"a real sequenced tail", `make world > log 2>&1 ; echo done`, "sequencedTail"},
		{"a real pipe AFTER a heredoc", "cat > s.sh <<'EOF'\nbody\nEOF\nmake world 2>&1 | tail -50", "swallowingPipe"},
	} {
		got := fired(c.cmd)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: want [%s], got %v\n  cmd: %s", c.name, c.want, got, c.cmd)
		}
	}

	// And the stage it names is quoted from the ORIGINAL text, not from the masked copy the match
	// was found in — the masking preserves length so the indices line up.
	note := swallowingPipeNote(0, `make world 2>&1 | tail -50`, false)
	if !strings.Contains(note, "tail -50") || strings.Contains(note, "xx") {
		t.Errorf("the named stage comes from the real command:\n%s", note)
	}
	// The session-keyed advisories read shell text too. ephemeralEnvNote matters most: it is
	// delivered ONCE per session, so a false fire spends the only delivery and the real export it
	// exists to warn about is never mentioned. All four of these fired before it was masked.
	for _, c := range []struct{ name, cmd string }{
		{"; export in a heredoc body", "cat > s.sh <<'EOF'\ncd /x ; export PATH=/y\nEOF"},
		{"| source in a heredoc body", "cat > s.sh <<'EOF'\nfoo | source bar\nEOF"},
		{"; export inside quotes", `python3 -c "print('a ; export B=1')"`},
		{"; export in a comment", "make world\n# note: ; export PATH=/x"},
	} {
		ephemeralNoted.mu.Lock()
		ephemeralNoted.m = map[string]bool{}
		ephemeralNoted.mu.Unlock()
		if ephemeralEnvNote(0, c.cmd, sid) != "" {
			t.Errorf("%s: no shell state was set, and this spends the session's one note", c.name)
		}
	}
	for _, c := range []string{
		"cat > s.sh <<'EOF'\n; ssh user@host\nEOF",
		`python3 -c "print('; ssh user@host')"`,
	} {
		if ptyNeededNote(c, false) != "" {
			t.Errorf("no ssh is about to run here: %s", c)
		}
	}

	// The session-keyed advisory notes are unaffected by any of this.
	ephemeralNoted.mu.Lock()
	ephemeralNoted.m = map[string]bool{}
	ephemeralNoted.mu.Unlock()
	if ephemeralEnvNote(0, `export PATH=/opt/bin:$PATH && which tool`, sid) == "" {
		t.Error("a real export still gets its note")
	}
	if ptyNeededNote(`ssh user@host uptime`, false) == "" {
		t.Error("a real ssh still gets its note")
	}
}

// maskNonShell keeps the string's length and newlines, which is what lets a match found in it be
// quoted out of the original.
func TestMaskNonShellPreservesLength(t *testing.T) {
	for _, cmd := range []string{
		`python3 -c "print('a | b')"`,
		"cat > f <<'EOF'\nbody | here\nEOF\nmake",
		"make world\n# a comment | tail",
		`echo 'single' && echo "double"`,
		"",
		"plain command with no quoting",
	} {
		got := maskNonShell(cmd)
		if len(got) != len(cmd) {
			t.Errorf("length changed: %d → %d for %q", len(cmd), len(got), cmd)
		}
		if strings.Count(got, "\n") != strings.Count(cmd, "\n") {
			t.Errorf("newlines changed for %q → %q", cmd, got)
		}
	}
	// An unterminated quote blanks to the end rather than leaving the tail as shell.
	if got := maskNonShell(`echo "open | tail`); strings.Contains(got, "| tail") {
		t.Errorf("an unclosed quote is still not shell text: %q", got)
	}
}
