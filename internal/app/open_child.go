package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// openChild opens a child session of parent that the tool running outer drives itself — no model
// runs in it. It is made like a spawned child (CreateSession with Parent, the task line as its
// prompt, a place on the subagent strip), and each call the tool runs in it goes through callInner
// against the CHILD, so the start fact, the permission gate and the result land in the child's log
// and the parent's log keeps only outer and its result. No loop runs in it to write a
// turn.finished on its own; Close writes it (after the reason, when the script failed), takes the
// child off the strip and drops its state.
func (a *App) openChild(ctx context.Context, parent session.Session, agent AgentSpec, depth int, actor event.Actor,
	outer *session.ToolCall, guard *runGuard, turnTask string, count *int32, slots chan struct{}, label string) (port.ChildRun, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = outer.Name
	}
	child, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: parent.Workdir,
		Parent:  string(parent.ID),
		Agent:   spawnAgentName,
		Model:   parent.Model,
		Actor:   actor,
	})
	if err != nil {
		return port.ChildRun{}, fmt.Errorf("open child: %w", err)
	}
	if err := a.appendPromptText(ctx, child, event.Actor{Kind: event.ActorUser, ID: spawnAgentName}, label); err != nil {
		return port.ChildRun{}, fmt.Errorf("open child: seed: %w", err)
	}
	a.subJobs.start(child, outer.Name, label)
	cs := a.sessionInfo(ctx, child)
	var once sync.Once
	return port.ChildRun{
		SessionID: string(child),
		CallTool: func(cctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
			return a.callInner(cctx, cs, agent, depth, actor, outer, guard, turnTask, count, slots, name, args)
		},
		Close: func(text string, failed error) {
			once.Do(func() {
				bg := context.WithoutCancel(ctx)
				if text = strings.TrimSpace(text); text != "" {
					a.appendPart(bg, child, actor, "m_"+newID(), session.RoleAssistant,
						session.Part{ID: "p_" + newID(), Kind: session.PartText, Text: text})
				}
				why := ""
				if failed != nil {
					why = failed.Error()
					a.emitError(bg, child, actor, "the script stopped: "+why)
				}
				// The child's own log says it ended, so recovery and a screen read it as done (or failed)
				// from the child alone; without this a finished child reads as a turn cut off.
				d, _ := json.Marshal(event.TurnFinishedData{})
				a.appendBestEffort(bg, child, event.TypeTurnFinished, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
				a.subJobs.finish(child, 0, why)
				a.mu.Lock()
				delete(a.states, child)
				a.usage.forget(child)
				a.mu.Unlock()
				a.liveness.Delete(child)
			})
		},
	}, nil
}
