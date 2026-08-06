package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// spawnApp builds an app and a parent session, with a council stubbed so the child's finish path is
// exercised against a real one (the point of TestAChildDoesNotConveneACouncil below).
func spawnApp(t *testing.T, llm port.LLMProvider) (*App, session.Session, *fakeCouncil) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.sessionInfo(ctx, sid), fc
}

// The seam is unreachable unless something calls it, and nothing in the tree does: no builtin tool
// takes a Spawn, and magi ships no agent. This is the property that lets the seam exist at all
// without contradicting the record that removed delegation — so it is asserted, not assumed.
func TestNothingInTheTreeSpawns(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	ctx := context.Background()

	before, _ := a.store.Read(ctx, parent.ID, 0)
	// Every builtin, executed with the env the loop builds. None may reach for Spawn.
	for _, name := range a.ToolNames() {
		if _, ok := a.tools.Get(name); !ok {
			t.Errorf("%s is advertised but not registered", name)
		}
	}
	after, _ := a.store.Read(ctx, parent.ID, 0)
	if len(before) != len(after) {
		t.Errorf("merely listing the tools wrote %d events", len(after)-len(before))
	}
	// And no child session exists: CreateSession is the only way to make one and nothing called it.
	for _, e := range after {
		if e.Type != event.TypeSessionCreated {
			continue
		}
		var d event.SessionCreatedData
		if json.Unmarshal(e.Data, &d) == nil && d.Parent != "" {
			t.Errorf("a child session exists with no spawner: parent=%s", d.Parent)
		}
	}
}

// A child runs to completion in its own session and hands back its final text.
func TestAChildRunsInItsOwnSessionAndReturnsItsText(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "the child's answer"})
	res, err := a.spawnChild(context.Background(), parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		port.SpawnSpec{System: "you are a reviewer", Prompt: "REVIEW the parser"}, nil)
	if err != nil {
		t.Fatalf("spawnChild: %v", err)
	}
	if res.Err != "" {
		t.Errorf("child reported %q", res.Err)
	}
	if !strings.Contains(res.Text, "the child's answer") {
		t.Errorf("the child's text did not come back: %q", res.Text)
	}
	if res.SessionID == "" || res.SessionID == string(parent.ID) {
		t.Fatalf("the child must have its own session, got %q", res.SessionID)
	}
	// It carries the parent link, which is what keeps it out of the resume list.
	child := session.SessionID(res.SessionID)
	evs, _ := a.store.Read(context.Background(), child, 0)
	var created event.SessionCreatedData
	for _, e := range evs {
		if e.Type == event.TypeSessionCreated {
			_ = json.Unmarshal(e.Data, &created)
		}
	}
	if created.Parent != string(parent.ID) {
		t.Errorf("child's parent = %q, want %q", created.Parent, parent.ID)
	}
	// The task was seeded VERBATIM — the first defect charged against the removed machinery was a
	// brief paraphrased until the graded identifier was gone.
	if !promptContains(t, a, child, "REVIEW the parser") {
		t.Error("the child's prompt was not seeded verbatim")
	}
}

// The child inherits the parent's scratch pointer. Without it scratchFor returns nil for the child
// and every tool it runs is handed empty log/tmp paths — a failure that shows up only inside tools.
func TestAChildInheritsTheParentsScratch(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	sc := newTurnScratch()
	a.setScratch(parent.ID, sc)

	res, err := a.spawnChild(context.Background(), parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		port.SpawnSpec{Prompt: "do it"}, nil)
	if err != nil {
		t.Fatalf("spawnChild: %v", err)
	}
	if got := a.scratchFor(session.SessionID(res.SessionID)); got != sc {
		t.Errorf("the child got scratch %v, want the parent's %v", got, sc)
	}
}

// A child cannot spawn. The env it runs under has no Spawn at all, so recursion is impossible by
// construction rather than bounded by a counter somebody has to remember to check.
func TestAChildCannotSpawn(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	// depth 1 is what spawnChild passes to runLoop; the env builder keys off exactly that.
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	if fn, _, _, _ := a.spawnFnFor(1, parent, actor, "c1", "t"); fn != nil {
		t.Error("a child was handed a Spawn hook — it could spawn its own child")
	}
	if fn, _, _, _ := a.spawnFnFor(0, parent, actor, "c1", "t"); fn == nil {
		t.Error("the top level must have one, or nothing can spawn at all")
	}
}

