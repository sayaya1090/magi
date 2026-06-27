package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeLLM returns a scripted sequence of responses, one per loop step.
type fakeLLM struct {
	mu    sync.Mutex
	steps [][]port.ProviderEvent
	call  int
}

func (f *fakeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 16)
	var evs []port.ProviderEvent
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	} else {
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}}
	}
	f.call++
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func textStep(s string) []port.ProviderEvent {
	return []port.ProviderEvent{{Type: port.ProviderText, Text: s}, {Type: port.ProviderFinish}}
}

func toolStep(name, args string) []port.ProviderEvent {
	return []port.ProviderEvent{
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_" + name, Name: name, Args: json.RawMessage(args)}},
		{Type: port.ProviderFinish},
	}
}

func newApp(t *testing.T, llm port.LLMProvider, cfg Config) (*App, string) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, llm, builtin.Default(), bus.New(), nil, cfg), t.TempDir()
}

// waitForTerminal subscribes and drains events until turn.finished or error.
func waitForTerminal(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	var got []event.Event
	for e := range ch {
		got = append(got, e)
		if e.Type == event.TypeTurnFinished || e.Type == event.TypeError {
			return got
		}
	}
	t.Fatal("stream ended before terminal event")
	return got
}

func countType(evs []event.Event, typ event.Type) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// F-LOOP-STOP loop-stop-1: text-only reply finishes in one step.
func TestLoopStopTextOnly(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("안녕")}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}}})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Errorf("loop-stop-1: expected 1 turn.finished, got events %v", typesOf(got))
	}
	if countType(got, event.TypePartAppended) != 1 {
		t.Errorf("loop-stop-1: expected 1 part.appended (text), got %d", countType(got, event.TypePartAppended))
	}
}

// F-LOOP-STOP loop-stop-2: tool-call then completion runs two steps.
func TestLoopToolThenFinish(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("write", `{"path":"out.txt","content":"hello"}`),
		textStep("완료"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "make a file"}}})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("loop-stop-2: expected turn.finished, got %v", typesOf(got))
	}
	// The write tool actually ran against the workdir.
	if b, err := readFile(wd, "out.txt"); err != nil || b != "hello" {
		t.Errorf("loop-stop-2: file content=%q err=%v, want hello", b, err)
	}
}

// F-LOOP-STOP loop-stop-3: infinite tool calls stop at MaxSteps.
func TestLoopMaxSteps(t *testing.T) {
	// Every step asks to read a (missing) file → never finishes on its own.
	steps := make([][]port.ProviderEvent, 10)
	for i := range steps {
		steps[i] = toolStep("read", `{"path":"nope.txt"}`)
	}
	llm := &fakeLLM{steps: steps}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 3})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "loop"}}})

	got := waitForTerminal(t, a, sid)
	last := got[len(got)-1]
	if last.Type != event.TypeError {
		t.Fatalf("loop-stop-3: expected error (max steps), got %q", last.Type)
	}
	var d event.ErrorData
	_ = json.Unmarshal(last.Data, &d)
	if d.Code != "max_steps" {
		t.Errorf("loop-stop-3: code=%q want max_steps", d.Code)
	}
}

// Loop guard: an agent repeating the SAME tool call is stopped well before
// MaxSteps, with a loop_guard error (self-verification against infinite loops).
func TestLoopGuardStopsRepetition(t *testing.T) {
	steps := make([][]port.ProviderEvent, 40)
	for i := range steps {
		steps[i] = toolStep("read", `{"path":"nope.txt"}`) // identical every step
	}
	llm := &fakeLLM{steps: steps}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "loop"}}})

	got := waitForTerminal(t, a, sid)
	last := got[len(got)-1]
	if last.Type != event.TypeError {
		t.Fatalf("loop-guard: expected error, got %q", last.Type)
	}
	var d event.ErrorData
	_ = json.Unmarshal(last.Data, &d)
	if d.Code != "loop_guard" {
		t.Errorf("loop-guard: code=%q want loop_guard (events=%v)", d.Code, typesOf(got))
	}
	// It must stop EARLY, not grind to MaxSteps: the LLM is called once per step,
	// and the guard trips after a handful of blocked repeats.
	if a.llm.(*fakeLLM).call >= 40 {
		t.Errorf("loop-guard: ran %d steps, expected an early stop", a.llm.(*fakeLLM).call)
	}
}

