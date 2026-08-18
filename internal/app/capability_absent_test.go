package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// "I cannot say" and "there is nothing" were one answer, and a caller cannot act differently on
// facts it is handed identically. An empty model menu meant both "this backend lists no models"
// and "nobody asked it" — and the second is a bug the first hides, which is how a menu stayed
// empty for as long as the wrapper had existed with nobody able to see why.
func TestAMissingCapabilityIsNotAnEmptyAnswer(t *testing.T) {
	a := &App{llm: noopProvider{}} // implements the port and nothing else
	got, err := a.ListModels(context.Background())
	if !errors.Is(err, port.ErrCapabilityAbsent) {
		t.Fatalf("a backend that cannot list models answered (%v, %v) — indistinguishable from an "+
			"empty catalogue", got, err)
	}
	if got != nil {
		t.Errorf("the absent answer carried a list: %v", got)
	}
}

// And a backend that CAN answer still answers, through both wrappers, in the order a turn uses
// them: the meter wraps the guard wraps the backend.
func TestACapabilitySurvivesBothWrappers(t *testing.T) {
	a := &App{}
	inner := listingProvider{models: []string{"opus"}}
	through := a.MeterProvider(GuardProvider(inner))
	lister, ok := through.(port.ModelLister)
	if !ok {
		t.Fatal("the stack a turn actually runs on cannot be asked for its models")
	}
	got, err := lister.ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0] != "opus" {
		t.Errorf("through guard+meter the catalogue came back as %v (%v)", got, err)
	}
}

// A reader process asks the log, not a snapshot it took earlier.
//
// The console caches a session's meta on first sight and has nothing to invalidate it with: the
// daemon changes the model in another process, writes the fact, and the console kept answering
// from the meta it had cached before that — so the model select repainted the old value after
// every successful change and a switch that had landed looked refused.
func TestTheContextReportFollowsTheModelChange(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// TWO Apps over one store, which is the arrangement this is about: the daemon runs the turn
	// and the console reads its log. One App would prove nothing — SetModel updates that process's
	// own cache, so the reading side is right for a reason the console does not have.
	daemon := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	console := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	ctx := context.Background()
	sid, _ := daemon.CreateSession(ctx, command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "opus"},
	})

	// The console reads once, which is what fills the cache it has nothing to invalidate.
	if st, err := console.ContextStateOf(ctx, sid); err != nil || st.Model != "opus" {
		t.Fatalf("the opening model came back as %q (%v)", st.Model, err)
	}
	daemon.SetModel(sid, "haiku")
	st, err := console.ContextStateOf(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Model != "haiku" {
		t.Errorf("the console still reports %q after the daemon switched to haiku — this is the "+
			"value the model select repaints, so the change looks refused", st.Model)
	}
}
