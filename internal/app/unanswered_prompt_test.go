package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// hasUnansweredPrompt: false after a fully-answered turn (no re-run storm), true when a
// prompt landed mid-council and the loop then appended the answer AFTER it (the bug:
// the new prompt is buried behind the assistant reply, yet must still re-run). Reuses
// the committed event helpers from seed_abandon_test.go.
func TestHasUnansweredPrompt(t *testing.T) {
	// Normal completed turn: prompt then its answer → nothing open.
	answered := []event.Event{userPromptEvt(t, "m1", "do x"), agentPartEvt(t)}
	if hasUnansweredPrompt(answered, nil) {
		t.Error("a fully-answered turn must report NO unanswered prompt (else it re-runs forever)")
	}
	// Mid-council interjection: m1's answer (agent text) was produced BEFORE the council
	// gate; m2 then arrives during deliberation. The council appends only ActorSystem
	// facts after m2 (no agent part), so m2 has no agent reply after it — it is open, and
	// the old "trailing message" check missed it because council facts followed it.
	buried := []event.Event{userPromptEvt(t, "m1", "do x"), agentPartEvt(t), userPromptEvt(t, "m2", "also do y")}
	if !hasUnansweredPrompt(buried, nil) {
		t.Error("a prompt that landed mid-council (before the finish facts) must count as unanswered")
	}
	// Abandoned (cancelled) trailing prompt → resolved, not open.
	abandonedEvs := []event.Event{userPromptEvt(t, "m1", "x"), agentPartEvt(t), userPromptEvt(t, "m2", "y"), abandonEvt(t, "m2")}
	if hasUnansweredPrompt(abandonedEvs, nil) {
		t.Error("an abandoned prompt must not count as unanswered")
	}
	if hasUnansweredPrompt(nil, nil) {
		t.Error("empty log has no unanswered prompt")
	}
}

// A queued interjection (in the deferred set) must NOT count as an unanswered prompt:
// the dedicated drain path owns it, so counting it at the re-run gate loops it (the
// "consecutive requests keep getting recalled" bug — present before the mid-council
// fix too, since a queued interjection is the trailing user message).
func TestQueuedInterjectionNotRecalled(t *testing.T) {
	// A ran and was answered; B, C queued as interjections (trailing, unanswered).
	evs := []event.Event{
		userPromptEvt(t, "mA", "do A"), agentPartEvt(t),
		userPromptEvt(t, "mB", "then B"), userPromptEvt(t, "mC", "then C"),
	}
	deferred := map[string]bool{"mB": true, "mC": true}
	if hasUnansweredPrompt(evs, deferred) {
		t.Error("queued interjections must not trigger a re-run — the drain path owns them")
	}
	// A genuinely NEW (non-queued) trailing prompt still triggers a re-run.
	if !hasUnansweredPrompt(evs, map[string]bool{"mB": true}) {
		t.Error("a non-deferred unanswered prompt (mC) must still count")
	}
}
