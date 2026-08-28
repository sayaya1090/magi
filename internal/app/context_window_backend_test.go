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

// An answer from the backend we left is not stamped as the new one's.
//
// A probe is a goroutine with a 4s timeout and a redirect is one call, so a probe outlives one
// routinely. Landing without checking where it asked, the old server's 262144 registered under the
// new base and was marked as something THIS backend had said — so the next read's invalidation
// left it alone, and the correct answer from the probe that read had started was dropped by
// RegisterIfAbsent as a duplicate. The wrong number then governed compaction until the NEXT switch.
func TestAnAnswerFromTheBackendWeLeftIsNotStampedAsTheNewOnes(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"shared-name"}}
	a := newAppWith(t, GuardProvider(llm))
	// Both probes are held on channels, so the landing order is decided here and not by whichever
	// goroutine the scheduler happens to run: the late one lands first, which is the whole case.
	old, current := make(chan struct{}), make(chan struct{})
	asked := make(chan string, 4)
	a.cfg.ContextWindowProber = func(_ context.Context, _ string) (int, bool) {
		base := llm.BaseURL()
		asked <- base
		if base == "" {
			<-old
			return 262144, true
		}
		<-current
		return 96000, true
	}
	sid := open(t, a)
	a.SetModel(sid, "shared-name")

	a.contextWindow("shared-name") // launches the probe against the first backend, now held
	if b := <-asked; b != "" {
		t.Fatalf("the first probe asked at %q, not the first backend; the test measures nothing", b)
	}
	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("shared-name") // notices the move and starts a probe against the new backend
	if b := <-asked; b != "http://b/v1" {
		t.Fatalf("the second probe asked at %q, not the new backend; the test measures nothing", b)
	}

	close(old) // the answer from the backend we left arrives first
	settle(t)
	// Dropping that answer must not clear the mark either: the mark standing now is the second
	// probe's, and erasing it lets the next read start a third probe against a backend already
	// being asked.
	a.contextWindow("shared-name")
	settle(t) // the probe would be a goroutine, so give it the same chance to report as a real one
	if len(asked) != 0 {
		t.Errorf("a third probe was started while the second is still out — the dropped answer " +
			"cleared a mark that was not its own")
	}
	close(current)
	settle(t)

	if got := a.contextWindow("shared-name"); got != 96000 {
		t.Errorf("window = %d, want 96000 — an answer from the backend we left is being served "+
			"as this one's, and it will outlive the read that should have dropped it", got)
	}
}

// Forgetting a backend's windows does not reach an entry the probe never made.
//
// Registry.Forget deletes by exact id without asking who wrote it, so the ids it is handed have to
// be ours. Under windowProbing they are not: the probe registered nothing, and an entry under that
// id arrived by another route — a plugin's Register, or a built-in default. Deleting one is the
// dangerous direction twice over, because Get then answers 0 and every consumer reads 0 as
// unlimited, so compaction stops entirely for a model that had a perfectly good window.
func TestForgettingDoesNotReachAnEntryTheProbeNeverMade(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"plug-model"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.ContextWindowProber = func(context.Context, string) (int, bool) { return 0, false }
	sid := open(t, a)
	a.SetModel(sid, "plug-model")

	a.contextWindow("plug-model") // marks the id; the probe finds nothing and the mark stays
	settle(t)
	// Someone else supplies the window while that mark is standing.
	a.cfg.Models.Register(model.Info{ID: "plug-model", ContextWindow: 8000, MaxOutput: 2000, Tools: true})
	if got := a.contextWindow("plug-model"); got != 8000 {
		t.Fatalf("window before the switch = %d, want 8000; the test measures nothing", got)
	}

	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	if got := a.contextWindow("plug-model"); got != 8000 {
		t.Errorf("window after the switch = %d, want 8000 — a registration the probe never made "+
			"was deleted, and 0 turns compaction off", got)
	}
}

// A probe writes the window it measured, and does not answer for the rest.
//
// The registration was built fresh, so Vision and both costs came out 0/false — and creating an
// entry under the exact name is exactly what stops Get falling through to the family that knew
// them. Probing a ":tag" variant of a vision model therefore turned pictures off and its price to
// zero, but only once the probe landed: before that the same call was right. Left in, it also
// blinks, because forgetting another backend's windows now drops that entry and restores the
// family until the next probe lands — the same model answering differently at different moments
// for a reason that has nothing to do with pictures.
//
// The window itself must still narrow: that is what the probe is for.
func TestAProbeDoesNotAnswerForWhatItDidNotMeasure(t *testing.T) {
	llm := &wholeRedirectLLM{models: []string{"seer:8b"}}
	a := newAppWith(t, GuardProvider(llm))
	a.cfg.Models.Register(model.Info{ID: "seer", ContextWindow: 128000, MaxOutput: 32000,
		Tools: true, Vision: true, InputCost: 2, OutputCost: 8})
	a.cfg.ContextWindowProber = twoBackends(llm, map[string]int{"": 96000, "http://b/v1": 64000})
	sid := open(t, a)
	a.SetModel(sid, "seer:8b")

	if !a.VisionOf("seer:8b") {
		t.Fatalf("the variant does not read the family before probing; the test measures nothing")
	}
	a.contextWindow("seer:8b")
	settle(t)

	if got := a.contextWindow("seer:8b"); got != 96000 {
		t.Fatalf("window = %d, want the probed 96000 — the probe must still narrow the window", got)
	}
	if !a.VisionOf("seer:8b") {
		t.Errorf("a window probe turned pictures off for a model that reads them: it is answering " +
			"for a field it never measured, and the model now gets a filename instead of the image")
	}
	if c := a.cfg.Models.Get("seer:8b").Cost(1_000_000, 1_000_000); c != 10 {
		t.Errorf("cost of 1M/1M after probing = $%.2f, want $10.00 — the probe zeroed the price", c)
	}

	// And it does not blink: dropping the probed entry on a switch, then probing again, has to land
	// on the same answer both times.
	if err := a.UseBackend(sid, "http://b/v1"); err != nil {
		t.Fatal(err)
	}
	a.contextWindow("seer:8b")
	settle(t)
	if got := a.contextWindow("seer:8b"); got != 64000 {
		t.Fatalf("window on the second backend = %d, want 64000; the test measures nothing", got)
	}
	if !a.VisionOf("seer:8b") {
		t.Errorf("pictures went off again once the new backend's probe landed")
	}
}
