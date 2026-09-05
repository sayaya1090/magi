package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// turnSteps is the current turn's tool calls of sid — what port.ToolEnv.TurnSteps hands a
// plugin tool. Same shape as childSteps, scoped to the turn instead of the whole log.
func (a *App) turnSteps(ctx context.Context, sid session.SessionID) ([]port.ChildStep, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, fmt.Errorf("turn steps: %w", err)
	}
	return turnStepsOf(evs), nil
}

// turnStepOutputCap bounds an OK result's text in a turn step; a failed one travels whole.
const turnStepOutputCap = 6000

// turnStepsOf keeps the tool calls since the last user prompt that have a result. A call
// without one is the call in flight — the plugin tool asking — and is left out, so a door
// never counts itself as work the turn did.
func turnStepsOf(evs []event.Event) []port.ChildStep {
	return stepsOf(evs, true)
}

// stepsOf is the one reader of tool calls and their results from a session log. turnOnly
// resets at each user prompt and drops unanswered calls (the turn's own door asking);
// otherwise the whole log is read and an unanswered call stays — a child cut off mid-tool
// must show the tool it was in (childSteps). An OK result's text is clipped, a failure's
// travels whole.
func stepsOf(evs []event.Event, turnOnly bool) []port.ChildStep {
	var out []port.ChildStep
	var answered []bool
	at := map[string]int{}
	for _, e := range evs {
		if turnOnly && e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			out, answered, at = nil, nil, map[string]int{}
			continue
		}
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch {
		case d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil:
			at[d.Part.ToolCall.CallID] = len(out)
			out = append(out, port.ChildStep{Name: d.Part.ToolCall.Name, Args: d.Part.ToolCall.Args})
			answered = append(answered, false)
		case d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil:
			i, ok := at[d.Part.ToolResult.CallID]
			if !ok {
				continue
			}
			text := resultText(d.Part.ToolResult.Content)
			out[i].OutputBytes = len(text)
			out[i].Failed = d.Part.ToolResult.IsError
			switch {
			case d.Part.ToolResult.IsError:
				out[i].Output = text // a failure travels whole, in both modes
			case !turnOnly:
				// childSteps' contract: an OK result carries its size, not its text (a parent
				// reads what a child did, not everything it read).
			case len(text) <= turnStepOutputCap:
				out[i].Output = text
			default:
				out[i].Output = text[:turnStepOutputCap] + "…[clipped]"
			}
			answered[i] = true
		}
	}
	if !turnOnly {
		return out
	}
	kept := out[:0]
	for i, st := range out {
		if answered[i] {
			kept = append(kept, st)
		}
	}
	return kept
}
