package app

import (
	"strings"
	"testing"
)

// git's own machine-readable form, read as the five things a person acts on.
//
// Parsed rather than shelled out to in a test: what is worth checking is the reading — that a
// rename names the file to open, that a path staged AND changed since says so (committing now
// would commit half of what is on screen), and that a detached head is not drawn as a branch.
func TestGitStatusIsReadAsWhatAPersonActsOn(t *testing.T) {
	got := parseGitStatus(strings.Join([]string{
		"# branch.oid 4a1e9c2b8f0d1122334455667788990011223344",
		"# branch.head engine-ui-split",
		"# branch.upstream origin/engine-ui-split",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 aaa bbb cmd/magi-web/page.js",
		"1 M. N... 100644 100644 100644 aaa bbb internal/app/git.go",
		"1 MM N... 100644 100644 100644 aaa bbb docs/UI.md",
		"2 R. N... 100644 100644 100644 aaa bbb R100 cmd/magi-web/access.go\tcmd/magi-web/people.go",
		"u UU N... 100644 100644 100644 100644 aaa bbb ccc internal/core/cluster/cluster.go",
		"? scratchpad/notes.md",
	}, "\n"))

	if !got.Repo || got.Branch != "engine-ui-split" || got.Upstream != "origin/engine-ui-split" {
		t.Fatalf("the branch line read as %+v", got)
	}
	if got.Ahead != 2 || got.Behind != 1 {
		t.Errorf("ahead/behind is %d/%d", got.Ahead, got.Behind)
	}
	want := map[string]string{
		"cmd/magi-web/page.js":             "unstaged",
		"internal/app/git.go":              "staged",
		"docs/UI.md":                       "both",
		"cmd/magi-web/access.go":           "staged",
		"internal/core/cluster/cluster.go": "conflict",
		"scratchpad/notes.md":              "untracked",
	}
	if len(got.Changes) != len(want) {
		t.Fatalf("%d changes: %+v", len(got.Changes), got.Changes)
	}
	for _, c := range got.Changes {
		if kind, ok := want[c.Path]; !ok {
			t.Errorf("a path came out as %q", c.Path)
		} else if kind != c.Kind {
			t.Errorf("%s is %q and git said %q", c.Path, c.Kind, kind)
		}
	}

	// A detached head is a state, not a branch: git writes "(detached)" where the name goes, and a
	// screen printing that under the word "branch" teaches somebody the wrong thing in exactly the
	// state where it costs work.
	off := parseGitStatus("# branch.oid 4a1e9c2b8f0d1122\n# branch.head (detached)\n")
	if off.Branch != "" || off.Head != "4a1e9c2b" {
		t.Errorf("a detached head read as %+v", off)
	}

	// And a directory nobody put under version control is not an error: no repo, nothing to show.
	if none := (GitState{}); none.Repo {
		t.Error("the zero value claims to be a checkout")
	}
}
