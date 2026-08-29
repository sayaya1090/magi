package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// sessionLog is a session log written by hand, because the interesting records are the ones the
// writer would never produce: a payload of the wrong shape, a plan.changed event sitting behind an
// unrelated one, an interjection whose original is resurfaced later. putTodos and appendPrompt can
// only write well-formed records in the order the loop happens to take.
func sessionLog(t *testing.T, evs ...event.Event) (*App, session.SessionID) {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sid := session.SessionID("s_log")
	created, err := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	evs = append([]event.Event{{Type: event.TypeSessionCreated, Data: created}}, evs...)
	for i := range evs {
		evs[i].SessionID = sid
		evs[i].TS = time.Now()
		evs[i].Actor = event.Actor{Kind: event.ActorAgent, ID: "test"}
	}
	if _, err := st.Append(context.Background(), sid, evs...); err != nil {
		t.Fatal(err)
	}
	return &App{store: st}, sid
}

func todosRecord(t *testing.T, td ...session.Todo) event.Event {
	t.Helper()
	d, err := json.Marshal(event.TodosChangedData{Todos: td})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypeTodosChanged, Data: d}
}

func planText(td []session.Todo) string {
	b, _ := json.Marshal(td)
	return string(b)
}

// TestADroppedStepStaysDropped is why the last record replaces the plan instead of merging into it.
//
// Each record carries the WHOLE plan, so a step missing from the newest one is missing on purpose —
// the agent rewrote its plan and left that step out. A reader that accumulated across records would
// hand the panel a step nobody intends to do any more, and it would never leave: no later record
// can remove what a merge has already absorbed.
func TestADroppedStepStaysDropped(t *testing.T) {
	a, sid := sessionLog(t,
		todosRecord(t,
			session.Todo{Content: "read the failing test", Status: "pending"},
			session.Todo{Content: "rewrite the parser", Status: "pending"}),
		todosRecord(t,
			session.Todo{Content: "read the failing test", Status: "completed"}),
	)
	got, err := a.PlanOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	want := []session.Todo{{Content: "read the failing test", Status: "completed"}}
	if planText(got) != planText(want) {
		t.Errorf("the plan is not the last record verbatim\n got: %s\nwant: %s", planText(got), planText(want))
	}
}

// TestAnUnrelatedRecordIsNotAnEmptyPlan holds the type guard to its job.
//
// The guard cannot be replaced by the unmarshal check below it: an event of another type is very
// often a JSON object that decodes into TodosChangedData without complaint, just with no todos in
// it. Drop the guard and the next prompt in the log silently empties the panel.
func TestAnUnrelatedRecordIsNotAnEmptyPlan(t *testing.T) {
	a, sid := sessionLog(t,
		todosRecord(t, session.Todo{Content: "rewrite the parser", Status: "in_progress"}),
		event.Event{Type: event.TypePromptSubmitted, Data: json.RawMessage(`{"parts":[{"kind":"text","text":"and now this"}]}`)},
	)
	got, err := a.PlanOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "rewrite the parser" {
		t.Errorf("a prompt overwrote the plan; got %s", planText(got))
	}
}

// TestAGarbledRecordLeavesTheStandingPlanAlone: an unreadable record is one we know nothing about,
// which is not the same as knowing the plan is now empty. The plan that was last legible stands.
func TestAGarbledRecordLeavesTheStandingPlanAlone(t *testing.T) {
	a, sid := sessionLog(t,
		todosRecord(t, session.Todo{Content: "rewrite the parser", Status: "in_progress"}),
		event.Event{Type: event.TypeTodosChanged, Data: json.RawMessage(`{"todos":"not a list"}`)},
	)
	got, err := a.PlanOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "rewrite the parser" {
		t.Errorf("a garbled record wiped the plan; got %s", planText(got))
	}
}

// TestBothWordsForDoneCount: the log carries two spellings of a finished step, written by different
// producers, and a count that knows only one of them under-reports every plan containing the other.
// Everything else — including cancelled, which is finished but not done — is not progress.
func TestBothWordsForDoneCount(t *testing.T) {
	done, total := PlanProgress([]session.Todo{
		{Content: "a", Status: "completed"},
		{Content: "b", Status: "done"},
		{Content: "c", Status: "in_progress"},
		{Content: "d", Status: "pending"},
		{Content: "e", Status: "cancelled"},
	})
	if done != 2 || total != 5 {
		t.Errorf("progress = %d/%d, want 2/5", done, total)
	}
}

type unreadableStore struct {
	port.Store
	err error
}

func (u unreadableStore) Read(context.Context, session.SessionID, int64) ([]event.Event, error) {
	return nil, u.err
}

// TestAnUnreadableLogIsNotAnEmptyPlan: the caller renders "0 steps" for an empty plan, so a read
// failure that came back as one would draw a session with no plan — a claim about the session,
// made out of a fact about the disk.
func TestAnUnreadableLogIsNotAnEmptyPlan(t *testing.T) {
	boom := errors.New("log is on a disk that went away")
	a := &App{store: unreadableStore{err: boom}}
	got, err := a.PlanOf(context.Background(), session.SessionID("s_log"))
	if !errors.Is(err, boom) {
		t.Errorf("read failure did not reach the caller; err = %v", err)
	}
	if got != nil {
		t.Errorf("a failed read still produced a plan: %s", planText(got))
	}
}
