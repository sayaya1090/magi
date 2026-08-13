package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
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

// The four git commands the console offers, and the one of them that is written into the log.
//
// stage, unstage and commit move git's own state and leave the working tree as it was — git's
// history is their record. discard THROWS AWAY what was in a file, and the agent's context still
// holds the version that is now gone, so it is written down exactly like a console edit.
func TestGitCommandsAreFourAndDiscardIsRecorded(t *testing.T) {
	wd := t.TempDir()
	dir := t.TempDir()
	st, serr := jsonl.New(dir)
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), platform.OS{}, Config{})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if _, xerr := (platform.OS{}).Exec(context.Background(),
			port.Cmd{Path: "git", Args: args, Dir: wd}); xerr != nil {
			t.Skipf("git is not usable here: %v", xerr)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	run("add", "-A")
	run("commit", "-qm", "first")

	// A change, staged from the console.
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("two\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stage", "a.txt", ""); gerr != nil {
		t.Fatalf("staging: %v", gerr)
	}
	if got, _ := a.GitFacts(context.Background(), wd); len(got.Changes) != 1 || got.Changes[0].Kind != "staged" {
		t.Fatalf("after staging: %+v", got.Changes)
	}
	// Nothing in the log: staging moved git's state and left the file alone.
	if said := userSaid(t, st, sid); said != "" {
		t.Errorf("staging was written into the log: %q", said)
	}

	// And discarding is. The file goes back, and the companion is told, because what it holds in
	// context is the version that has just gone.
	if _, gerr := a.GitDo(context.Background(), sid, wd, "unstage", "a.txt", ""); gerr != nil {
		t.Fatalf("unstaging: %v", gerr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "discard", "a.txt", ""); gerr != nil {
		t.Fatalf("discarding: %v", gerr)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); strings.TrimSpace(string(b)) != "one" {
		t.Errorf("the file is %q", string(b))
	}
	if said := userSaid(t, st, sid); !strings.Contains(said, "a.txt") {
		t.Errorf("throwing the file away was not written into the log: %q", said)
	}

	// The list is closed. Anything else somebody wants from git they have a terminal for.
	for _, what := range []string{"push", "reset", "clean", "checkout", "rm"} {
		if _, gerr := a.GitDo(context.Background(), sid, wd, what, "a.txt", ""); gerr == nil {
			t.Errorf("%q ran from a screen offering four commands", what)
		}
	}
	// And a filename is a filename, not an argument: `--force` is a file called --force.
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stage", "--force", ""); gerr == nil {
		t.Error("a path that looks like a flag was taken as one")
	}
}

func userSaid(t *testing.T, st *jsonl.Store, sid session.SessionID) string {
	t.Helper()
	evs, err := st.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			out += string(e.Data)
		}
	}
	return out
}
