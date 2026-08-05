package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// outcomeFor finds one path's result, so a test names what it is asserting about.
func outcomeFor(t *testing.T, out []RestoreOutcome, path string) RestoreOutcome {
	t.Helper()
	for _, r := range out {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("%s is not in the report at all — a path the child touched must be accounted for: %+v", path, out)
	return RestoreOutcome{}
}

// The journal puts content back, with no git anywhere in sight.
//
// This is the layer that has to work on a machine with no git and in a directory that is not a
// repository, because those are ordinary and magi does not install anything to change them.
func TestAFailedChildsEditsGoBackWithoutGit(t *testing.T) {
	a, dir := restoreApp(t)
	write(t, dir, "keep.go", "original\n")
	sid := session.SessionID("child-1")

	j := a.journalFor(sid, dir)
	j.note("keep.go", "original\n", true, true) // edited
	j.note("new.go", "", false, false)          // created
	j.note("gone.go", "was here\n", true, true) // deleted by the child
	write(t, dir, "keep.go", "the child's version\n")
	write(t, dir, "new.go", "brand new\n")

	out := a.RestoreChild(context.Background(), sid)

	if got := read(t, dir, "keep.go"); got != "original\n" {
		t.Errorf("the edited file came back as %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.go")); !os.IsNotExist(err) {
		t.Error("the file the child created is still there")
	}
	if got := read(t, dir, "gone.go"); got != "was here\n" {
		t.Errorf("the deleted file came back as %q, want its original contents", got)
	}
	for _, p := range []string{"keep.go", "new.go", "gone.go"} {
		if r := outcomeFor(t, out, p); !r.Restored {
			t.Errorf("%s reported as not restored: %s", p, r.Reason)
		}
	}
	// Nothing here needed git, and the report says which layer did the work.
	if r := outcomeFor(t, out, "keep.go"); r.How != "journal" {
		t.Errorf("the content restore was attributed to %q, want journal", r.How)
	}
	if r := outcomeFor(t, out, "new.go"); r.How != "saga" {
		t.Errorf("removing a created file was attributed to %q, want saga", r.How)
	}
}

// What could NOT be put back is named, with a reason. A half restore reported as a clean one is
// worse than no restore: the next round builds on a tree it believes is clean.
func TestWhatCannotBePutBackIsSaidSo(t *testing.T) {
	a, dir := restoreApp(t)
	sid := session.SessionID("child-2")
	// A path magi never held the contents of — too large to snapshot, or a directory.
	a.journalFor(sid, dir).note("blob.bin", "", false, true)

	out := a.RestoreChild(context.Background(), sid)
	r := outcomeFor(t, out, "blob.bin")
	if r.Restored {
		t.Fatal("a file whose contents magi never held was reported as restored")
	}
	if strings.TrimSpace(r.Reason) == "" {
		t.Error("it was reported as not restored with no reason — the caller cannot act on that")
	}
	if !strings.Contains(r.Reason, "git") {
		t.Errorf("the reason does not say whether git could have helped: %q", r.Reason)
	}
}

// git recovers a tracked file magi could not snapshot — and only that file. The child shares the
// parent's tree, so anything wider would throw away the user's own uncommitted work.
func TestGitRecoversATrackedFileAndLeavesTheRestAlone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed — the layer under test is the one that needs it")
	}
	a, dir := restoreApp(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write(t, dir, "tracked.bin", "committed\n")
	git("add", "tracked.bin")
	git("commit", "-qm", "base")

	// The USER's own uncommitted work, which magi was never asked to touch.
	write(t, dir, "mine.txt", "the user is in the middle of this\n")

	sid := session.SessionID("child-3")
	// The child changed a file magi could not snapshot (readable=false).
	a.journalFor(sid, dir).note("tracked.bin", "", false, true)
	write(t, dir, "tracked.bin", "the child's version\n")

	out := a.RestoreChild(context.Background(), sid)
	if got := read(t, dir, "tracked.bin"); got != "committed\n" {
		t.Errorf("git did not put the tracked file back: %q", got)
	}
	if r := outcomeFor(t, out, "tracked.bin"); !r.Restored || r.How != "git" {
		t.Errorf("attributed to %q (restored=%v), want git", r.How, r.Restored)
	}
	// The user's file is untouched. A reset --hard or a stash would have taken it.
	if got := read(t, dir, "mine.txt"); got != "the user is in the middle of this\n" {
		t.Errorf("the restore destroyed the user's own uncommitted work: %q", got)
	}
}

// A restore happens once. Replaying it against a tree the caller has since moved on from would
// undo work that is no longer the child's.
func TestARestoreIsNotReplayed(t *testing.T) {
	a, dir := restoreApp(t)
	sid := session.SessionID("child-4")
	a.journalFor(sid, dir).note("f.go", "original\n", true, true)
	write(t, dir, "f.go", "child\n")

	a.RestoreChild(context.Background(), sid)
	write(t, dir, "f.go", "what the PARENT did afterwards\n")

	if out := a.RestoreChild(context.Background(), sid); len(out) != 0 {
		t.Errorf("a second restore reported %d paths, want none", len(out))
	}
	if got := read(t, dir, "f.go"); got != "what the PARENT did afterwards\n" {
		t.Errorf("the second restore undid work that was not the child's: %q", got)
	}
}

func restoreApp(t *testing.T) (*App, string) {
	t.Helper()
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	_ = parent
	return a, t.TempDir()
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(b)
}

// The journal is filled by a REAL child run, not only by tests calling note().
//
// Every layer above is exercised against a journal a test wrote by hand. If nothing on the tool
// path ever wrote one, all of it would pass and none of it would work — the defect this session
// has found twenty times over.
func TestARealChildsWritesLandInTheJournal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target.go"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, parent, _ := spawnApp(t, &writingChildLLM{path: filepath.Join(dir, "target.go")})
	parent.Workdir = dir

	spawn, _, restore := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")
	res, err := spawn(context.Background(), port.SpawnSpec{
		Prompt: "change it", Tools: []string{"write", "read"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if got := read(t, dir, "target.go"); got != "the child's version\n" {
		t.Fatalf("the child did not write the file (got %q) — the test proves nothing", got)
	}

	out, err := restore(context.Background(), res.SessionID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("a child that wrote a file left an empty journal — nothing on the tool path fills it")
	}
	if got := read(t, dir, "target.go"); got != "before\n" {
		t.Errorf("the child's write was not put back: %q", got)
	}
}

// writingChildLLM makes the child write one file and then answer.
type writingChildLLM struct {
	n    int
	path string
}

func (f *writingChildLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	if f.n == 0 {
		args, _ := json.Marshal(map[string]string{"path": f.path, "content": "the child's version\n"})
		ch <- port.ProviderEvent{Type: port.ProviderToolCall,
			ToolCall: &session.ToolCall{CallID: "w1", Name: "write", Args: args}}
	} else {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "done"}
	}
	f.n++
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}
