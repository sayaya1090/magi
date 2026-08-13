package app

import (
	"context"
	"encoding/json"
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
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stage", "a.txt", "", false); gerr != nil {
		t.Fatalf("staging: %v", gerr)
	}
	if got, _ := a.GitFacts(context.Background(), wd); len(got.Changes) != 1 || got.Changes[0].Kind != "staged" {
		t.Fatalf("after staging: %+v", got.Changes)
	}
	// And the agent is told, because staging changes what a commit IT makes would capture. Every
	// mutation this console makes is written down — a rule with a carve-out is one somebody has to
	// remember, and the carve-out is always the case that later surprises a model.
	if said := userSaid(t, st, sid); !strings.Contains(said, "staged") {
		t.Errorf("staging was not written into the log: %q", said)
	}

	// And discarding is. The file goes back, and the companion is told, because what it holds in
	// context is the version that has just gone.
	if _, gerr := a.GitDo(context.Background(), sid, wd, "unstage", "a.txt", "", false); gerr != nil {
		t.Fatalf("unstaging: %v", gerr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "discard", "a.txt", "", false); gerr != nil {
		t.Fatalf("discarding: %v", gerr)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); strings.TrimSpace(string(b)) != "one" {
		t.Errorf("the file is %q", string(b))
	}
	if said := userSaid(t, st, sid); !strings.Contains(said, "a.txt") {
		t.Errorf("throwing the file away was not written into the log: %q", said)
	}

	// A commit, a stash and putting it back: the three the agent most needs to hear about, because
	// each one moves what it is reasoning about — HEAD, or the files it has read.
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("three\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stage", "a.txt", "", false); gerr != nil {
		t.Fatal(gerr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "commit", "", "a second commit", false); gerr != nil {
		t.Fatalf("committing: %v", gerr)
	}
	if said := userSaid(t, st, sid); !strings.Contains(said, "a second commit") {
		t.Errorf("the commit was not written into the log: %q", said)
	}
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("four\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stash", "", "", false); gerr != nil {
		t.Fatalf("stashing: %v", gerr)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); strings.TrimSpace(string(b)) != "three" {
		t.Errorf("after a stash the file is %q", string(b))
	}
	// The consequence, not just the fact: what the agent has read is not what is on disk.
	if said := userSaid(t, st, sid); !strings.Contains(said, "not what is on disk") {
		t.Errorf("the stash did not say what it means for the agent: %q", said)
	}
	if _, gerr := a.GitDo(context.Background(), sid, wd, "unstash", "", "", false); gerr != nil {
		t.Fatalf("putting the stash back: %v", gerr)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); strings.TrimSpace(string(b)) != "four" {
		t.Errorf("after putting the stash back the file is %q", string(b))
	}

	// The list is closed. Anything else somebody wants from git they have a terminal for.
	for _, what := range []string{"reset", "clean", "checkout", "rm", "rebase"} {
		if _, gerr := a.GitDo(context.Background(), sid, wd, what, "a.txt", "", false); gerr == nil {
			t.Errorf("%q ran from a screen offering four commands", what)
		}
	}
	// And a filename is a filename, not an argument: `--force` is a file called --force.
	if _, gerr := a.GitDo(context.Background(), sid, wd, "stage", "--force", "", false); gerr == nil {
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

// Asked for an answer, an idle companion gives one; not asked, it is only told.
//
// Both are wanted and neither is the default for the other: a console that always answered would
// spend a turn on every click of a stage button, and one that never did would make somebody who
// wants a second opinion on the change they just made go and type "look at it".
func TestAConsoleChangeCanAskForAnAnswerOrJustSayIt(t *testing.T) {
	wd, dir := t.TempDir(), t.TempDir()
	st, serr := jsonl.New(dir)
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), platform.OS{}, Config{})
	sid, cerr := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if cerr != nil {
		t.Fatal(cerr)
	}

	// Told, not asked: the note is in the log and the turn reads as closed, so nothing on any
	// screen says this companion is working.
	if _, err := a.WriteTool(context.Background(), sid, wd, "write",
		json.RawMessage(`{"path":"a.txt","content":"one\n"}`), false); err != nil {
		t.Fatal(err)
	}
	if _, open := a.UnfinishedTurnOf(context.Background(), sid); open {
		t.Error("a change nobody asked about left the companion looking mid-turn")
	}

	// Asked: the prompt stands as a prompt — not marked abandoned — which is what makes the loop
	// pick it up. There is no model in this test, so what is checked is the record, not the reply.
	if _, err := a.WriteTool(context.Background(), sid, wd, "write",
		json.RawMessage(`{"path":"a.txt","content":"two\n"}`), true); err != nil {
		t.Fatal(err)
	}
	evs, rerr := st.Read(context.Background(), sid, 0)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var prompts, abandoned int
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			prompts++
		case event.TypePromptAbandoned:
			abandoned++
		}
	}
	if prompts != 2 {
		t.Fatalf("%d notes were written for two changes", prompts)
	}
	if abandoned != 1 {
		t.Errorf("%d of them were marked abandoned; only the one nobody asked about should be", abandoned)
	}
}

