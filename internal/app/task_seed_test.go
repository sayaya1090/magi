package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// "The task" was read off reconstruct()'s messages, and reconstruct labels every prompt.submitted
// RoleUser no matter who wrote it. magi writes several — the stall nudge, an orchestrator
// re-prompt, a hook message, a permission note, a plugin note — so the newest of those became the
// answer to "what was I asked to do".
//
// The caller that hurts most is the nudge: it ends with "Re-read the original task:" and quotes
// this, so a second nudge handed the agent the FIRST nudge as its task. For a subagent, where
// turnTask is empty and this fallback is the only path, the real seed was gone entirely. The
// retrieval query for every remaining step came from the same place.
func TestTheTaskIsNeverSomethingMagiSaid(t *testing.T) {
	prompt := func(actor event.Actor, text string) event.Event {
		id := text
		if len(id) > 4 {
			id = id[:4]
		}
		b, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + id,
			Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		})
		return event.Event{Type: event.TypePromptSubmitted, Actor: actor, Data: b}
	}
	user := event.Actor{Kind: event.ActorUser, ID: "cli"}
	parent := event.Actor{Kind: event.ActorAgent, ID: "parent"}
	const task = "fix the GC bug in shared_heap.c"
	const seed = "SUBTASK: port the parser"

	// Every actor magi injects prompts under, after a real task.
	for _, injected := range []event.Actor{
		{Kind: event.ActorSystem, ID: "loop"},
		{Kind: event.ActorSystem, ID: "orchestrator"},
		{Kind: event.ActorSystem, ID: "hook"},
		{Kind: event.ActorSystem, ID: "plugin"},
	} {
		for _, base := range []struct {
			name, want string
			seedEv     event.Event
		}{
			{"top-level user task", task, prompt(user, task)},
			{"subagent seed", seed, prompt(parent, seed)},
		} {
			evs := []event.Event{base.seedEv, prompt(injected, "You've run many steps without changing anything…")}
			if got := taskSeedText(evs); got != base.want {
				t.Errorf("%s/%s: the task is what was asked, got %q", injected.ID, base.name, got)
			}
			// Two of magi's own in a row must not become the task either.
			evs = append(evs, prompt(injected, "You stopped without saying you are finished."))
			if got := taskSeedText(evs); got != base.want {
				t.Errorf("%s/%s: a second injection is still not the task, got %q", injected.ID, base.name, got)
			}
		}
	}

	// A NEW user request replaces the old one — that is a real task change.
	evs := []event.Event{
		prompt(user, task),
		prompt(event.Actor{Kind: event.ActorSystem, ID: "loop"}, "nudge"),
		prompt(user, "now write the tests instead"),
	}
	if got := taskSeedText(evs); got != "now write the tests instead" {
		t.Errorf("the newest real request wins, got %q", got)
	}
	// Nothing to report is silence, not a guess.
	if got := taskSeedText(nil); got != "" {
		t.Errorf("no prompts, no task: %q", got)
	}
	if got := taskSeedText([]event.Event{
		prompt(event.Actor{Kind: event.ActorSystem, ID: "loop"}, "nudge only")}); got != "" {
		t.Errorf("magi's own words alone are not a task: %q", got)
	}
	// Whitespace-only prompts are skipped rather than returned as an empty task.
	if got := taskSeedText([]event.Event{prompt(user, task), prompt(user, "   ")}); got != task {
		t.Errorf("a blank prompt does not erase the task, got %q", got)
	}
}
