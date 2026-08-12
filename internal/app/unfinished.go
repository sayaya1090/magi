package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn that was cut off is still in the log, and until now nothing said so.
//
// The record already holds everything needed to know: a prompt was submitted, tools ran, and no
// turn.finished ever arrived. What was missing is anyone asking. A session resumed after a crash
// (or a kill, or a laptop closing) came back with the conversation intact and the work abandoned —
// the agent did not pick it up, and the only sign that forty steps had been thrown away was the
// user remembering they had asked for something.
//
// This is a READ over the log, not a new record. Nothing is written to decide it, so a log written
// by any earlier version answers the question the same way, and a crash between two events cannot
// leave the answer half-updated.

// UnfinishedTurn is a prompt whose turn never ended.
type UnfinishedTurn struct {
	MessageID string
	Text      string
	Steps     int // tool calls the abandoned turn had already made — what would be lost
}

// UnfinishedTurnOf reports the last prompt in a session that has no turn.finished after it.
//
// Only the LAST prompt can be unfinished: a later prompt means the user moved on, whatever the log
// says about the earlier one, and re-running something they have already superseded would be worse
// than dropping it.
func (a *App) UnfinishedTurnOf(ctx context.Context, sid session.SessionID) (UnfinishedTurn, bool) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return UnfinishedTurn{}, false
	}
	return unfinishedTurn(evs)
}

func unfinishedTurn(evs []event.Event) (UnfinishedTurn, bool) {
	var last UnfinishedTurn
	var open bool
	steps := 0
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			// A new prompt opens a turn and closes the question about the previous one.
			last, open, steps = UnfinishedTurn{MessageID: d.MessageID, Text: promptText(d)}, true, 0
		case event.TypeTurnFinished:
			open = false
		case event.TypePromptAbandoned:
			// A prompt nobody is going to answer is not an open turn.
			//
			// The abandonment names the prompt, and only that one closes anything: an older
			// cancellation says nothing about the request somebody made since. Two things write
			// this — the interrupt path, where a person stopped the turn, and a note the console
			// appends when it edits a file, which is a record rather than a request — and in both
			// cases what is true afterwards is that nothing is running.
			var d event.PromptAbandonedData
			if json.Unmarshal(e.Data, &d) == nil && d.MsgID != "" && d.MsgID == last.MessageID {
				open = false
			}
		case event.TypeError:
			// An error the loop recorded is an ending: the turn stopped for a reason that is in the
			// log, and the user saw it. Resuming that is re-running a failure, not rescuing work.
			open = false
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolCall {
				steps++
			}
		}
	}
	if !open || last.MessageID == "" {
		return UnfinishedTurn{}, false
	}
	last.Steps = steps
	return last, true
}

// promptText joins a prompt's text parts, which is what the user actually typed.
func promptText(d event.PromptSubmittedData) string {
	var s string
	for _, p := range d.Parts {
		if p.Kind == session.PartText {
			s += p.Text
		}
	}
	return s
}
