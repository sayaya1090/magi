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
	// spawnMaxRounds caps how many times a caller's review may send a child back. The step budget
	// already bounds the work; this bounds the number of model round trips spent on the QUESTION,
	// which the step budget does not see.
	spawnMaxRounds = 5
)

// Bounds on the WHOLE tool call, not one child. Each child is clamped above, but nothing counted
// how many a plugin starts: `while true do magi.spawn{...} end` is one step to the parent loop no
// matter how long it runs, and a tool call has no timeout of its own. Loop engineering is exactly
// the shape that spawns repeatedly, so the budget belongs at the call.
//
// Cumulative STEPS rather than a spawn count: how many rounds a loop wants is the plugin's
// business, and how much model work the host will hand one tool call is the host's. The clock is
// here because a child can burn its own 15 minutes doing almost no steps.
//
// Vars, not consts, so a test can lower them and actually reach the refusal — at the shipped
// numbers that would take hundreds of child runs.
var (
	spawnCallStepBudget = 240
	spawnCallDeadline   = 45 * time.Minute
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

	// Its own checkout, when the caller asked for one. Done BEFORE the session exists so a clone
	// that cannot be made fails the spawn outright instead of leaving a child session behind with
	// nowhere to work.
	workdir, workspace, base := parent.Workdir, "", ""
	if strings.EqualFold(strings.TrimSpace(spec.Workspace), "clone") {
		dir, b, err := a.cloneWorkspace(ctx, parent.Workdir)
		if err != nil {
			return port.SpawnResult{}, fmt.Errorf("spawn: %w", err)
		}
		workdir, workspace, base = dir, dir, b
	}

	// Parent is what keeps the child out of the resume list; the store already hides sessions that
	// carry one.
	child, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: workdir,
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

	// Put the child on the strip before it starts, so a pane exists for the whole time it runs
	// rather than appearing once it has something to say.
	a.subJobs.start(child, spec.ToolName, prompt)
	stop := a.forwardChildProgress(cctx, child, onProgress)
	defer stop()

	agent := AgentSpec{Name: spawnAgentName, System: spec.System, Tools: spec.Tools, Model: model,
		Provider: spec.Provider, Groups: spec.Groups}

	// Round-trip with the caller's review, if it set one. Each round runs the SAME session, so the
	// child keeps what it has already read — that is the whole reason this is a loop here rather
	// than the caller spawning a second child, which would start by re-gathering what the first one
	// knew. The step budget is the child's TOTAL, spent down across rounds, so a review that never
	// accepts runs out of steps rather than running forever.
	var text string
	var rerr error
	left, rounds := steps, 0
	for {
		rounds++
		text, rerr = a.runLoop(cctx, a.sessionInfo(cctx, child), agent, 1, left, true)
		if rerr != nil || cctx.Err() != nil || spec.Review == nil {
			break
		}
		spent := a.countTurns(ctx, child)
		if left = steps - spent; left <= 0 {
			break // the child has spent its whole budget; there is nothing to send it back with
		}
		if rounds >= spawnMaxRounds {
			break
		}
		more, err := spec.Review(rounds, text, spent)
		if err != nil {
			// The caller's own judgement failed. Report it rather than treating a broken review as
			// acceptance — "it did not answer" and "it said yes" are different facts.
			rerr = fmt.Errorf("review round %d: %w", rounds, err)
			break
		}
		if strings.TrimSpace(more) == "" {
			break // accepted
		}
		// VERBATIM again, for the same reason the first prompt is: what the caller sends back is
		// the instruction, and a paraphrase of it is how the identifiers go missing.
		if err := a.appendPromptText(ctx, child, event.Actor{Kind: event.ActorUser, ID: spawnAgentName}, more); err != nil {
			rerr = fmt.Errorf("review round %d: seed: %w", rounds, err)
			break
		}
	}

	// Steps is the child's model round trips. Nothing set it before, so a caller reading it saw
	// zero however much work the child did — and the per-call budget that charges it was really
	// counting spawns. Counted from the log because runLoop reports text and an error, not a count.
	// Commit whatever the child left behind, so what it did is a COMMIT RANGE and not a pile of
	// loose files. base..HEAD is then exactly the child's work — the parent's own uncommitted
	// changes are below the baseline and do not travel home as though the child had written them.
	head := base
	if workspace != "" {
		if h, cerr := commitAll(ctx, workspace, "magi: "+spec.ToolName+" — what the child changed"); cerr == nil {
			head = h
		} else if rerr == nil {
			rerr = cerr
		}
	}
	res := port.SpawnResult{SessionID: string(child), Text: text, Steps: a.countTurns(ctx, child),
		Workspace: workspace, Branch: childBranch, BaseCommit: base, HeadCommit: head}
	defer func() { a.subJobs.finish(child, res.Steps, res.Err) }()
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
func (a *App) spawnFnFor(depth int, s session.Session, actor event.Actor, callID, toolName string) (
	func(context.Context, port.SpawnSpec) (port.SpawnResult, error),
	func(context.Context, string) ([]port.ChildStep, error),
	func(context.Context, string) ([]port.RestoredPath, error),
	func(context.Context, string) error,
) {
	if depth != 0 {
		return nil, nil, nil, nil
	}
	// One budget per tool call, closed over by every spawn the call makes. spawnFnFor runs once
	// per call (execute.go), so these are per-call state and not per-App.
	var (
		mu        sync.Mutex
		usedSteps int
		children  int
		started   time.Time
		mine      = map[string]bool{} // children THIS call spawned; childStepsFnFor answers for these only
		// Where each child worked, so a later merge names a RANGE instead of a directory the
		// caller had to remember. Per tool call, like the ownership set above it.
		spaces = map[string][3]string{} // sid -> {workspace, base, head}
	)
	spawn := func(sctx context.Context, spec port.SpawnSpec) (port.SpawnResult, error) {
		// Check and reserve under the lock. Spawns from one plugin serialise on its own mutex
		// today, but the budget must not depend on that staying true.
		mu.Lock()
		if started.IsZero() {
			started = time.Now()
		}
		spent, kids, elapsed := usedSteps, children, time.Since(started)
		mu.Unlock()
		// Say which bound and what it was. A plugin told only "refused" cannot tell a budget from
		// a crash, and its loop will keep asking.
		if spent >= spawnCallStepBudget {
			return port.SpawnResult{}, fmt.Errorf(
				"spawn: this tool call has used its whole child-step budget (%d/%d steps across %d children); "+
					"return what you have to the caller instead of starting another",
				spent, spawnCallStepBudget, kids)
		}
		if elapsed >= spawnCallDeadline {
			return port.SpawnResult{}, fmt.Errorf(
				"spawn: this tool call has been running children for %s (bound %s); return what you have",
				elapsed.Round(time.Second), spawnCallDeadline)
		}
		// Do not let one child exceed what is left, or the budget overshoots by a whole child.
		// Fill the default FIRST: clamping a spec that asked for nothing would otherwise read 0 as
		// "one step", and every child a plugin started without naming a count would run once.
		if spec.MaxSteps <= 0 {
			spec.MaxSteps = spawnDefaultSteps
		}
		spec.MaxSteps = min(spec.MaxSteps, spawnCallStepBudget-spent) // >= 1: spent < budget above
		// The user's choice in /subagents outranks what the plugin asked for. Planning on a strong
		// model while the work runs on a cheap one is the case this exists for, and the setting
		// belongs where the user can see and change it — not in a plugin's own config section,
		// which the /subagents screen has no way to edit.
		spec.ToolName = toolName // the host names it, not the plugin
		if m, pv := a.subagentOverride(toolName); m != "" || pv != "" {
			if m != "" {
				spec.Model = m
			}
			if pv != "" {
				spec.Provider = pv
			}
		}
		res, err := a.spawnChild(sctx, s, actor, spec, func(line string) {
			a.emitToolProgress(s.ID, actor, callID, toolName, line)
		})
		// Charge what the child actually spent, including when it failed — a child that burned
		// twenty steps and then errored consumed the same model work as one that succeeded, and a
		// loop that only pays for successes is not bounded at all. The floor of 1 is so a child
		// that produced nothing measurable still costs something; without it a failing spawn is
		// free and the loop is unbounded again.
		mu.Lock()
		usedSteps += max(res.Steps, 1)
		children++
		if res.SessionID != "" {
			mine[res.SessionID] = true
			spaces[res.SessionID] = [3]string{res.Workspace, res.BaseCommit, res.HeadCommit}
		}
		mu.Unlock()
		return res, err
	}

	restore := func(sctx context.Context, sid string) ([]port.RestoredPath, error) {
		mu.Lock()
		ok := mine[sid]
		mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("restore: %q was not spawned by this tool call", sid)
		}
		out := a.RestoreChild(sctx, session.SessionID(sid))
		paths := make([]port.RestoredPath, 0, len(out))
		for _, r := range out {
			paths = append(paths, port.RestoredPath{
				Path: r.Path, Restored: r.Restored, How: r.How, Reason: r.Reason})
		}
		return paths, nil
	}

	merge := func(sctx context.Context, sid string) error {
		mu.Lock()
		ok, w := mine[sid], spaces[sid]
		mu.Unlock()
		if !ok {
			return fmt.Errorf("merge: %q was not spawned by this tool call", sid)
		}
		return a.MergeChildWork(sctx, s.Workdir, w[0], w[1], w[2])
	}

	steps := func(sctx context.Context, sid string) ([]port.ChildStep, error) {
		mu.Lock()
		ok := mine[sid]
		mu.Unlock()
		if !ok {
			// Naming a session this call did not spawn is not a lookup failure to paper over.
			return nil, fmt.Errorf("child steps: %q was not spawned by this tool call", sid)
		}
		return a.childSteps(sctx, session.SessionID(sid))
	}
	return spawn, steps, restore, merge
}

