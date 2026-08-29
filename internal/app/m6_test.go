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
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// reconstruct keeps events newer than a compaction boundary and replaces older
// ones with the summary (seq-aware compaction).
func TestReconstructCompactionKeepsTail(t *testing.T) {
	mk := func(seq int64, typ event.Type, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Seq: seq, Type: typ, Data: b}
	}
	textPart := func(s string) session.Part { return session.Part{Kind: session.PartText, Text: s} }

	evs := []event.Event{
		mk(1, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u1", Parts: []session.Part{textPart("first")}}),
		mk(2, event.TypePartAppended, event.PartAppendedData{MessageID: "a1", Role: session.RoleAssistant, Part: textPart("reply1")}),
		mk(3, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u2", Parts: []session.Part{textPart("recent")}}),
		mk(4, event.TypeCompaction, event.CompactionData{Summary: "SUMMARY", ReplacesUpToSeq: 2}),
	}
	msgs := reconstruct(evs)
	// Expect [system SUMMARY, user "recent"] — the pre-boundary turns are gone.
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != session.RoleSystem || msgs[0].Parts[0].Text != "SUMMARY" {
		t.Errorf("first message should be the summary, got %+v", msgs[0])
	}
	if msgs[1].ID != "u2" || msgs[1].Parts[0].Text != "recent" {
		t.Errorf("tail not preserved, got %+v", msgs[1])
	}
}

// turn.finished carries a computed cost for a priced model.
func TestTurnCost(t *testing.T) {
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "priced", ContextWindow: 100000, Tools: true, InputCost: 10, OutputCost: 10})

	llm := &usageLLM{in: 1_000_000, out: 0} // → cost = $10
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow", Models: reg,
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "priced"}})
	got := runToTerminal(t, a, sid)

	var tf event.TurnFinishedData
	if !lastData(got, event.TypeTurnFinished, &tf) {
		t.Fatal("no turn.finished")
	}
	if tf.Usage.Cost != 10 {
		t.Errorf("cost=%v want 10", tf.Usage.Cost)
	}
}

// F-COMPACT compact-ctx-1: history over threshold, next turn compacts.
//
// Half of the probe. It holds "1 compaction event"; the clause after it — that the request the
// model then receives is SHORTER — is not asserted anywhere, and a fold that emitted the event
// while sending the same history would pass. Auto-compaction triggers when context exceeds the
// model window budget.
func TestAutoCompactionTriggers(t *testing.T) {
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "tiny", ContextWindow: 64, Tools: true}) // ~256 chars budget*0.8

	// Seed a session with enough history to blow the tiny budget.
	store, _ := jsonl.New(t.TempDir())
	llm := &usageLLM{text: "ok"} // also used as the summarizer (returns "ok")
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow", Models: reg, CompactRatio: 0.8,
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "tiny"}})

	// Pre-load many fact events directly so the first turn is already over budget.
	big := strings.Repeat("x", 200)
	for i := 0; i < 10; i++ {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: big}})
		a.appendFact(context.Background(), sid, event.TypePartAppended, event.Actor{}, d)
	}

	got := runToTerminal(t, a, sid)
	if countType(got, event.TypeCompaction) == 0 {
		t.Errorf("expected a compaction event, got types %v", typesOf(got))
	}
}

// ---- helpers ----

// usageLLM returns a fixed text turn and records the last requested model and a
// usage report. The recorded fields are guarded by mu: a subagent turn runs the
// provider on its own goroutine, so a reader (post-turn) and that write must be
// synchronized for `go test -race` (mirrors fakeLLM's locking).
type usageLLM struct {
	mu         sync.Mutex
	text       string
	in, out    int
	lastModel  string
	lastSystem string
}

func (f *usageLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.lastModel = r.Model
	f.lastSystem = r.System
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	txt := f.text
	if txt == "" {
		txt = "ok"
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: txt}
	if f.in != 0 || f.out != 0 {
		ch <- port.ProviderEvent{Type: port.ProviderUsage, Usage: &event.Usage{In: f.in, Out: f.out}}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func usedModel(f *usageLLM) string  { f.mu.Lock(); defer f.mu.Unlock(); return f.lastModel }
func (f *usageLLM) lastSys() string { f.mu.Lock(); defer f.mu.Unlock(); return f.lastSystem }

func runToTerminal(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	var got []event.Event
	for e := range ch {
		got = append(got, e)
		if e.Type == event.TypeTurnFinished || e.Type == event.TypeError {
			return got
		}
	}
	t.Fatal("no terminal event")
	return got
}

func lastData(evs []event.Event, typ event.Type, out any) bool {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == typ {
			return json.Unmarshal(evs[i].Data, out) == nil
		}
	}
	return false
}

// The compaction boundary never splits a message: a step writes reasoning, text and tool calls
// as separate parts under one message id, and a boundary landing between them used to lose the
// post-boundary parts from BOTH sides of the fold (the entry dropped by its opening seq, the
// summarizer cut at the boundary).
func TestCompactionBoundarySnapsToAMessageSeam(t *testing.T) {
	mk := func(seq int64, typ event.Type, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Seq: seq, Type: typ, Data: b}
	}
	part := func(msg, txt string) event.PartAppendedData {
		return event.PartAppendedData{MessageID: msg, Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: txt}}
	}
	evs := []event.Event{
		mk(1, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u1"}),
		mk(2, event.TypePartAppended, part("a1", "reasoning")),
		mk(3, event.TypePartAppended, part("a1", "the tool call")), // a1 spans 2..3
		mk(4, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u2"}),
	}
	if got := snapToMessageSeam(evs, 2); got != 1 {
		t.Fatalf("a boundary inside a1 must snap below its first part, got %d", got)
	}
	if got := snapToMessageSeam(evs, 3); got != 3 {
		t.Fatalf("a boundary at a message's LAST part is a seam, got %d", got)
	}
	// Chained straddles resolve to the outermost seam.
	evs = append(evs,
		mk(5, event.TypePartAppended, part("a2", "x")),
		mk(6, event.TypePartAppended, part("a2", "y")),
	)
	if got := snapToMessageSeam(evs, 5); got != 4 {
		t.Fatalf("a boundary inside a2 snaps to 4, got %d", got)
	}
	if got := snapToMessageSeam(evs, 4); got != 4 {
		t.Fatalf("a clean seam stays put, got %d", got)
	}
	// When lowering runs out of log — the straddling message IS the log — the other direction
	// folds it whole instead of splitting it.
	one := []event.Event{
		mk(1, event.TypePartAppended, part("solo", "a")),
		mk(2, event.TypePartAppended, part("solo", "b")),
		mk(3, event.TypePartAppended, part("solo", "c")),
	}
	if got := snapToMessageSeam(one, 2); got != 0 {
		t.Fatalf("lowering inside the only message reaches zero, got %d", got)
	}
	if got := raiseToMessageSeam(one, 2); got != 3 {
		t.Fatalf("raising folds the message whole, got %d", got)
	}
}
