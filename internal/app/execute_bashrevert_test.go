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
)

// TestBashRestoreLoopKeepsTheProgressWindowClimbing drives REAL bash commands through executeTool,
// because the guard machinery this relies on was already correct and merely unreachable from the
// bash path — a unit test on the guard alone would have passed before the fix too. The shape is the
// one observed live: back up, edit, restore, edit, restore. The net effect of each restore is a
// file state the turn already held, so it must not buy the run a fresh progress window.
func TestBashRestoreLoopKeepsTheProgressWindowClimbing(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"}))
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err := os.WriteFile(filepath.Join(wd, "heap.c"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard()
	run := func(cmd string) {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"command": cmd})
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, &session.ToolCall{
			CallID: "c_" + newID(), Name: "bash", Args: args,
		}, guard)
	}

	run("cp heap.c heap.c.bak")
	if guard.mutationEpoch() == 0 {
		t.Fatal("precondition: a cp must register as a bash mutation")
	}
	// The first patch is REAL progress — a state this file has never held — so it earns a fresh
	// window. That is the baseline the loop then has to climb away from.
	run("sed -i.tmp 's/original/patched/' heap.c && rm -f heap.c.tmp")
	guard.mu.Lock()
	since0 := guard.sinceProgress
	guard.mu.Unlock()
	if since0 != 0 {
		t.Fatalf("precondition: a genuinely new version restarts the progress window, got %d", since0)
	}

	// Now the loop: restore→re-patch, over and over. Every command differs from the one before it,
	// so the command-text idempotence check inside mutated() cannot see it — each swing looked like
	// a brand-new deliverable version and zeroed both windows, which is how the run stayed one step
	// from the threshold forever and burned its whole budget here. The content read is what sees it,
	// so the windows must now CLIMB straight through the loop.
	for i := 0; i < 18; i++ {
		run("cp heap.c.bak heap.c")
		run("sed -i.tmp 's/original/patched/' heap.c && rm -f heap.c.tmp")
	}

	guard.mu.Lock()
	since := guard.sinceProgress
	guard.mu.Unlock()
	if since == 0 {
		t.Error("the progress window must climb across a restore loop, got sinceProgress=0")
	}

	// The control, in the same run: a bash edit to a state the file has never held IS progress and
	// restarts the window, so this cannot be mistaken for "bash mutations stopped counting".
	run("sed -i.tmp 's/patched/brand-new/' heap.c && rm -f heap.c.tmp")
	guard.mu.Lock()
	since = guard.sinceProgress
	guard.mu.Unlock()
	if since != 0 {
		t.Errorf("a genuinely new version must restart the window, got sinceProgress=%d", since)
	}
}

// Driven through executeTool for the same reason as the test above: the defect is at the seam, and
// a unit test on noteEdit alone passes either way. Observed live on the restarted fix-ocaml-gc:
// `rm -rf _build && make world … || true` — _build never existed, so the before and after reads both
// came back empty and the result carried "[self-edit check] this write left the file byte-for-byte
// as it already was" about a command that neither wrote nor deleted a thing.
func TestRemovingAPathThatNeverExistedSaysNothing(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"}))
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard()
	run := func(cmd string) string {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"command": cmd})
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, &session.ToolCall{
			CallID: "c_" + newID(), Name: "bash", Args: args,
		}, guard)
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		var last string
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type != event.TypePartAppended || json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				last = string(d.Part.ToolResult.Content)
			}
		}
		return last
	}

	// A real file must still register, so the epoch is armed exactly as it was live.
	run("echo hi > kept.txt")
	if out := run("rm -rf _build"); strings.Contains(out, "self-edit check") {
		t.Errorf("removing a path that never existed is not a rewrite of anything:\n%s", out)
	}
	// The check still fires for what it exists to catch: a mutation whose net effect returns a file
	// to a state this turn already held. (An IDENTICAL command text is not a new mutation at all, so
	// it never reaches the content comparison — the swing has to be written two different ways.)
	run("printf 'A\\n' > f.txt")
	run("sed -i.tmp 's/A/B/' f.txt && rm -f f.txt.tmp")
	if out := run("sed -i.bak 's/B/A/' f.txt && rm -f f.txt.bak"); !strings.Contains(out, "self-edit check") {
		t.Errorf("a mutation that restores a state the turn already held must be reported:\n%s", out)
	}
}
