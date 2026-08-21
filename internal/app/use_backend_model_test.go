package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// redirectableLLM is a provider that can both be pointed somewhere else and say what that
// somewhere serves — the two capabilities a backend switch needs together, and the shape the
// concrete *openai.Client has.
type redirectableLLM struct {
	fakeLLM
	models []string
	err    error
	base   string
}

func (l *redirectableLLM) ListModels(context.Context) ([]string, error) { return l.models, l.err }
func (l *redirectableLLM) SetBaseURL(b string) uint64                   { l.base = b; return 1 }

// Switching a companion's backend moves it onto a model that backend serves.
//
// Redirecting the base URL used to be the whole of a switch. The model name stayed exactly as it
// was, and backends do not share a vocabulary — so a companion sat on backend `alpha` holding
// "Beta 3.7 Flash (High)", which is the other backend's name for something this one never heard of.
// /fleet reported the pairing truthfully, the console drew it, and the next turn would have been
// refused by the backend.
func TestSwitchingBackendMovesOntoAModelItServes(t *testing.T) {
	llm := &redirectableLLM{models: []string{"alpha-default", "alpha-mini"}}
	a := newAppWith(t, llm)
	sid := open(t, a)
	a.SetModel(sid, "Gemini 3.7 Flash (High)") // what the previous backend was called

	if err := a.UseBackend(sid, "http://127.0.0.1:9/v1"); err != nil {
		t.Fatalf("UseBackend: %v", err)
	}
	if llm.base != "http://127.0.0.1:9/v1" {
		t.Errorf("the backend was not redirected: %q", llm.base)
	}
	if got := modelOf(t, a, sid); got != "alpha-default" {
		t.Errorf("model = %q, want the new backend's first offering — a name it has never heard of "+
			"is a turn the backend will refuse", got)
	}
}

// A model the new backend already serves is left alone. The replacement exists to make the
// companion runnable, not to overwrite a choice that is still valid.
func TestSwitchingBackendKeepsAModelItAlreadyServes(t *testing.T) {
	llm := &redirectableLLM{models: []string{"opus", "sonnet", "haiku"}}
	a := newAppWith(t, llm)
	sid := open(t, a)
	a.SetModel(sid, "sonnet")

	if err := a.UseBackend(sid, "http://127.0.0.1:9/v1"); err != nil {
		t.Fatalf("UseBackend: %v", err)
	}
	if got := modelOf(t, a, sid); got != "sonnet" {
		t.Errorf("model = %q, want sonnet kept", got)
	}
}

// A backend that will not say what it serves is not evidence that the current model is wrong.
// An older daemon, a gateway behind auth, a provider with no catalog at all: the switch stands and
// the model is left alone, because the alternative is trading a working control for a guess.
func TestABackendThatCannotListLeavesTheModelAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		llm  *redirectableLLM
	}{
		{"empty catalog", &redirectableLLM{models: nil}},
		{"refuses to answer", &redirectableLLM{err: context.DeadlineExceeded}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAppWith(t, tc.llm)
			sid := open(t, a)
			a.SetModel(sid, "whatever-it-was")
			if err := a.UseBackend(sid, "http://127.0.0.1:9/v1"); err != nil {
				t.Fatalf("UseBackend: %v", err)
			}
			if got := modelOf(t, a, sid); got != "whatever-it-was" {
				t.Errorf("model = %q, want it untouched", got)
			}
		})
	}
}

func modelOf(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.metaLocked(sid)
	if !ok {
		return ""
	}
	return s.Model.Model
}

// open makes a session the app actually knows about. SetModel writes into session meta and does
// nothing at all for an id the app has never opened, so a test that invents one measures silence.
func open(t *testing.T, a *App) session.SessionID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = a.Close(c)
	})
	return sid
}
