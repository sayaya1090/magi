package app

import (
	"strings"
	"testing"
)

// The record's `changed:` line is what the council reads as what exists now, and what the
// not-on-disk-now check stats. Everything in it must be a path the agent actually wrote.
//
// Live command (cobol-modernization, 2026-07-29). The agent asked python to describe a 15-byte
// file; magi read the quoted Python body as shell text, found `>` inside `>=`, and filed the two
// tokens after it as written files. The record then said the run had written `=` and `12`.
func TestQuotedBodyIsNotShellText(t *testing.T) {
	const live = `python3 -c "
data = open('/app/src/INPUT.DAT', 'rb').read()
# INPUT-RECORD: buyer_id(4) + seller_id(4) + book_id(4) + amount(10) = 22 bytes
buyer_id = data[0:4] if len(data) >= 4 else data[:4].ljust(4, b' ')
book_id = data[8:12] if len(data) >= 12 else data[8:12].ljust(4, b' ')
"`
	if got := bashWritePaths(live); len(got) != 0 {
		t.Errorf("a quoted script body writes nothing magi can name, got %q", got)
	}
	for _, junk := range []string{"=", "12"} {
		if got := redirectTargets(live); hasTarget(got, junk) {
			t.Errorf("%q is not a file the agent wrote: %q", junk, got)
		}
	}

	// A real redirect still lands, including one whose target is itself quoted, and `>>` still
	// counts once. This is the half that must not regress: the scan is now quote-aware, not blind.
	for _, c := range []struct {
		cmd  string
		want string
	}{
		{`make 2>&1 | tee build.log > out.txt`, "out.txt"},
		{`echo hi > "spaced.txt"`, "spaced.txt"},
		{`md5sum data/*.DAT >> after_cobol.md5`, "after_cobol.md5"},
		// The agent's own typo: bash really does create a file named `after` here, so the record
		// naming it is correct — magi reports the shell's behaviour, not the intent.
		{`md5sum data/*.DAT > after cobol.md5`, "after"},
	} {
		got := redirectTargets(c.cmd)
		if !hasTarget(got, c.want) {
			t.Errorf("%s: want %q among targets, got %q", c.cmd, c.want, got)
		}
	}

	// An apostrophe inside a double-quoted body must not be read as opening a quote of its own —
	// that would resync the scanner mid-command and hand back junk again.
	const apost = `python3 -c "print('it\'s fine')" > real.txt`
	if got := redirectTargets(apost); !hasTarget(got, "real.txt") {
		t.Errorf("a redirect after a quoted body is still a redirect, got %q", got)
	}
}

func hasTarget(xs []string, want string) bool {
	for _, x := range xs {
		if strings.Trim(x, `"'`) == want {
			return true
		}
	}
	return false
}

// A heredoc body is unquoted text the shell hands to a program verbatim, so quote-skipping alone
// does not reach it. Observed live (custom-memory-heap-crash, 2026-07-29): C++ written through
// `cat > /app/user.cpp << 'EOF'` put `heap_size)`, `(heap_memory)`, `allocate(size)` and
// `deallocate(ptr)` into the record's changed: line — four files nobody wrote, in the block the
// council reads as what exists now.
func TestAHeredocBodyIsNotShellText(t *testing.T) {
	const live = "cat > /app/user.cpp << 'EOF'\n" +
		"void* CustomHeapManager::allocate(size_t size) {\n" +
		"  if (size > heap_size) return nullptr;\n" +
		"  char* p = static_cast<char*>(heap_memory) + offset;\n" +
		"  return p;\n" +
		"}\nEOF"
	got := bashWritePaths(live)
	if !hasTarget(got, "/app/user.cpp") {
		t.Errorf("the heredoc's own destination is a real write: %q", got)
	}
	for _, junk := range []string{"heap_size)", "(heap_memory)", "allocate(size)", "deallocate(ptr)", "char*"} {
		if hasTarget(got, junk) {
			t.Errorf("%q is C++, not a path magi wrote: %q", junk, got)
		}
	}
	if len(got) != 1 {
		t.Errorf("one destination, the file the heredoc feeds: %q", got)
	}

	// A redirect AFTER the terminator is real again — the body ends where the tag says it does.
	after := bashWritePaths("cat > a.txt << 'EOF'\nx > y\nEOF\nmake 2>&1 > build.log")
	for _, want := range []string{"a.txt", "build.log"} {
		if !hasTarget(after, want) {
			t.Errorf("want %q among %q", want, after)
		}
	}
	if hasTarget(after, "y") {
		t.Errorf("`x > y` was inside the body: %q", after)
	}

	// An arithmetic left-shift is not a heredoc intro and must not swallow the rest.
	if got := bashWritePaths("echo $((1<<2)) > shifted.txt"); !hasTarget(got, "shifted.txt") {
		t.Errorf("`<<` here is a shift, not a heredoc: %q", got)
	}
}