// A diff is git's answer, not one this console works out.
//
// Renames, mode changes, binary files, what .gitattributes calls text: a screen that reimplemented
// any of it would show somebody something their repository does not agree with. So the three
// questions a person asks — what would a commit take, what have I changed since staging, what is
// in this new file — are three calls to git, and what comes back is passed through.
func TestADiffIsWhatGitSaysAboutOneFile(t *testing.T) {
	wd, dir := t.TempDir(), t.TempDir()
	st, serr := jsonl.New(dir)
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), platform.OS{}, Config{})
	run := func(args ...string) {
		t.Helper()
		if res, xerr := (platform.OS{}).Exec(context.Background(),
			port.Cmd{Path: "git", Args: args, Dir: wd}); xerr != nil || res.ExitCode != 0 {
			t.Skipf("git is not usable here: %v", xerr)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\ntwo\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	run("add", "-A")
	run("commit", "-qm", "first")

	// Changed and not staged.
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\nTWO\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	unstaged, err := a.GitDiffOf(context.Background(), wd, "a.txt", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unstaged, "-two") || !strings.Contains(unstaged, "+TWO") {
		t.Errorf("the unstaged diff is %q", unstaged)
	}
	// …and staged is a different question with a different answer: nothing is staged yet.
	if staged, serr := a.GitDiffOf(context.Background(), wd, "a.txt", true, false); serr != nil {
		t.Fatal(serr)
	} else if strings.TrimSpace(staged) != "" {
		t.Errorf("nothing is staged and the staged diff is %q", staged)
	}
	run("add", "a.txt")
	if staged, serr := a.GitDiffOf(context.Background(), wd, "a.txt", true, false); serr != nil {
		t.Fatal(serr)
	} else if !strings.Contains(staged, "+TWO") {
		t.Errorf("after staging, the staged diff is %q", staged)
	}

	// A file git does not know about has no diff, and gets one anyway: every line is an addition,
	// which is what a new file is. --no-index exits 1 to mean "they differ", which is the ordinary
	// case here rather than a failure.
	if werr := os.WriteFile(filepath.Join(wd, "new.txt"), []byte("fresh\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	fresh, ferr := a.GitDiffOf(context.Background(), wd, "new.txt", false, true)
	if ferr != nil {
		t.Fatalf("an untracked file: %v", ferr)
	}
	if !strings.Contains(fresh, "+fresh") {
		t.Errorf("a new file's diff is %q", fresh)
	}
}

// A save that arrives as a patch is also the check that nobody else moved the file.
//
// The whole-file save has nothing to disagree with: the last writer wins, silently. A patch carries
// the context around each change, so a file the agent edited while a person was typing no longer
// matches and git refuses — which turns a save that would have thrown work away into a sentence.
func TestAPatchRefusesWhenTheFileMovedUnderIt(t *testing.T) {
	wd, dir := t.TempDir(), t.TempDir()
	st, serr := jsonl.New(dir)
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), platform.OS{}, Config{})
	sid, cerr := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if cerr != nil {
		t.Fatal(cerr)
	}
	if res, xerr := (platform.OS{}).Exec(context.Background(),
		port.Cmd{Path: "git", Args: []string{"init", "-q"}, Dir: wd}); xerr != nil || res.ExitCode != 0 {
		t.Skipf("git is not usable here: %v", xerr)
	}
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}

	// The patch a console makes from what it opened and what was typed.
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if err := a.PatchFile(context.Background(), sid, wd, "a.txt", patch, false); err != nil {
		t.Fatalf("applying a patch to the file it was made from: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); !strings.Contains(string(b), "TWO") {
		t.Fatalf("the file is %q", string(b))
	}
	// And the companion is told, like every other change this console makes.
	if said := userSaid(t, st, sid); !strings.Contains(said, "a.txt") {
		t.Errorf("the patch was not written into the log: %q", said)
	}

	// Now somebody else changes the same lines — which is the agent, mid-turn — and the same patch
	// arrives from a person who has been typing since.
	if werr := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\nsomething else\nthree\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	err := a.PatchFile(context.Background(), sid, wd, "a.txt", patch, false)
	if err == nil {
		t.Fatal("a patch was applied over somebody else's change")
	}
	if !strings.Contains(err.Error(), "changed since you opened it") {
		t.Errorf("the refusal does not say what happened: %v", err)
	}
	// And it changed nothing on the way to refusing.
	if b, _ := os.ReadFile(filepath.Join(wd, "a.txt")); !strings.Contains(string(b), "something else") {
		t.Errorf("the refused patch still altered the file: %q", string(b))
	}

	// A path outside the workspace is refused HERE, before git is asked anything — git would refuse
	// it too, and only because this happens to be a checkout: the jail has to hold in a workspace
	// that is not one, so the refusal has to be ours.
	perr := a.PatchFile(context.Background(), sid, wd, "../escape.txt", patch, false)
	if perr == nil {
		t.Fatal("a patch named a path outside the workspace and was applied")
	}
	if !strings.Contains(perr.Error(), "outside this workspace") {
		t.Errorf("the path was refused by git rather than by the jail: %v", perr)
	}
}
