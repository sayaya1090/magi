package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func phaseOf(system string) string {
	for _, p := range []string{"LOCALIZE", "IMPLEMENT", "VERIFY", "REVIEW", "SUMMARIZE"} {
		if strings.Contains(system, "PHASE: "+p) {
			return strings.ToLower(p)
		}
	}
	return "?"
}

func toolNames(specs []port.ToolSpec) map[string]bool {
	m := map[string]bool{}
	for _, s := range specs {
		m[s.Name] = true
	}
	return m
}

// workflowLLM records the phase + offered tools of each call and acts per phase:
// implement emits a write (a real edit) unless implementNoEditOnce is armed for
// the first implement call.
type workflowLLM struct {
	mu                 sync.Mutex
	phases             []string // phase of each call, in order
	toolsByPhase       map[string]map[string]bool
	implementCalls     int
	implementNoEditFst bool
}

func (f *workflowLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ph := phaseOf(r.System)
	f.mu.Lock()
	f.phases = append(f.phases, ph)
	if f.toolsByPhase == nil {
		f.toolsByPhase = map[string]map[string]bool{}
	}
	if _, ok := f.toolsByPhase[ph]; !ok {
		f.toolsByPhase[ph] = toolNames(r.Tools)
	}
	lastWasToolResult := false // reflects the kind of the very last message part
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			lastWasToolResult = p.Kind == session.PartToolResult
		}
	}
	implementN := 0
	if ph == "implement" {
		f.implementCalls++
		implementN = f.implementCalls
	}
	f.mu.Unlock()

	ch := make(chan port.ProviderEvent, 4)
	emitText := func(s string) {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: s}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
	}
	if ph == "implement" && !lastWasToolResult {
		// First call of the implement phase: edit (unless armed to skip once).
		if f.implementNoEditFst && implementN == 1 {
			emitText("I will not edit anything.") // triggers the no-edit gate
			return ch, nil
		}
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_w", Name: "write", Args: json.RawMessage(`{"path":"fix.txt","content":"patched"}`),
		}}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
		return ch, nil
	}
	emitText("phase " + ph + " done")
	return ch, nil
}

func newWorkflowApp(t *testing.T, llm port.LLMProvider, plat port.Platform, cfg Config) (*App, session.SessionID, string) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), plat, cfg)
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	return a, sid, wd
}

// The pipeline runs phases in order and enforces per-phase tool restrictions:
// localize can't edit, implement can, verify has bash.
func TestWorkflowPhaseOrderAndToolRestriction(t *testing.T) {
	llm := &workflowLLM{}
	a, sid, _ := newWorkflowApp(t, llm, nil, Config{Permission: "allow", System: "base"})
	if err := a.runWorkflow(context.Background(), a.sessionInfo(context.Background(), sid)); err != nil {
		t.Fatalf("workflow: %v", err)
	}

	// Distinct phases, in order.
	var order []string
	for _, p := range llm.phases {
		if len(order) == 0 || order[len(order)-1] != p {
			order = append(order, p)
		}
	}
	want := []string{"localize", "implement", "verify", "review", "summarize"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("phase order = %v, want %v", order, want)
	}
	// Tool restrictions.
	if llm.toolsByPhase["localize"]["write"] || llm.toolsByPhase["localize"]["edit"] {
		t.Error("localize must not offer edit/write tools")
	}
	if !llm.toolsByPhase["implement"]["edit"] || !llm.toolsByPhase["implement"]["write"] {
		t.Error("implement must offer edit/write tools")
	}
	if !llm.toolsByPhase["verify"]["bash"] {
		t.Error("verify must offer bash")
	}
	if llm.toolsByPhase["localize"]["task"] {
		t.Error("phases must not offer the task (delegation) tool")
	}
}

// scriptPlatform returns a scripted exit code per Exec call (for the verify gate).
type scriptPlatform struct {
	mu    sync.Mutex
	codes []int
	calls int
}

func (p *scriptPlatform) Exec(ctx context.Context, c port.Cmd) (port.ExecResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	code := 0
	if p.calls < len(p.codes) {
		code = p.codes[p.calls]
	}
	p.calls++
	return port.ExecResult{Stdout: []byte("verify output"), ExitCode: code}, nil
}
func (p *scriptPlatform) ConfigDir() string           { return "" }
func (p *scriptPlatform) DataDir() string             { return "" }
func (p *scriptPlatform) TerminalCaps() port.TermCaps { return port.TermCaps{} }

// The verify gate loops implement→verify until the verification command passes,
// bounded by WorkflowMaxLoops.
func TestWorkflowVerifyGateLoops(t *testing.T) {
	llm := &workflowLLM{}
	plat := &scriptPlatform{codes: []int{1, 1, 0}} // fail, fail, pass
	a, sid, _ := newWorkflowApp(t, llm, plat, Config{
		Permission: "allow", System: "base", VerifyCmd: "run-tests", WorkflowMaxLoops: 3,
	})
	if err := a.runWorkflow(context.Background(), a.sessionInfo(context.Background(), sid)); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if plat.calls != 3 {
		t.Errorf("verify command ran %d times, want 3 (fail,fail,pass)", plat.calls)
	}
	if llm.implementCalls < 3 {
		t.Errorf("implement phase entered %d times, want >=3 (one per verify attempt)", llm.implementCalls)
	}
	// It must still proceed to review + summarize after passing.
	if !contains2(llm.phases, "review") || !contains2(llm.phases, "summarize") {
		t.Errorf("expected review+summarize after verify passed; phases=%v", llm.phases)
	}
}

// The implement gate re-prompts when a phase makes no file edits.
func TestWorkflowImplementNoEditRetry(t *testing.T) {
	llm := &workflowLLM{implementNoEditFst: true}
	a, sid, _ := newWorkflowApp(t, llm, nil, Config{Permission: "allow", System: "base", WorkflowMaxLoops: 3})
	if err := a.runWorkflow(context.Background(), a.sessionInfo(context.Background(), sid)); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if llm.implementCalls < 2 {
		t.Errorf("expected implement to retry after making no edits, got %d calls", llm.implementCalls)
	}
}

func contains2(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
