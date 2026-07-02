package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// turnToolEvidence feeds the council the current turn's TOOL results as evidence — not
// the model's narration (a defeatist agent's claim must never count) — and only from the
// latest turn (a prior turn's success can't masquerade as this one's).
func TestTurnToolEvidence(t *testing.T) {
	part := func(role session.Role, p session.Part) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{Role: role, Part: p})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	call := func(id, name string) event.Event {
		return part(session.RoleAssistant, session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name}})
	}
	result := func(id string, content string, isErr bool) event.Event {
		return part(session.RoleTool, session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: json.RawMessage(content), IsError: isErr}})
	}
	prompt := event.Event{Type: event.TypePromptSubmitted}

	// A defeatist turn: only model narration, no tool calls → NO evidence (so the
	// council can't be talked into "done").
	if got := turnToolEvidence([]event.Event{prompt,
		part(session.RoleAssistant, session.Part{Kind: session.PartText, Text: "I created the file, all done."}),
	}, 8); got != "" {
		t.Errorf("narration-only turn must yield no evidence, got %q", got)
	}

	// A real turn: the write result surfaces as labeled tool evidence.
	got := turnToolEvidence([]event.Event{prompt,
		call("c1", "write"), result("c1", `"wrote 13 bytes to hello.txt"`, false),
		part(session.RoleAssistant, session.Part{Kind: session.PartText, Text: "done"}),
	}, 8)
	if !strings.Contains(got, "tool write [ok]: wrote 13 bytes to hello.txt") {
		t.Errorf("tool result not surfaced as evidence: %q", got)
	}
	if strings.Contains(got, "done") || strings.Contains(got, "model:") {
		t.Errorf("model narration must not appear in evidence: %q", got)
	}

	// Turn boundary: a prior turn's successful write must NOT leak into this turn's
	// (which only claimed success with no tool).
	leak := turnToolEvidence([]event.Event{prompt,
		call("c1", "write"), result("c1", `"wrote 99 bytes to old.txt"`, false),
		prompt, // new turn starts here
		part(session.RoleAssistant, session.Part{Kind: session.PartText, Text: "created new.txt"}),
	}, 8)
	if leak != "" {
		t.Errorf("prior turn's tool result leaked into this turn: %q", leak)
	}

	// An errored tool is labeled [error].
	if e := turnToolEvidence([]event.Event{prompt, call("c1", "bash"), result("c1", `"boom"`, true)}, 8); !strings.Contains(e, "[error]") {
		t.Errorf("errored tool should be labeled: %q", e)
	}
}

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

// workingLLM simulates a turn that does real work — a read-only tool call —
// before its final reply. The council gate only fires on turns that used tools
// (pure conversational turns are skipped), so council tests must do real work.
func workingLLM(after ...[]port.ProviderEvent) *fakeLLM {
	steps := append([][]port.ProviderEvent{toolStep("read", `{"path":"x"}`)}, after...)
	return &fakeLLM{steps: steps}
}

func newApp(t *testing.T, llm port.LLMProvider, cfg Config) (*App, string) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), nil, cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx) // drain run/dispatch goroutines before TempDir cleanup
	})
	return a, t.TempDir()
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

// selfVerifyFired reports whether the pre-finish 2-pass self-verification prompt
// was injected (a system prompt.submitted carrying its signature text).
func selfVerifyFired(evs []event.Event) bool {
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "VERIFY you satisfied") {
			return true
		}
	}
	return false
}

// A bash-driven turn (which writes files but never populates guard.changeSet())
// must still trigger the pre-finish self-verify. This is the ② fix: keying the
// gate off "ran a write-capable tool" instead of "changeSet>0" — otherwise a
// bash-only agent that did 1 of N deliverables never gets the coverage check.
func TestSelfVerifyFiresAfterBash(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("bash", `{"command":"echo hi > out.txt"}`),
		textStep("완료"), // wants to finish → self-verify should fire here
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "make a file with bash"}}})

	got := waitForTerminal(t, a, sid)
	if !selfVerifyFired(got) {
		t.Errorf("self-verify: expected the pre-finish verify prompt after a bash turn, got %v", typesOf(got))
	}
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Errorf("self-verify: expected the turn to finish, got %v", typesOf(got))
	}
}

// A pure read-only turn (no write-capable tool) has no artifact to re-check, so
// the self-verify must NOT fire — gating it would only churn.
func TestSelfVerifySkippedForReadOnly(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("read", `{"path":"nope.txt"}`),
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "just look"}}})

	got := waitForTerminal(t, a, sid)
	if selfVerifyFired(got) {
		t.Errorf("self-verify: must NOT fire on a read-only turn, got %v", typesOf(got))
	}
}

// The self-verify prompt must carry the anti-fabrication clause: a weak model
// told to "verify" will otherwise hand-write what a run WOULD have produced to
// make the check pass. The prompt must forbid manufacturing output and require
// treating unrun/unobserved work as UNFINISHED.
func TestSelfVerifyForbidsFabrication(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("bash", `{"command":"echo hi > out.txt"}`),
		textStep("완료"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "make a file with bash"}}})

	got := waitForTerminal(t, a, sid)
	var verify string
	for _, e := range got {
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "VERIFY you satisfied") {
			verify = string(e.Data)
		}
	}
	if verify == "" {
		t.Fatalf("self-verify: prompt never fired, got %v", typesOf(got))
	}
	for _, want := range []string{"Do NOT invent", "fabricate", "UNFINISHED", "actually observed"} {
		if !strings.Contains(verify, want) {
			t.Errorf("self-verify: anti-fabrication prompt missing %q", want)
		}
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
	a, wd := newApp(t, llm, Config{Permission: "ask", Interactive: true})
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
	a, wd := newApp(t, llm, Config{Permission: "auto", Interactive: true})
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