// countTurns counts the child's model round trips: one per distinct assistant message in its log.
// Best effort — a read failure yields 0 rather than failing a run that has already produced work,
// and the caller's budget applies its own floor.
func (a *App) countTurns(ctx context.Context, sid session.SessionID) int {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Role != session.RoleAssistant || d.MessageID == "" {
			continue
		}
		seen[d.MessageID] = true
	}
	return len(seen)
}

// resultText unwraps a tool result's JSON string encoding and CHANGES NOTHING ELSE.
//
// The package's other decoder (toolResultText) replaces newlines with a marker because it feeds a
// one-line council row. A stack trace with its newlines flattened is not the raw output the caller
// asked for, so this one keeps them — no trimming, no clipping, no substitution.
func resultText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	return string(raw)
}

// childSteps reads one child's log and returns the tool calls it made, paired with their results.
//
// Ordering comes from the log itself, and a call with no result (the child was cut off mid-tool) is
// still reported — a step that never finished is exactly the kind of thing a caller deciding
// whether to retry needs to see, and dropping it would make the child look like it stopped one tool
// earlier than it did.
func (a *App) childSteps(ctx context.Context, sid session.SessionID) ([]port.ChildStep, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, fmt.Errorf("child steps: %w", err)
	}
	var out []port.ChildStep
	at := map[string]int{} // callID -> index in out, so a result finds the call it belongs to
	for _, e := range evs {
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
		case d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil:
			i, ok := at[d.Part.ToolResult.CallID]
			if !ok {
				continue // a result whose call is not in this log; nothing to attach it to
			}
			text := resultText(d.Part.ToolResult.Content)
			out[i].OutputBytes = len(text)
			out[i].Failed = d.Part.ToolResult.IsError
			// Verbatim, and only for a failure — see the ChildStep doc for why the successful
			// output is left behind.
			if d.Part.ToolResult.IsError {
				out[i].Output = text
			}
		}
	}
	return out, nil
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
