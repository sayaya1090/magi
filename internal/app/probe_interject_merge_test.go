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

// (B) The council's per-turn evidence window and what may reset it.
//
// It used to reset on ANY prompt.submitted, and magi emits several of its own — the stall nudge,
// a plugin note, a permission message, a hook, an orchestrator re-prompt. Those laundered the
// turn's failures away: measured live (cancel-async-tasks, 2026-07-30) an orchestrator re-prompt
// landed 12 seconds before a council was convened, so the council judged on an EMPTY block. Fixed:
// only an ActorUser prompt is a boundary.
//
// A genuine mid-turn USER interjection still resets it, and that half is still open — steering the
// agent ("just read the md files") should not erase the failed lookup this same turn produced. It
// needs a way to tell an interjection from a new request, which is a design question, so this
// documents it rather than masking it.