// Cancelling the parent cancels the child. The child's context is derived from the parent's, so a
// user interrupt does not leave a child writing to the store behind the scenes.
func TestCancellingTheParentCancelsTheChild(t *testing.T) {
	a, parent, _ := spawnApp(t, &blockingLLM{started: make(chan struct{}), release: make(chan struct{})})
	llm := a.llm.(*blockingLLM)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan port.SpawnResult, 1)
	go func() {
		res, _ := a.spawnChild(ctx, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
			port.SpawnSpec{Prompt: "hang"}, nil)
		done <- res
	}()
	<-llm.started
	cancel()
	select {
	case res := <-done:
		if res.Err == "" {
			t.Error("a cancelled child must say why it stopped, not report success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the child outlived its parent's cancellation")
	}
	close(llm.release)
}

// blockingLLM holds the first request open until released, so a test can cancel mid-flight.
type blockingLLM struct {
	started chan struct{}
	release chan struct{}
	once    bool
}

func (f *blockingLLM) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if !f.once {
		f.once = true
		close(f.started)
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "done"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A child never convenes a council.
//
// The council is the gate on ending the USER's turn: it reads the user's task, the plan, and a
// fresh look at the workspace, and judges whether the work is done. A child answers to the tool
// that spawned it, and the parent turn that call belongs to has its own declaration to make. The
// removed machinery carried the same guard; it went with the last thing that ran at depth>0, and
// requireFinishDeclaration has had no depth test since.
func TestAChildDoesNotConveneACouncil(t *testing.T) {
	llm := &toolThenTextLLM{}
	a, parent, fc := spawnApp(t, llm)

	res, err := a.spawnChild(context.Background(), parent, event.Actor{Kind: event.ActorAgent, ID: "coder"},
		port.SpawnSpec{Prompt: "read something then answer"}, nil)
	if err != nil {
		t.Fatalf("spawnChild: %v", err)
	}
	// The gate's observable is the DEMAND, not a council call: requireFinishDeclaration injects a
	// "call the council tool with complete: true" prompt and keeps the turn going. A child that
	// gets one is being held open for a gate that is not its to pass.
	child := session.SessionID(res.SessionID)
	if promptContains(t, a, child, "A turn ends by declaring it") {
		t.Error("the child was told to declare completion to a council — that gate belongs to the user's turn")
	}
	fc.mu.Lock()
	calls := fc.calls
	fc.mu.Unlock()
	if calls != 0 {
		t.Errorf("the child convened the council %d time(s)", calls)
	}
}

// toolThenTextLLM runs one tool and then answers, so the turn counts as having done work — which is
// the condition requireFinishDeclaration keys on.
type toolThenTextLLM struct{ n int }

func (f *toolThenTextLLM) StreamChat(_ context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	if f.n == 0 {
		f.n++
		ch <- port.ProviderEvent{Type: port.ProviderToolCall,
			ToolCall: &session.ToolCall{CallID: "c1", Name: "list", Args: json.RawMessage(`{"path":"."}`)}}
	} else {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "child answer"}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// The model a user set in /subagents is the model the child actually runs on. Recording the
// override is not the same as applying it, and only one of the two is what a user asked for.
func TestTheUsersModelReachesTheChild(t *testing.T) {
	llm := &modelRecordingLLM{}
	a, parent, _ := spawnApp(t, llm)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}

	// No override: the child runs on what the plugin asked for.
	spawn, _, _, _ := a.spawnFnFor(0, parent, actor, "c1", "seele_plan")
	if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "plan it", Model: "plugin-model"}); err != nil {
		t.Fatal(err)
	}
	if got := llm.last(); got != "plugin-model" {
		t.Errorf("with no override the child should run on the plugin's model, got %q", got)
	}

	// With one, the user's wins.
	if err := a.SetSubagentModel("seele_plan", "user-model", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "plan it", Model: "plugin-model"}); err != nil {
		t.Fatal(err)
	}
	if got := llm.last(); got != "user-model" {
		t.Errorf("the user's model must reach the child, got %q", got)
	}
}

// modelRecordingLLM answers immediately and remembers which model it was asked for.
type modelRecordingLLM struct {
	mu     sync.Mutex
	models []string
}

func (f *modelRecordingLLM) StreamChat(_ context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.models = append(f.models, r.Model)
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "done"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *modelRecordingLLM) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.models) == 0 {
		return ""
	}
	return f.models[len(f.models)-1]
}
