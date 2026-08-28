package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/model"
)

// settle waits for a background window probe to land. The probe is a goroutine by design — the
// read that starts it is on the compaction hot path and must not block — so a test that reads
// straight after gets the fallback, which is what it is for.
func settle(t *testing.T) { t.Helper(); time.Sleep(250 * time.Millisecond) }

// twoBackends is a prober whose answer depends on WHERE requests currently go: the same model
// name, served by two servers started with different context lengths. That is not a contrived
// case — it is the one probe.go was written for, where /api/show reports the trained length and
// the running instance serves whatever num_ctx it was started with.
func twoBackends(llm interface{ BaseURL() string }, by map[string]int) func(context.Context, string) (int, bool) {
	return func(context.Context, string) (int, bool) {
		w, ok := by[llm.BaseURL()]
		return w, ok
	}
}

// A window learned from one backend does not follow the session to another.
//
// The cache was keyed by the model NAME, and a window is a fact about a (backend, model) pair.
// So after a /route switch onto a server that serves the same name with a smaller window, the
// previous server's number went on governing compaction — sized against a limit this backend will
// never reach, so the turn is never compacted and the backend refuses it. That is the exact
// failure the probe exists to prevent, reintroduced by the switch.
func TestAWindowLearnedFromOneBackendDoesNotFollowToAnother(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"shared-name"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.ContextWindowProber = twoBackends(llm, map[string]int{"": 262144, "http://b/v1": 96000})
	sid := open(t, a)
	a.SetModel(sid, "shared-name")

	a.contextWindow("shared-name") // starts the probe; returns the fallback
	settle(t)
	if got := a.contextWindow("shared-name"); got != 262144 {
		t.Fatalf("window on the first backend = %d, want 262144; the test measures nothing", got)
	}

	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("shared-name") // the read that notices the move and re-probes
	settle(t)
	if got := a.contextWindow("shared-name"); got != 96000 {
		t.Errorf("window after the switch = %d, want 96000 — the old backend's number is still "+
			"sizing compaction for a server that will refuse it", got)
	}
}

// A backend that could not answer said nothing about the next one.
//
// The mark that stops us hammering a backend is per-backend too. Left in place across a redirect
// it would silence the probe for the rest of the process, so a model would keep the family
// fallback even after moving to a server that knows its real window.
func TestAFailedProbeIsRetriedOnADifferentBackend(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"quiet-model"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.ContextWindowProber = twoBackends(llm, map[string]int{"http://b/v1": 96000}) // "" answers nothing
	sid := open(t, a)
	a.SetModel(sid, "quiet-model")

	a.contextWindow("quiet-model")
	settle(t)
	if got := a.contextWindow("quiet-model"); got != 0 {
		t.Fatalf("a backend that answers nothing gave %d; the test measures nothing", got)
	}

	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("quiet-model")
	settle(t)
	if got := a.contextWindow("quiet-model"); got != 96000 {
		t.Errorf("window after the switch = %d, want 96000 — the silence of the previous backend "+
			"is still standing in for an answer this one would give", got)
	}
}

// A window a person pinned outlives a backend switch. They were not talking about a backend.
func TestAPinnedWindowSurvivesABackendSwitch(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"pinned-model"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.ContextWindowProber = twoBackends(llm, map[string]int{"": 262144, "http://b/v1": 96000})
	sid := open(t, a)
	a.SetModel(sid, "pinned-model")

	if _, err := a.SetContextWindow(context.Background(), sid, "pinned-model", 40000); err != nil {
		t.Fatal(err)
	}
	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("pinned-model")
	settle(t)
	if got := a.contextWindow("pinned-model"); got != 40000 {
		t.Errorf("window after the switch = %d, want the pinned 40000 — a redirect threw away a "+
			"number a person typed", got)
	}
}

// A model the operator configured is not a probe result and is never forgotten. Only what a
// backend told us belongs to that backend.
func TestAConfiguredWindowIsNotForgottenOnASwitch(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"configured-model"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.Models.Register(model.Info{ID: "configured-model", ContextWindow: 8000, Tools: true})
	a.cfg.ContextWindowProber = twoBackends(llm, map[string]int{"": 262144, "http://b/v1": 96000})
	sid := open(t, a)
	a.SetModel(sid, "configured-model")

	if got := a.contextWindow("configured-model"); got != 8000 {
		t.Fatalf("configured window = %d, want 8000; the test measures nothing", got)
	}
	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("configured-model")
	settle(t)
	if got := a.contextWindow("configured-model"); got != 8000 {
		t.Errorf("window after the switch = %d, want the configured 8000", got)
	}
}