// Loop guard does NOT fire for distinct calls: varied tool args run to normal
// completion (no false positives on legitimate repeated tool use).
func TestLoopGuardAllowsDistinctCalls(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"a.txt"}`),
		toolStep("read", `{"path":"b.txt"}`),
		toolStep("read", `{"path":"c.txt"}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "read three"}}})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("loop-guard: distinct calls should finish normally, got %v", typesOf(got))
	}
}

// F-LOOP-PERMISSION perm-3: deny yields an error tool-result and the loop continues.
func TestPermissionDeny(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("write", `{"path":"x.txt","content":"hi"}`),
		textStep("ok"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "deny"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "write"}}})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("perm-3: expected turn.finished, got %v", typesOf(got))
	}
	// File must NOT exist (write was denied).
	if _, err := readFile(wd, "x.txt"); err == nil {
		t.Errorf("perm-3: file should not exist after deny")
	}
	if countType(got, event.TypePermissionDecided) != 1 {
		t.Errorf("perm-3: expected 1 permission.decided")
	}
}

// F-LOOP-PERMISSION perm-1 + perm-4: ask blocks; "always" auto-allows subsequent calls.
func TestPermissionAskAlways(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("write", `{"path":"a.txt","content":"1"}`),
		toolStep("write", `{"path":"b.txt","content":"2"}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "ask"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	// Subscribe first to observe permission.requested.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()

	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "write two"}}})

	requested := 0
	for e := range ch {
		if e.Type == event.TypePermissionRequested {
			requested++
			var d event.PermissionRequestedData
			_ = json.Unmarshal(e.Data, &d)
			// Answer "always" the first time; we should not be asked again.
			a.RespondPermission(context.Background(), command.RespondPermission{SessionID: sid, CallID: d.CallID, Decision: "always"})
		}
		if e.Type == event.TypeTurnFinished {
			break
		}
	}
	if requested != 1 {
		t.Errorf("perm-4: permission.requested count=%d, want 1 (always grant)", requested)
	}
	if b, _ := readFile(wd, "a.txt"); b != "1" {
		t.Errorf("perm: a.txt=%q want 1", b)
	}
	if b, _ := readFile(wd, "b.txt"); b != "2" {
		t.Errorf("perm: b.txt=%q want 2", b)
	}
}

// auto (accept-edits): file edits run without a prompt.
func TestPermissionAutoApprovesEdits(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("write", `{"path":"x.txt","content":"hi"}`),
		textStep("ok"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "auto"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "write"}}})

	got := waitForTerminal(t, a, sid)
	if n := countType(got, event.TypePermissionRequested); n != 0 {
		t.Errorf("auto: expected no permission.requested for edit, got %d", n)
	}
	if b, _ := readFile(wd, "x.txt"); b != "hi" {
		t.Errorf("auto: x.txt=%q want hi (edit should auto-run)", b)
	}
}

// auto (accept-edits): commands (bash) still prompt.
func TestPermissionAutoConfirmsCommands(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("bash", `{"command":"echo hi"}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "auto"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()

	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "run"}}})

	requested := 0
	for e := range ch {
		if e.Type == event.TypePermissionRequested {
			requested++
			var d event.PermissionRequestedData
			_ = json.Unmarshal(e.Data, &d)
			a.RespondPermission(context.Background(), command.RespondPermission{SessionID: sid, CallID: d.CallID, Decision: "allow"})
		}
		if e.Type == event.TypeTurnFinished {
			break
		}
	}
	if requested != 1 {
		t.Errorf("auto: bash should prompt; permission.requested=%d want 1", requested)
	}
}

func typesOf(evs []event.Event) []event.Type {
	out := make([]event.Type, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func readFile(dir, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	return string(b), err
}
