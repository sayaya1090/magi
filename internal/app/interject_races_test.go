package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// hookLLM is fakeLLM with a per-call hook, so a test can steer the session at a chosen point in
// the turn — the only way to reach the windows where an interjection actually races something.
type hookLLM struct {
	mu     sync.Mutex
	steps  [][]port.ProviderEvent
	call   int
	onCall func(n int) // runs BEFORE the reply is produced, with the 1-based call number
}

func (f *hookLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := f.call
	f.call++
	var evs []port.ProviderEvent
	if n < len(f.steps) {
		evs = f.steps[n]
	} else {
		evs = textStep("done")
	}
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall(n + 1)
	}
	ch := make(chan port.ProviderEvent, 16)
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// watcher records the bus so a test can ask the two questions that matter about any interjection
// path: was the request lost, and was the screen left running.
type watcher struct {
	mu       sync.Mutex
	terminal int // turn.finished seen on the bus (persisted OR transient)
	lastWork int // events carrying assistant content
	seq      int
	lastTerm int
}

func watch(t *testing.T, a *App, sid session.SessionID) *watcher {
	t.Helper()
	sub, cancel, err := a.Subscribe(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	w := &watcher{}
	go func() {
		for e := range sub {
			w.mu.Lock()
			w.seq++
			switch e.Type {
			case event.TypeTurnFinished:
				w.terminal++
				w.lastTerm = w.seq
			case event.TypePartAppended, event.TypePartDelta:
				var d event.PartAppendedData
				if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant {
					w.lastWork = w.seq
				}
			}
			w.mu.Unlock()
		}
	}()
	return w
}

// idle reports whether a terminal signal came AFTER the last assistant content — the property the
// transcript uses to stop the spinner.
func (w *watcher) idle() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.terminal > 0 && w.lastTerm > w.lastWork
}

func (w *watcher) waitIdle(t *testing.T, d time.Duration) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(15 * time.Millisecond) {
		if w.idle() {
			return
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	t.Fatalf("the run went quiet with work still showing: terminals=%d lastTerminal=%d lastAssistantContent=%d",
		w.terminal, w.lastTerm, w.lastWork)
}

func submitAs(t *testing.T, a *App, sid session.SessionID, text string) {
	t.Helper()
	if err := a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "cli"},
	}); err != nil {
		t.Fatal(err)
	}
}

// A question typed mid-turn must reach an answer, and the run must not be left showing work —
// whichever window it lands in. The reported failure was the last of these: the interjection was
// drained and answered INLINE after the turn's own terminal event, so the transcript revived the
// spinner on the reply's tokens and nothing ever turned it off.
func TestAnInterjectionInEveryWindowLandsAndGoesQuiet(t *testing.T) {
	for _, c := range []struct {
		name string
		at   int // steer just before the model's Nth call; 0 = immediately after submit
	}{
		{"immediately after the prompt", 0},
		{"while the first step is in flight", 1},
		{"while a later step is in flight", 2},
		{"at the finish boundary", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			llm := &hookLLM{steps: [][]port.ProviderEvent{
				toolStep("list", `{"path":"."}`),
				toolStep("list", `{"path":"."}`),
				textStep("the original answer"),
			}}
			a, wd := newApp(t, llm, Config{Permission: "allow"})
			sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
			if err != nil {
				t.Fatal(err)
			}
			w := watch(t, a, sid)
			var once sync.Once
			if c.at > 0 {
				llm.onCall = func(n int) {
					if n == c.at {
						once.Do(func() { submitAs(t, a, sid, "그런데 파일 몇 개나 봤어?") })
					}
				}
			}
			submitAs(t, a, sid, "look around and report")
			if c.at == 0 {
				submitAs(t, a, sid, "그런데 파일 몇 개나 봤어?")
			}

			w.waitIdle(t, 20*time.Second)

			// And the request itself is not lost: it is answered, resurfaced as its own prompt, or
			// explicitly resolved in the ledger. Silence is the one outcome that is a bug.
			evs, err := a.store.Read(context.Background(), sid, 0)
			if err != nil {
				t.Fatal(err)
			}
			var msgID string
			for _, e := range evs {
				var d event.PromptSubmittedData
				if e.Type == event.TypePromptSubmitted && json.Unmarshal(e.Data, &d) == nil &&
					strings.Contains(partsText(d.Parts), "파일 몇 개나") && d.ResurfacedFrom == "" {
					msgID = d.MessageID
				}
			}
			if msgID == "" {
				t.Fatal("the interjection never reached the log at all")
			}
			accounted := false
			for _, e := range evs {
				switch e.Type {
				case event.TypeInterjectionDeferred:
					var d event.InterjectionDeferredData
					if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID && d.Resolved {
						accounted = true
					}
				case event.TypePromptSubmitted:
					var d event.PromptSubmittedData
					if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom == msgID {
						accounted = true
					}
				case event.TypeInterjectionAnswered:
					var d event.InterjectionAnsweredData
					if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
						accounted = true
					}
				}
			}
			// A steer the loop simply absorbed into the running turn is accounted for too: the turn
			// answered it without ever queueing it.
			if !accounted && !seenAfter(evs, msgID) {
				t.Errorf("the interjection was neither answered, resurfaced nor resolved — it was dropped")
			}
		})
	}
}

// seenAfter reports whether any assistant content was persisted after the prompt — the loop
// absorbed it into the running turn rather than queueing it.
func seenAfter(evs []event.Event, msgID string) bool {
	past := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
				past = true
				continue
			}
		}
		if past && e.Type == event.TypePartAppended {
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant {
				return true
			}
		}
	}
	return false
}
