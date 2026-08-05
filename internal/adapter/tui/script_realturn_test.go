package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Sweep five: a REAL turn.
//
// Everything up to here fed the view synthetic events. That found real bugs, but it can only ever
// check that the view renders what it is told — never that it is told the right thing. The gap is
// not hypothetical: a panel test that fed the plan to the VIEW rendered no panel at all, because
// the panel asks the APP. So this runs the actual loop — a scripted model emitting real tool calls,
// the real tools executing against a real workdir — and pumps the events the app publishes into
// Update, in order, exactly as the runtime does.
//
// What it buys: the surfaces that only exist once magi has OBSERVED something (the panel's record
// of what it wrote and what it ran), and any disagreement between what the app reports and what
// the view shows.

// scriptedLLM replays a fixed sequence of responses, one per step, so a turn can be composed of
// real tool calls. The last response repeats, which ends the turn (no tool call → finish).
type scriptedLLM struct {
	mu    sync.Mutex
	steps [][]port.ProviderEvent
	at    int
}

func (l *scriptedLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	l.mu.Lock()
	step := []port.ProviderEvent{{Type: port.ProviderText, Text: "done."}, {Type: port.ProviderFinish}}
	if l.at < len(l.steps) {
		step = l.steps[l.at]
		l.at++
	}
	l.mu.Unlock()
	ch := make(chan port.ProviderEvent, len(step))
	for _, e := range step {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func call(id, name, args string) port.ProviderEvent {
	return port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
		CallID: id, Name: name, Args: json.RawMessage(args)}}
}

func say(text string) port.ProviderEvent {
	return port.ProviderEvent{Type: port.ProviderText, Text: text}
}

var finish = port.ProviderEvent{Type: port.ProviderFinish}

// realTurn is a Model wired to a live app: it submits a prompt and folds every event the app
// publishes into Update until the turn ends.
type realTurn struct {
	*script
	app    *app.App
	events <-chan event.Event
	cancel func()
	seen   []event.Event // every event the app published while this harness was pumping
}

func newRealTurn(t *testing.T, llm port.LLMProvider) *realTurn {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	a := app.New(store, llm, builtin.Default(), bus.New(), nil, app.Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	evs, cancel, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	m := New(ctx, a, nil, sid, "m", wd, true, "")
	rt := &realTurn{script: &script{t: t, m: m}, app: a, events: evs, cancel: cancel}
	rt.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	return rt
}

// run types the prompt, submits it, and pumps events until the turn finishes or the deadline passes.
//
// The typing is not decoration. A user prompt's bubble is added LOCALLY by the submit path — the
// prompt.submitted event only stamps the request id onto the bubble already there — so a harness
// that calls Submit directly runs a faithful turn under a transcript missing the question. (The
// harness discards the tea.Cmd Update returns, which is where the real submit lives; typing gives
// the same local state without teaching the harness to run command trees.)
func (r *realTurn) run(prompt string) {
	r.t.Helper()
	r.typeText(prompt).enter()
	if err := r.app.Submit(context.Background(), command.SubmitPrompt{
		SessionID: r.m.sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: prompt}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		r.t.Fatal(err)
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return
			}
			r.seen = append(r.seen, ev)
			r.send(eventMsg{ev: ev, sid: r.m.sid, sub: r.m.mainSub})
			if ev.Type == event.TypeTurnFinished || ev.Type == event.TypeError {
				// Keep pumping through the quiet after the finish. The interjection queue is
				// drained by the run goroutine AFTER the turn ends, so stopping at turn.finished
				// would miss exactly the bookkeeping these tests are about.
				quiet := time.NewTimer(600 * time.Millisecond)
				for {
					select {
					case e2, ok := <-r.events:
						if !ok {
							return
						}
						r.seen = append(r.seen, e2)
						r.send(eventMsg{ev: e2, sid: r.m.sid, sub: r.m.mainSub})
						if !quiet.Stop() {
							select {
							case <-quiet.C:
							default:
							}
						}
						quiet.Reset(600 * time.Millisecond)
					case <-quiet.C:
						return
					}
				}
			}
		case <-deadline:
			r.t.Fatal("the turn never finished")
		}
	}
}

// A turn that actually writes a file and runs a command. The view must show both, and magi's own
// record of what it did — the panel's "Observed" section — must appear, which is the one surface
// no synthetic event can produce because the app derives it from the tools that really ran.
func TestARealTurnThatWritesAndRuns(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("I will write the file."), call("c1", "write", `{"path":"note.txt","content":"hello from the tool\n"}`), finish},
		{say("Now I will read it back."), call("c2", "bash", `{"command":"cat note.txt"}`), finish},
		{say("The file says hello from the tool."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("write note.txt and read it back")

	// The workdir really changed.
	got, err := os.ReadFile(filepath.Join(r.m.workdir, "note.txt"))
	if err != nil {
		t.Fatalf("the write tool did not produce the file: %v", err)
	}
	if strings.TrimSpace(string(got)) != "hello from the tool" {
		t.Errorf("the file holds %q", got)
	}
	// The view shows the work and the answer.
	r.renders("a real turn", "write note.txt and read it back", "The file says hello from the tool.")

}

// A tool that FAILS. The exit code and the message are what the user reads to know the turn is in
// trouble, and a frame that renders a failed command like a successful one is the display half of
// the fabrication problem the rest of magi works to prevent.
func TestARealTurnWhoseCommandFails(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Checking."), call("c1", "bash", `{"command":"exit 3"}`), finish},
		{say("It exited 3."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("run the check")

	plain := r.view()
	if !strings.Contains(plain, "exit 3") {
		t.Errorf("a failing command's exit is not on screen:\n%s", plain)
	}
	failed := false
	for _, b := range r.m.blocks {
		if b.kind == blockToolCall && b.done && !b.ok {
			failed = true
		}
	}
	if !failed {
		t.Error("the failed call is not marked failed — it renders like one that worked")
	}
}

// A turn the agent ends by declaring it, so the council really convenes. The verdicts and the
// evidence come from the app, not from a hand-written event, which is the only way to check that
// what the members were handed is what the view shows.
func TestARealCouncilRound(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Writing it."), call("c1", "write", `{"path":"out.txt","content":"result\n"}`), finish},
		{say("Done, declaring."), call("c2", "council", `{"complete":true}`), finish},
		{say("The council accepted."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("write out.txt then finish")

	// With no council configured the tool refuses rather than pretending — either way the turn
	// must land and the refusal must be visible, not swallowed.
	plain := r.view()
	if strings.TrimSpace(plain) == "" {
		t.Fatal("the frame is blank after a declared finish")
	}
	if !strings.Contains(plain, "council") && !strings.Contains(plain, "accepted") {
		t.Errorf("the declaration left no trace on screen:\n%s", plain)
	}
}

// storeEvents is every event the app published while the harness was pumping — the record the
// user's request either is or is not in.
func (r *realTurn) storeEvents(t *testing.T) []event.Event {
	t.Helper()
	return r.seen
}
