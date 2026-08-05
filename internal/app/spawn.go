package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Bounds a spawned child runs inside. They are deliberately dumb numbers.
//
// The machinery that came out of this tree bounded a child with an elastic attempt cap keyed on
// observed model latency, a lease ladder with five deterministic arms, an LLM judge to decide the
// sixth, and a backstop credit — several hundred lines whose job was to guess whether a child that
// had gone quiet was still worth waiting for. None of it is coming back. A child gets a step count
// and a clock, both from the caller, both clamped here.
const (
	spawnMaxSteps       = 60               // hard ceiling on a child's steps, whatever the spec asks
	spawnDefaultSteps   = 20               // when the spec asks for none
	spawnMaxTimeout     = 15 * time.Minute // hard ceiling on a child's wall clock
	spawnDefaultTimeout = 5 * time.Minute
	spawnAgentName      = "spawn" // what a child is called in its events and the parent's progress line
)

// spawnChild runs one child agent to completion and returns what it produced. onProgress, when
// non-nil, receives a line per tool the child finishes — see forwardChildProgress.
//
// It is synchronous on purpose. A background child needs a lease, a reaper, and a way to show a
// user that something is running they cannot see — the three things that made the removed version
// large. Here the parent's tool call blocks, the parent's spinner keeps turning, and the child's
// progress is forwarded onto the parent's own stream.
//
// Everything it needs already exists: runLoop takes a depth, and every depth>0 branch in the loop
// (interjection detection, route_interjection, ask_user, the top-level contract reset) is already
// written for a child. What this adds is the four things that had no owner once the spawn path was
// removed — a session with a parent, the parent's scratch, a bounded context registered for
// interrupt and shutdown, and the seed.
func (a *App) spawnChild(ctx context.Context, parent session.Session, actor event.Actor, spec port.SpawnSpec, onProgress func(string)) (port.SpawnResult, error) {
	prompt := strings.TrimSpace(spec.Prompt)
	if prompt == "" {
		return port.SpawnResult{}, fmt.Errorf("spawn: prompt is required")
	}
	steps := spec.MaxSteps
	if steps <= 0 {
		steps = spawnDefaultSteps
	}
	steps = min(steps, spawnMaxSteps)
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = spawnDefaultTimeout
	}
	timeout = min(timeout, spawnMaxTimeout)

	model := parent.Model
	if spec.Model != "" {
		model.Model = spec.Model
	}
	if spec.Provider != "" {
		model.Provider = spec.Provider
	}

	// Parent is what keeps the child out of the resume list; the store already hides sessions that
	// carry one.
	child, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: parent.Workdir,
		Parent:  string(parent.ID),
		Agent:   spawnAgentName,
		Model:   model,
		Actor:   actor,
	})
	if err != nil {
		return port.SpawnResult{}, fmt.Errorf("spawn: %w", err)
	}

	// The scratch directory is created at depth 0 only, and scratchFor reads the session's OWN
	// pointer — so without this the child's tools are handed empty log/tmp paths. Sharing the
	// parent's is what scratch.go's own comment describes: a child inherits the pointer and must
	// not remove it.
	a.setScratch(child, a.scratchFor(parent.ID))

	// Seed it VERBATIM. seedTurnTask at depth>0 takes the last user prompt from the child's log,
	// so this one prompt is the whole task, unrewritten.
	if err := a.appendPromptText(ctx, child, event.Actor{Kind: event.ActorUser, ID: spawnAgentName}, prompt); err != nil {
		return port.SpawnResult{SessionID: string(child)}, fmt.Errorf("spawn: seed: %w", err)
	}

	// Derived from the PARENT's ctx, so cancelling the parent turn cancels the child. Registered on
	// the child's own state so Interrupt reaches it by session id, and on the wait group so Close
	// does not return while a child is still writing to the store.
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return port.SpawnResult{SessionID: string(child)}, fmt.Errorf("spawn: shutting down")
	}
	a.stateLocked(child).cancel = cancel
	a.wg.Add(1)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.stateLocked(child).cancel = nil
		a.mu.Unlock()
		a.wg.Done()
	}()

	stop := a.forwardChildProgress(cctx, child, onProgress)
	defer stop()

	agent := AgentSpec{Name: spawnAgentName, System: spec.System, Tools: spec.Tools, Model: model, Provider: spec.Provider}
	text, rerr := a.runLoop(cctx, a.sessionInfo(cctx, child), agent, 1, steps, true)

	res := port.SpawnResult{SessionID: string(child), Text: text}
	switch {
	case rerr != nil:
		res.Err = rerr.Error()
	case cctx.Err() != nil:
		// Say which bound was hit. A caller handed a truncated answer with no reason cannot tell it
		// apart from a short one.
		res.Err = fmt.Sprintf("child stopped: %v (bound %s)", cctx.Err(), timeout)
	}
	return res, nil
}

// spawnFnFor is the Spawn hook a tool at this depth gets, and nil is the answer for a CHILD.
//
// A child with no hook cannot spawn, which makes recursion impossible by construction rather than
// bounded by a counter somebody has to remember to check. It is a named method so a test can ask
// the question directly instead of inferring it from a tool call that never happens.
func (a *App) spawnFnFor(depth int, s session.Session, actor event.Actor, callID, toolName string) func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
	if depth != 0 {
		return nil
	}
	return func(sctx context.Context, spec port.SpawnSpec) (port.SpawnResult, error) {
		// The user's choice in /subagents outranks what the plugin asked for. Planning on a strong
		// model while the work runs on a cheap one is the case this exists for, and the setting
		// belongs where the user can see and change it — not in a plugin's own config section,
		// which the /subagents screen has no way to edit.
		if m, pv := a.subagentOverride(toolName); m != "" || pv != "" {
			if m != "" {
				spec.Model = m
			}
			if pv != "" {
				spec.Provider = pv
			}
		}
		return a.spawnChild(sctx, s, actor, spec, func(line string) {
			a.emitToolProgress(s.ID, actor, callID, toolName, line)
		})
	}
}

// forwardChildProgress relays what the child is doing onto the PARENT's transcript, returning a
// stop function the caller defers.
//
// The child's events are published under the child's session id and the TUI subscribes to exactly
// one — so without this a spawn that runs for minutes is a silent gap on screen. It is a summary,
// not a mirror: one clipped line per tool call the child makes, on the channel a long-running tool
// already uses for its heartbeat.
func (a *App) forwardChildProgress(ctx context.Context, child session.SessionID, onProgress func(string)) func() {
	if onProgress == nil {
		return func() {}
	}
	ch, cancel := a.bus.Subscribe(ctx, child)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		n := 0
		for e := range ch {
			if e.Type != event.TypePartAppended {
				continue
			}
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
				continue
			}
			n++
			onProgress(fmt.Sprintf("%s · step %d · %s", spawnAgentName, n, d.Part.ToolCall.Name))
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
