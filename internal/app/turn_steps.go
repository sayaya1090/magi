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

// turnStepsOf keeps the tool calls since the last user prompt that have a result. A call
// without one is the call in flight — the plugin tool asking — and is left out, so a door
// never counts itself as work the turn did.
func turnStepsOf(evs []event.Event) []port.ChildStep {
	var out []port.ChildStep
	var answered []bool
	at := map[string]int{}
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
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
			if d.Part.ToolResult.IsError {
				out[i].Output = text
			}
			answered[i] = true
		}
	}
	kept := out[:0]
	for i, st := range out {
		if answered[i] {
			kept = append(kept, st)
		}
	}
	return kept
}
