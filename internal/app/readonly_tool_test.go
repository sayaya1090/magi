package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

// The console reads a workspace through the companion's own tools, and only the ones that look.
//
// The allowlist is here, in the process that owns the workspace, rather than in whatever is in
// front of it: there is more than one thing in front of it — a console, a relay, a peer — and only
// one of these. A caller that could name `bash` would have a shell in somebody's workspace through
// a door built for reading a file.
func TestOnlyToolsThatLookCanBeRunOutsideATurn(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "note.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The real registry, because what is under test is which of the REAL tools may be run
	// this way. An empty one would pass the refusals for the wrong reason.
	a := New(nil, nil, builtin.Default(), bus.New(), nil, Config{})

	// The directory, which is what a file tree is built from: JSON entries, each with a name and
	// whether it is one.
	dir, err := a.ReadOnlyTool(context.Background(), wd, "list", json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("listing the workspace: %v", err)
	}
	if !strings.Contains(dir, "note.txt") {
		t.Errorf("the listing came back as %q", dir)
	}

	out, err := a.ReadOnlyTool(context.Background(), wd, "read", json.RawMessage(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatalf("reading a file in the workspace: %v", err)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("the file came back as %q", out)
	}

	// Not a shell — and asked with arguments that would LEAVE EVIDENCE, because a refusal and a
	// tool that merely complained about empty arguments look identical from the return value.
	if _, err := a.ReadOnlyTool(context.Background(), wd, "bash",
		json.RawMessage(`{"command":"touch ran-through-the-reading-door"}`)); err == nil {
		t.Error("bash ran through a door built for reading")
	}
	if _, err := os.Stat(filepath.Join(wd, "ran-through-the-reading-door")); err == nil {
		t.Fatal("bash RAN: a shell in somebody's workspace through a door built for reading a file")
	}
	// Nor an edit, for the same reason and with the same evidence.
	if _, err := a.ReadOnlyTool(context.Background(), wd, "write",
		json.RawMessage(`{"path":"written.txt","content":"x"}`)); err == nil {
		t.Error("write ran through a door built for reading")
	}
	if _, err := os.Stat(filepath.Join(wd, "written.txt")); err == nil {
		t.Fatal("write RAN: the console can put files in a workspace")
	}
	// And the rest of what is not looking.
	for _, name := range []string{"edit", "multiedit", "ask_user", "spawn", "todowrite"} {
		if _, err := a.ReadOnlyTool(context.Background(), wd, name, json.RawMessage(`{}`)); err == nil {
			t.Errorf("%q ran through a door built for reading", name)
		}
	}

	// And the workspace is the edge of it. The tools confine paths themselves; this checks that
	// what they refuse still arrives as a refusal rather than as content.
	if _, err := a.ReadOnlyTool(context.Background(), wd, "read",
		json.RawMessage(`{"path":"../../../etc/passwd"}`)); err == nil {
		t.Error("a path outside the workspace was read")
	}
}

// The secret-path deny that stops the AGENT reading .env / keys guards the console/relay/peer path
// too. That path (ReadOnlyTool/WriteTool) reaches the same tools but never passes through
// gatePermission, where Decide is consulted — so without a deny floor a teammate granted only `read`
// could fetch workspace secrets the more-trusted agent is refused. Reverting denyFloor lets these
// reads through.
func TestSecretPathsAreDeniedOnTheConsolePath(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, ".env"), []byte("SECRET=shh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "note.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, serr := jsonl.New(t.TempDir())
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), nil, Config{})
	ctx := context.Background()

	if _, err := a.ReadOnlyTool(ctx, wd, "read", json.RawMessage(`{"path":".env"}`)); err == nil {
		t.Error("the console read .env — the secret-path deny does not guard the read path")
	}
	// Case variation, because the deny is case-folded and the read tool resolves case-insensitively.
	if _, err := a.ReadOnlyTool(ctx, wd, "read", json.RawMessage(`{"path":".ENV"}`)); err == nil {
		t.Error("the console read .ENV — the fold does not reach the console path")
	}
	// An ordinary file still reads: the floor denies secrets, not everything.
	if _, err := a.ReadOnlyTool(ctx, wd, "read", json.RawMessage(`{"path":"note.txt"}`)); err != nil {
		t.Errorf("an ordinary file was refused through the read path: %v", err)
	}
	// The write path denies a secret too (and, being the same floor, the guardrail globs).
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteTool(ctx, sid, wd, "write", json.RawMessage(`{"path":".env","content":"x"}`), false); err == nil {
		t.Error("the console wrote .env — the secret-path deny does not guard the write path")
	}
}

// An edit made from a console is written into the companion's log as the person's own words.
//
// This is the condition the whole write path exists under. Without it the next turn overwrites the
// change or builds on it, and nothing anywhere says a person was in there — the agent's context
// still holds the file as it was. Orca and Paseo answer the same problem with a worktree per agent
// and git as the record; magi has one workspace per companion and a log, so the log is where it
// goes.
func TestAConsoleEditIsRecordedInTheCompanionsOwnLog(t *testing.T) {
	wd := t.TempDir()
	dir := t.TempDir()
	st, serr := jsonl.New(dir)
	if serr != nil {
		t.Fatal(serr)
	}
	a := New(st, nil, builtin.Default(), bus.New(), nil, Config{})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.WriteTool(context.Background(), sid, wd, "write",
		json.RawMessage(`{"path":"note.txt","content":"a person wrote this\n"}`), false); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if b, rerr := os.ReadFile(filepath.Join(wd, "note.txt")); rerr != nil || !strings.Contains(string(b), "a person") {
		t.Fatalf("the file is %q (%v)", string(b), rerr)
	}

	// The record: the person's own words in that session's log, which is the transcript a reader
	// scrolls and the context the next turn is built from.
	evs, rerr := st.Read(context.Background(), sid, 0)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var said string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			said += string(e.Data)
		}
	}
	if !strings.Contains(said, "note.txt") {
		t.Errorf("the companion was not told which file a person changed: %q", said)
	}

	// And the note is not an open turn. Everything that reads a log decides "is this one mid-turn"
	// from the last prompt with no finish after it — so without saying otherwise, saving a file
	// made the fleet row read as Working while nothing at all was running.
	if _, open := a.UnfinishedTurnOf(context.Background(), sid); open {
		t.Error("saving a file left the companion looking mid-turn")
	}

	// And nothing was started by it. Saving a file is not asking for work, and a console that
	// launched a turn because somebody pressed save would be a second surprise on top of the first.
	if _, running := a.Running(); running {
		t.Error("saving a file started a turn")
	}

	// The write door is not the read door: bash is refused here too, and a tool that only looks is
	// not what this one is for.
	if _, err := a.WriteTool(context.Background(), sid, wd, "bash",
		json.RawMessage(`{"command":"touch through-the-edit-door"}`), false); err == nil {
		t.Error("bash ran through the editing door")
	}
	if _, err := os.Stat(filepath.Join(wd, "through-the-edit-door")); err == nil {
		t.Fatal("bash RAN through the editing door")
	}
}
