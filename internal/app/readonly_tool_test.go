package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
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
