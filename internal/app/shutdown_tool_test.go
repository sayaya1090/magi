package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A shutdown that cuts a tool mid-run is the same uncertainty a crash leaves: the call started and
// may have done part of its work. It must close the way startup recovery closes it — may have
// run, not run again, the turn handed to the person — never as a resumable turn a "continue"
// would carry straight back into the same call.
// SPEC F-LOOP-INTERRUPT loop-int-2.
func TestAToolCutByShutdownIsHandedToThePersonNotResumed(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	wd := t.TempDir()
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("bash", `{"command":"echo ran >> marker; sleep 30"}`)}}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(wd, "marker")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tool never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if left := a.Shutdown(5 * time.Second); len(left) != 0 {
		t.Fatalf("the tool honours cancel and should settle within the grace; left %v", left)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if countType(evs, event.TypePromptAbandoned) != 0 {
		t.Fatalf("a shutdown recorded the prompt as abandoned: %v", typesOf(evs))
	}
	r, ok := resultsByCall(t, evs)["c_bash"]
	if !ok || !r.IsError || !strings.Contains(string(r.Content), "may have run") || !strings.Contains(string(r.Content), "not run again") {
		t.Fatalf("the cut call should say it may have run and was not run again; got ok=%v %+v", ok, r)
	}
	var ed event.ErrorData
	if !lastData(evs, event.TypeError, &ed) || ed.Code != "interrupted" || ed.Resumable {
		t.Fatalf("want interrupted and NOT resumable (a call may have run); got %+v types=%v", ed, typesOf(evs))
	}
	fresh := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	if _, ok := fresh.ResumableTurnOf(ctx, sid); ok {
		t.Fatal("a turn with a call that may have run must not read as resumable")
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "marker")); strings.Count(string(b), "ran") != 1 {
		t.Fatalf("the tool ran %d times, want once", strings.Count(string(b), "ran"))
	}
}
