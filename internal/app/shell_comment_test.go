package app

import (
	"strings"
	"testing"
)

// A trailing comment is shell text that does nothing, and every operand scan in this file read its
// words as file paths. `rm -rf build # clean up the tree` named six: build, and then #, clean, up,
// the, tree. redirectTargets has stopped at a `#` since it was written — these never did.
//
// Both directions of that are wrong, and the second one is the dangerous one. bashWritePaths fed
// the self-revert check five destinations that were never there, which is what its own doc says
// must not happen ("inventing one would retract real progress"). bashMoveSources fed the SAME five
// into a child's restore journal, recorded as paths that did not exist before the child ran — and
// that is the one journal shape restoreOne answers with os.Remove. Restoring such a child would
// delete a file called `clean`, `up`, `the` or `tree` that the child never touched, and report
// Restored: true for it, in a restore whose stated contract is that what could not be put back is
// named.
func TestATrailingCommentIsNotAListOfFiles(t *testing.T) {
	for _, c := range []struct {
		cmd   string
		write []string
		move  []string
	}{
		{"rm -rf build # clean up the tree", []string{"build"}, []string{"build"}},
		{"rm -rf build #clean", []string{"build"}, []string{"build"}},
		{"mv a.txt b.txt # rename it", []string{"b.txt"}, []string{"a.txt"}},
		{"touch f.txt # create the marker", []string{"f.txt"}, nil},
		// A comment runs to the end of the LINE, so a `;` inside one starts nothing. Splitting on
		// the `;` first would have made `rm y` a command the child never issued.
		{"rm x # note; rm y", []string{"x"}, []string{"x"}},
		// …and it ends there: the next line is code again.
		{"# put the old one aside\nmv old.go old.go.bak", []string{"old.go.bak"}, []string{"old.go"}},
	} {
		if got := bashWritePaths(c.cmd); strings.Join(got, ",") != strings.Join(c.write, ",") {
			t.Errorf("bashWritePaths(%q) = %v, want %v", c.cmd, got, c.write)
		}
		if got := bashMoveSources(c.cmd); strings.Join(got, ",") != strings.Join(c.move, ",") {
			t.Errorf("bashMoveSources(%q) = %v, want %v", c.cmd, got, c.move)
		}
	}
}

// The other direction: a `#` that is not a comment must survive. A quoted one is data, and one in
// the middle of a word is part of that word — a URL fragment, or a filename somebody chose.
func TestAHashThatIsNotACommentSurvives(t *testing.T) {
	for _, c := range []struct{ cmd, want string }{
		{`echo "a # b" > f.txt`, "f.txt"},
		{`echo 'x' > 'a#b.txt'`, "a#b.txt"},
		{"curl http://h/p#frag > out.txt", "out.txt"},
		// Mid-word, unquoted, and reached through the segment scan rather than the redirect scan:
		// a filename somebody chose with a `#` in it is one token, not a command and a comment.
		{"cp a#1.txt b#2.txt", "b#2.txt"},
	} {
		got := bashWritePaths(c.cmd)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("bashWritePaths(%q) = %v, want [%s]", c.cmd, got, c.want)
		}
	}
	// A command whose only `#` is inside quotes still runs, and the segment after it is still a
	// segment.
	segs := splitShellSegments("git commit -m 'fix #123' && touch done.txt")
	if len(segs) != 2 {
		t.Fatalf("the quoted issue number is not a comment: %q", segs)
	}
	if got := bashWritePaths("git commit -m 'fix #123' && touch done.txt"); len(got) != 1 || got[0] != "done.txt" {
		t.Errorf("the touch after it still names its file, got %v", got)
	}
}

// bashMoveSources was the one function in shellcmd.go at zero coverage — measured across every
// package that can reach internal/app — while the twin it says it shares parsing rules with was at
// 100%. It answers the question a restore asks and bashWritePaths cannot: not "what did this
// write" but "what is now missing", because a moved file's old name appears in no destination list.
func TestMoveSourcesNamesWhatWentAwayAndGuessesAtNothing(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"mv old.txt new.txt", []string{"old.txt"}},
		{"mv a#1.txt b.txt", []string{"a#1.txt"}},
		{"cd x && mv a b", []string{"a"}},
		{"rm -f stale.log", []string{"stale.log"}},
		{"mv a.txt b.txt && rm c.txt", []string{"a.txt", "c.txt"}},
		// A copy takes nothing away. bashWritePaths names its destination; there is no source to
		// put back.
		{"cp a.txt b.txt", nil},
		// Many-to-one, recursive, or glob: the per-file sources cannot be named from the text, and
		// naming the wrong one is worse than naming none — the journal it feeds deletes and
		// overwrites by path.
		{"mv a b c/", nil},
		{"mv -r src dst", nil},
		{"mv *.go dst", nil},
		{"git mv a b", nil},
	} {
		if got := bashMoveSources(c.cmd); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("bashMoveSources(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
	// The same cap the destination list has: a command naming more paths than magi will track is
	// truncated rather than refused, because the first ones are still worth putting back.
	wide := make([]string, bashWriteCap+5)
	for i := range wide {
		wide[i] = "f" + string(rune('a'+i%26)) + ".txt"
	}
	if got := bashMoveSources("rm " + strings.Join(wide, " ")); len(got) != bashWriteCap {
		t.Errorf("a wide rm is capped at %d, got %d", bashWriteCap, len(got))
	}
}
