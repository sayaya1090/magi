package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// innerCallLimit bounds the calls one outer call may run, so a script with a runaway loop ends in
// words instead of filling the log.
const innerCallLimit = 200

// callInner runs one tool for the call outer, through executeTool like a model's call: the
// permission gate, the start fact and the result all apply. The call is recorded under its own name
// with outer as its Parent, which keeps it out of the model's history (rebuild) while recovery,
// the turn's steps and the council still see it.
func (a *App) callInner(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor,
	outer *session.ToolCall, guard *runGuard, turnTask string, count *int32, slots chan struct{}, name string, args json.RawMessage) (session.ToolResult, error) {
	if atomic.AddInt32(count, 1) > innerCallLimit {
		return session.ToolResult{}, fmt.Errorf("not run: this call already ran %d tools, the limit for one call", innerCallLimit)
	}
	// A subagent call is a whole child turn: a script running calls together takes a slot for each,
	// as a step's own parallel calls do (max_parallel_children), and the rest wait.
	if slots != nil && a.subagentTool(name) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-ctx.Done():
			return session.ToolResult{}, fmt.Errorf("not run: %s was still waiting for a free slot when the call ended", name)
		}
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	call := &session.ToolCall{CallID: "inner_" + newID(), Name: name, Args: args, Parent: outer.CallID}
	a.appendPart(ctx, s.ID, actor, "m_"+newID(), session.RoleAssistant, session.Part{ID: "p_" + newID(), Kind: session.PartToolCall, ToolCall: call})
	a.executeTool(ctx, s, agent, depth, actor, call, guard, turnTask)
	evs, err := a.store.Read(context.WithoutCancel(ctx), s.ID, 0)
	if err != nil {
		return session.ToolResult{}, fmt.Errorf("%s ran but its result could not be read back: %w", name, err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(evs[i].Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == call.CallID {
			return *d.Part.ToolResult, nil
		}
	}
	return session.ToolResult{}, fmt.Errorf("%s left no result", name)
}
