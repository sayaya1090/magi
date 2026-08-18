package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Two children that each write their OWN checkout have nothing to race over either, so a step that
// asks for two isolated subagents runs both at once — the ReadOnlyChildren bargain, reached by
// isolation instead of by taking writing away.
func TestIsolatedChildrenRunTogether(t *testing.T) {
	reg := builtin.NewRegistry()
	reg.Register(metaTool{name: "worker", meta: port.ToolMetadata{Subagent: true, IsolatedChildren: true}})
	reg.Register(metaTool{name: "builder", meta: port.ToolMetadata{Subagent: true}})
	a := &App{cfg: Config{}, tools: reg}
	a.cfg = a.cfg.withDefaults()

	calls := func(names ...string) []*session.ToolCall {
		out := make([]*session.ToolCall, 0, len(names))
		for _, n := range names {
			out = append(out, &session.ToolCall{Name: n})
		}
		return out
	}
	if !a.allParallelSafe(calls("worker", "worker")) {
		t.Error("two isolated children are still being run one after the other")
	}
	if !a.allParallelSafe(calls("worker", "read")) {
		t.Error("an isolated child beside a read is not parallel-safe")
	}
	// A subagent with neither declaration is what it always was: alone.
	if a.allParallelSafe(calls("worker", "builder")) {
		t.Error("a batch with an undeclared subagent in it must serialise")
	}
}

// The declaration is applied, not believed: a writing child spawned by an IsolatedChildren tool
// gets its own clone whether or not the spec repeated workspace="clone". Proven from the failure
// side — in a directory with no repository the clone cannot be made, and the spawn that tried it
// says so — and from the reader side: a child that can only look keeps the shared tree.
func TestAnIsolatedToolsWritingChildGetsItsOwnCheckout(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	a.tools.Register(metaTool{name: "worker", meta: port.ToolMetadata{Subagent: true, IsolatedChildren: true}})

	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "worker")
	// The parent's workdir is a bare temp directory: the forced clone must fail loudly rather
	// than fall back to sharing — that failure is the proof the isolation was applied.
	res, err := spawn(context.Background(), port.SpawnSpec{Prompt: "fix it",
		Tools: []string{"read", "write", "bash"}})
	if err == nil || !strings.Contains(err.Error(), "isolated workspace needs git") {
		t.Fatalf("a writing child under an isolated tool did not get the clone applied: res=%+v err=%v", res, err)
	}
	// A reader shares the tree, so no clone is attempted and the same directory is fine.
	if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "look around",
		Tools: []string{"read", "grep"}}); err != nil {
		t.Fatalf("a reading child under an isolated tool was forced into a clone: %v", err)
	}
}

// In a real repository the whole shape holds together: the child works in its own checkout, is told
// so where it reads, and its log is filed under the PARENT's project — a child keyed by its temp
// clone landed where no child listing ever scanned, and vanished from every view.
func TestAnIsolatedChildWorksInItsOwnCheckoutAndIsStillListed(t *testing.T) {
	llm := &usageLLM{text: "done"}
	a, seed, _ := spawnApp(t, llm)
	a.tools.Register(metaTool{name: "worker", meta: port.ToolMetadata{Subagent: true, IsolatedChildren: true}})

	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v: %s", err, out)
		}
	}
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: repo})
	if err != nil {
		t.Fatal(err)
	}
	parent := a.sessionInfo(ctx, sid)
	_ = seed

	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "worker")
	res, err := spawn(ctx, port.SpawnSpec{Prompt: "fix it", Tools: []string{"read", "write", "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Workspace == "" || res.Workspace == repo {
		t.Fatalf("the child did not get its own checkout: workspace=%q", res.Workspace)
	}
	if res.BaseCommit == "" {
		t.Error("an isolated child's work must be a commit range, and there is no baseline")
	}
	if sys := llm.lastSys(); !strings.Contains(sys, "your own checkout") {
		t.Errorf("the child was confined without being told — its system prompt says nothing:\n%s", sys)
	}
	// The Project routing: the child's log belongs to the parent's project, so the parent's child
	// listing finds it even though the child worked in a temp clone.
	kids, err := a.ChildSessions(ctx, repo, string(parent.ID))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range kids {
		if string(k.ID) == res.SessionID {
			found = true
		}
	}
	if !found {
		t.Errorf("the isolated child %s is missing from its parent's child listing (%d listed)",
			res.SessionID, len(kids))
	}
}

// The override may tighten, never loosen.
func TestEffectiveSandboxNeverLoosens(t *testing.T) {
	for _, tc := range []struct{ global, override, want string }{
		{"", "workspace-write", "workspace-write"},     // unconfined default yields to the child's isolation
		{"full", "workspace-write", "workspace-write"}, // explicit unconfined yields too: the isolation is the child's contract
		{"read-only", "workspace-write", "read-only"},  // a stricter config stays in charge
		{"workspace-write", "workspace-write", "workspace-write"},
		{"", "", ""}, // no override: whatever the config says
	} {
		if got := effectiveSandbox(tc.global, tc.override); got != tc.want {
			t.Errorf("effectiveSandbox(%q, %q) = %q, want %q", tc.global, tc.override, got, tc.want)
		}
	}
}

// slotLLM blocks every completion until released, and reports each start — what a concurrency
// bound is observed with.
type slotLLM struct {
	started chan struct{}
	release chan struct{}
}

func (g *slotLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "done"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// The gate: children queue on the slot instead of all running at once. With one slot and two
// children, the second must not start until the first is released.
func TestSpawnGateBoundsConcurrentChildren(t *testing.T) {
	old := spawnMaxParallel
	spawnMaxParallel = 1
	t.Cleanup(func() { spawnMaxParallel = old })

	llm := &slotLLM{started: make(chan struct{}, 4), release: make(chan struct{})}
	a, parent, _ := spawnApp(t, llm)
	a.tools.Register(metaTool{name: "scout", meta: port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}})

	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "scout")
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := spawn(context.Background(), port.SpawnSpec{Prompt: "look", Tools: []string{"read"}})
			results <- err
		}()
	}
	<-llm.started // the first child is running
	select {
	case <-llm.started:
		t.Fatal("a second child started while the only slot was held")
	case <-time.After(150 * time.Millisecond):
		// held, as it should be
	}
	close(llm.release) // let the first finish and the second through
	<-llm.started
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Errorf("spawn %d: %v", i, err)
		}
	}
}
