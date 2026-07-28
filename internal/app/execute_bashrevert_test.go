package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	a := New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"})
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
