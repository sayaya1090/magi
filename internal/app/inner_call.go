package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// scriptFree reports whether a tool's script may run name without asking anyone. It is decided by
// what the tool IS, never by a list of names some plugin needs: a script runs many calls the model
// never looks at one by one, so only a tool that just looks (the read-only four), has no outside
// effect, or spawns children that can only look runs freely. Anything else (a write, the shell, the
// web, an MCP server) goes to the person first, whatever the permission mode. Which tools a plugin's
// scripts may reach at all is the plugin's own declaration, checked where the plugin is hosted.
func (a *App) scriptFree(name string) bool {
	if readOnlyTools[name] || noOutsideEffect[name] {
		return true
	}
	if a.tools == nil {
		return false
	}
	t, ok := a.tools.Get(name)
	if !ok {
		return false
	}
	m := port.ToolMetaOf(t)
	return m.Subagent && m.ReadOnlyChildren
}

type scriptedKey struct{}

// asScripted marks a call a tool's script asked for that is not scriptFree: the permission gate
// asks the person for it in every mode, and with nobody to ask it is refused.
func asScripted(ctx context.Context) context.Context {
	return context.WithValue(ctx, scriptedKey{}, true)
}

func isScripted(ctx context.Context) bool { v, _ := ctx.Value(scriptedKey{}).(bool); return v }

// innerCallLimit bounds the calls one outer call may run, so a script with a runaway loop ends in
// words instead of filling the log.
const innerCallLimit = 200

// callInner runs one tool for the call outer, through executeTool like a model's call: the
// permission gate, the start fact and the result all apply. The call is recorded under its own name
// with outer as its Parent, which keeps it out of the model's history (rebuild) while recovery,
// the turn's steps and the council still see it.
func (a *App) callInner(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor,
	outer *session.ToolCall, guard *runGuard, turnTask string, count *int32, slots chan struct{}, name string, args json.RawMessage) (session.ToolResult, error) {
	if !a.scriptFree(name) {
		ctx = asScripted(ctx)
	}
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
