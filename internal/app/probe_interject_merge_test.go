package app

// PROBE (UNCOMMITTED, observation-only) for Problem #1 — a mid-turn steered user
// interjection is "queued" only by a soft prompt directive, never structurally
// separated from the running turn. This is the root cause behind the runtime session
// s_a312… symptoms (a "하이" greeting judged against the prior docs-review task; the
// council rejecting completion on the wrong task).
//
// Two deterministic consequences are proven here WITHOUT a live model:
//
//	(A) LIVE-CONTEXT MERGE. Steer appends the interjection as a normal PromptSubmitted
//	    event (app.appendPrompt). The per-step message builder the loop feeds the model,
//	    reconstruct() (loop.go:281), renders that interjection as a live RoleUser turn —
//	    there is no masking keyed on the pending-interjection queue. So the running turn's
//	    model sees "do the unrelated thing" inline and answers it, merging the two tasks.
//
//	(B) COUNCIL EVIDENCE-WINDOW CORRUPTION. The council-gate scanners treat every
//	    PromptSubmitted as a hard turn boundary and reset their evidence window at it
//	    (council_gate.go:42/118/167). A mid-turn interjection is a PromptSubmitted spliced
//	    into the MIDDLE of the running turn, so the scan discards the real turn's tool
//	    evidence that came before it and judges only the fragment after — i.e. the council
//	    ends up judging the wrong slice of work.
//
// evPrompt / evToolCall / evToolResult live in council_lookup_test.go (committed).

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// evPromptText builds a user PromptSubmitted event carrying real text (what Steer
// appends for a mid-turn interjection).
func evPromptText(msgID, text string) event.Event {
	d, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: msgID,
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	return event.Event{Type: event.TypePromptSubmitted, Data: d}
}

func userText(m session.Message) string {
	if m.Role != session.RoleUser {
		return ""
	}
	var s string
	for _, p := range m.Parts {
		if p.Kind == session.PartText {
			s += p.Text
		}
	}
	return s
}

// (A) The interjection is visible to the running turn's model as its own user message.
// reconstruct takes no knowledge of the pending-interjection queue, so nothing hides a
// still-deferred interjection from the current turn's context → the model merges it in.
func TestProbeInterjectionVisibleInLiveTurn(t *testing.T) {
	evs := []event.Event{
		evPromptText("m_task", "review the project docs and summarize"), // the running task
		evToolCall("c1", "bash"), evToolResult("c1", "read docs/ ...", false),
		// User steers a NEW, unrelated request mid-turn; the loop "queues" it (soft
		// directive) but Steer has already appended it as this event:
		evPromptText("m_interject", "하이"),
	}

	msgs := reconstruct(evs)

	// The interjection must not merely be present — it must be the LAST user turn,
	// sitting after the task prompt and its assistant/tool work. That position is
	// exactly what a running turn's model reads as "the current ask", so it answers
	// "하이" inline (the merge). No masking keyed on the pending-interjection queue
	// stands between the queued event and the model.
	var lastUser string
	var taskSeenBeforeInterject bool
	for _, m := range msgs {
		if ut := userText(m); ut != "" {
			if ut == "review the project docs and summarize" {
				taskSeenBeforeInterject = true
			}
			lastUser = ut
		}
	}
	if !taskSeenBeforeInterject {
		t.Fatal("setup: the running task prompt should precede the interjection")
	}
	if lastUser != "하이" {
		t.Fatalf("expected the queued interjection to be the live turn's newest user "+
			"message (the merge); got last user text %q. If masking was added this flips "+
			"— update the probe", lastUser)
	}
}

// (B) A mid-turn interjection corrupts the council's per-turn evidence window: the
// failed-lookup evidence that came BEFORE the interjection is discarded, so the council
// warning that should fire for this turn goes silent.
func TestProbeInterjectionCorruptsCouncilWindow(t *testing.T) {
	fail := func(id string) []event.Event {
		return []event.Event{
			evToolCall(id, "websearch"),
			evToolResult(id, "search failed: x509: certificate signed by unknown authority", true),
		}
	}

	// Baseline: a clean turn with a failed knowledge lookup → the detector fires.
	clean := append([]event.Event{evPrompt()}, fail("w1")...)
	if unverifiedLookup(clean) == "" {
		t.Fatal("baseline: a failed lookup with no recovery must fire the detector")
	}

	// Same turn, but the user steers an interjection in AFTER the failed lookup. The
	// scanner resets at that PromptSubmitted boundary and forgets the failure that this
	// same turn actually produced.
	corrupted := append(append([]event.Event{evPrompt()}, fail("w1")...),
		evPromptText("m_interject", "md 파일만 읽어도 돼"))
	if got := unverifiedLookup(corrupted); got != "" {
		t.Fatalf("a mid-turn interjection should NOT be able to launder the failed lookup "+
			"away — but the boundary reset silenced the detector (got %q). This documents "+
			"the council-window corruption; if it fails, masking was added — update the probe", got)
	}
}
