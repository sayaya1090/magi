package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// gateAllowlist blocks a tool the agent isn't permitted to call. Returns true to stop.
func (a *App) gateAllowlist(ctx context.Context, sid session.SessionID, actor event.Actor, agent AgentSpec, tc *session.ToolCall, toolMsgID string) bool {
	if agent.allows(tc.Name) {
		return false
	}
	a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, "tool not permitted for agent "+agent.Name, true)
	return true
}

// gatePermission applies the guardrail policy (a hard deny blocks regardless of mode) and
// prompts for dangerous or policy-forced tool calls, recording the PermissionDecided fact.
// Returns true to stop (policy deny, or the user denied the prompt).
func (a *App) gatePermission(ctx context.Context, sid session.SessionID, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	verdict, reason := a.policy.Decide(tc.Name, tc.Args)
	if verdict == "deny" {
		dd, _ := json.Marshal(event.PermissionDecidedData{CallID: tc.CallID, Decision: "deny"})
		a.appendFact(ctx, sid, event.TypePermissionDecided, actor, dd)
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, "blocked by policy: "+reason, true)
		return true
	}
	forcePrompt := verdict == "ask"
	if !forcePrompt {
		reason = "" // routine danger-tool confirmation, not a policy hit
	}
	if (a.cfg.DangerTools[tc.Name] || forcePrompt) && !a.policy.AllowedByRule(tc.Name, tc.Args) {
		allowed := a.requestPermission(ctx, sid, actor, tc, forcePrompt, reason)
		decision := "allow"
		if !allowed {
			decision = "deny"
		}
		dd, _ := json.Marshal(event.PermissionDecidedData{CallID: tc.CallID, Decision: decision})
		a.appendFact(ctx, sid, event.TypePermissionDecided, actor, dd)
		if !allowed {
			a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, a.denyReason(tc.Name), true)
			return true
		}
	}
	return false
}

// denyReason explains a permission denial to the agent as its tool result. In a
// headless run there is no human to say "no": requestPermission denied the call
// categorically because the permission mode (ask/auto/deny) cannot approve a
// dangerous/forced tool with no interactive prompt. Reporting that as "denied by
// user" is a lie that invites a pointless retry — the agent re-issues the same call,
// gets denied again, and flails. So name the real cause and tell it not to retry.
// With an interactive human present it genuinely WAS a user decision.
func (a *App) denyReason(tool string) string {
	if a.cfg.Interactive {
		return "denied by user"
	}
	return fmt.Sprintf(
		"%s is unavailable in this headless run: permission mode %q cannot approve it with no interactive prompt. "+
			"Do not retry this tool — continue without it, or stop and report that it could not be run and why. "+
			"(The operator can re-run with --permission allow to enable it.)",
		tool, a.Permission())
}

// gatePreHooks runs PreToolUse hooks, which can block execution (e.g. protect paths).
// Returns true to stop.
func (a *App) gatePreHooks(ctx context.Context, s session.Session, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	if block := a.runPreToolHooks(ctx, s.Workdir, tc.Name, pathArg(tc.Args)); block != "" {
		a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID, "blocked by hook: "+block, true)
		return true
	}
	return false
}

// executeTool runs one tool call (with permission gating) and persists the result.
func (a *App) executeTool(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor, tc *session.ToolCall, guard *runGuard) {
	sid := s.ID
	workdir := s.Workdir
	toolMsgID := "m_" + newID()

	// Loop guard: refuse an identical tool call repeated past the limit, telling
	// the model to stop repeating. This breaks re-read / re-dispatch / echo loops
	// for every agent (orchestrator and subagents alike) without killing the turn.
	var guardFP string
	var guardNovel bool // this call's first occurrence this epoch (check n==1) — for D18a exercise novelty
	if guard != nil {
		block, n, fp := guard.check(tc.Name, tc.Args)
		guardFP = fp
		guardNovel = n == 1
		if block {
			msg := fmt.Sprintf(
				"Loop guard: you have already made this exact %q call %d times with nothing changed since. "+
					"Stop repeating it — take a different step, or finish and summarize. (Edit a file and the same "+
					"command is allowed again, since that's real progress.)",
				tc.Name, n)
			// Read fixation: re-reading the same file head — often paced past the guard by
			// nudging offset/limit — is the classic no-progress loop. We already hold the
			// content (appended below), so steer directly OFF another read instead of the
			// generic "take a different step", which a fixated model tends to satisfy with
			// yet another read.
			if tc.Name == "read" {
				msg = fmt.Sprintf(
					"Loop guard: you have already read this %d times and its contents (below) have not changed — "+
						"reading it again cannot make progress. Do NOT read it again. Take the next real action: make "+
						"the edit/write you were about to make, inspect a DIFFERENT file or region, or finish and "+
						"summarize. If you are waiting for this file to change, do not poll it with read — use the "+
						"wait_for tool (if available) to block until it actually changes.",
					n)
			}
			if last := guard.lastResult(fp); last != "" {
				msg += "\n\nThe earlier result (unchanged) was:\n" + last
			}
			a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, msg, true)
			return
		}
	}

	// Pre-execution gates, run in order — the first that blocks emits its own tool result
	// and stops the call (allowlist → guardrail policy/permission prompt → PreToolUse hooks).
	if a.gateAllowlist(ctx, sid, actor, agent, tc, toolMsgID) ||
		a.gatePermission(ctx, sid, actor, tc, toolMsgID) ||
		a.gatePreHooks(ctx, s, actor, tc, toolMsgID) {
		return
	}

	// Tool-env callbacks: background dispatch for the top-level orchestrator; escalation
	// (ask) + report for subagents, routed THROUGH the parent so full context is kept.
	var dispatchFn func(port.SpawnRequest) string
	var cancelDispatchFn func(agent, reason string) (int, error)
	var resolveConcernFn func(key, reason string) error
	if depth == 0 {
		dispatchFn = func(req port.SpawnRequest) string { return a.dispatch(ctx, s, depth, req) }
		cancelDispatchFn = func(agent, reason string) (int, error) {
			return a.cancelDispatched(ctx, s.ID, agent, reason)
		}
		// Only the orchestrator, which holds the whole-task view, may retire a concern —
		// and only advisorily: a still-true concern is re-raised next turn (self-healing),
		// so this cannot launder a fact away, only clear stale advisory memory.
		resolveConcernFn = func(key, reason string) error {
			return a.appendConcernResolved(context.WithoutCancel(ctx), sid,
				event.Actor{Kind: event.ActorAgent, ID: "orchestrator"}, key, "orchestrator", reason)
		}
	}
	// Route a mid-turn user interjection (top-level only — subagents aren't steered by
	// the user). The tool has already validated action ∈ {queue,redirect,append}; we
	// record the signal for the loop to drain and apply at its next step.
	var routeInterjectionFn func(action, reason, requestID string) error
	if depth == 0 {
		routeInterjectionFn = func(action, reason, requestID string) error {
			if !a.hasPendingInterject(sid) {
				return fmt.Errorf("there is no queued interjection to route right now")
			}
			a.signalTurnControl(sid, func(tc *turnControl) {
				tc.route = action
				tc.routeID = requestID
				if reason != "" {
					tc.reason = reason
				}
			})
			return nil
		}
	}
	// Agent-initiated replan for a plan-eligible agent (write-capable, below the plan-
	// depth cap). Records the signal; the loop enforces the per-turn budget and rebuild.
	var replanFn func(reason string) error
	if a.planEligible(agent, depth) {
		replanFn = func(reason string) error {
			a.signalTurnControl(sid, func(tc *turnControl) {
				tc.replan = true
				if reason != "" {
					tc.reason = reason
				}
			})
			return nil
		}
	}
	var askFn func(string) (string, error)
	var reportFn func(summary, status, details string) error
	if s.Parent != "" {
		reportFn = func(summary, status, details string) error { return a.fileReport(sid, summary, status, details) }
		if s.Escalatable {
			// Background-dispatched: the orchestrator stays in its loop and answers asks.
			askFn = func(q string) (string, error) { return a.escalate(ctx, s.Parent, agent.Name, q) }
		} else {
			// Synchronous spawn (planner explorer / nested subagent): the parent is blocked
			// awaiting THIS child, so nothing can answer — fail fast with guidance instead of
			// blocking until the 2-minute escalation timeout.
			askFn = func(string) (string, error) {
				return "", fmt.Errorf("no orchestrator is available to answer while you investigate — " +
					"proceed with your best assumption and note any ambiguity in your final report")
			}
		}
	}

	st, _ := json.Marshal(event.ToolStartedData{CallID: tc.CallID, Name: tc.Name})
	a.publishTransient(sid, event.TypeToolStarted, actor, st)

	tool, ok := a.tools.Get(tc.Name)
	if !ok {
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, "unknown tool: "+tc.Name, true)
		return
	}
	// For a file edit, snapshot the file's content BEFORE the tool runs so the council can
	// be shown the agent's actual before→after change (reconstructed from its own tools).
	var changeBefore, changePath string
	if guard != nil && fileModifiers[tc.Name] {
		changePath = pathArg(tc.Args)
		if changePath != "" {
			changeBefore = readForChange(workdir, changePath)
		}
	}
	// Mark a tool as in flight so the stall watchdog does not kill a child that is
	// legitimately blocked in a long, silent tool (e.g. a multi-minute bash). Held
	// through the rest of executeTool (result append / council eval), which is short.
	a.enterTool(sid)
	defer a.leaveTool(sid)
	res, err := tool.Execute(ctx, tc.Args, port.ToolEnv{
		SessionID:    sid,
		Workdir:      workdir,
		Platform:     a.plat,
		EmitArtifact: func(art artifact.Artifact) { a.emitArtifact(ctx, sid, actor, art) },
		EmitProgress: func(text string) { a.emitToolProgress(sid, actor, tc.CallID, tc.Name, text) },
		Spawn: func(sctx context.Context, req port.SpawnRequest) port.SpawnResult {
			return a.spawn(sctx, s, depth, req)
		},
		// Background dispatch is offered only to the top-level orchestrator; nested
		// subagents delegate synchronously (they have no UI thread to keep free).
		Dispatch:          dispatchFn,
		CancelDispatch:    cancelDispatchFn,
		ResolveConcern:    resolveConcernFn,
		RouteInterjection: routeInterjectionFn,
		Replan:            replanFn,
		Ask:               askFn,
		AskUser:           a.askUserFn(ctx, s, depth, tc),
		Report:            reportFn,
		SetTodos:          func(td []session.Todo) { a.putTodos(ctx, sid, actor, td) },
		Propose: func(c port.Contribution) error {
			if a.cfg.Experience == nil {
				return fmt.Errorf("shared experience not configured")
			}
			return a.cfg.Experience.Propose(ctx, c)
		},
		LoadSkill: func(name string) (string, bool) { return a.skillBody(s.Workdir, name) },
		Recall: func(query string) (string, error) {
			// Budget/dedupe is applied inside recallContext, keyed on the RESOLVED topic
			// (so two phrasings of one topic don't both spend it, and a miss is free).
			return a.recallContext(ctx, sid, query, guard)
		},
		RecallMemory: func(query string) (string, error) {
			if a.cfg.Experience == nil {
				return "", fmt.Errorf("shared experience not configured")
			}
			mems, skills, err := a.cfg.Experience.Retrieve(ctx, query)
			if err != nil {
				return "", err
			}
			return formatExperienceFull(mems, skills), nil
		},
		Sandbox: port.SandboxSpec{Mode: a.cfg.Sandbox, Workdir: workdir},
	})
	if err != nil {
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, string(capToolResult([]byte(err.Error()))), true)
		return
	}
	res.CallID = tc.CallID
	// Cap a single tool result so one giant output (e.g. reading a 500KB file) can't blow
	// the model's context window past what compaction can recover (compaction can't summarize
	// a result that's still in the recent, un-compacted window). Truncate the raw output here,
	// before diagnostics are appended, so the agent is told to narrow its read/command.
	res.Content = capToolResult(res.Content)
	// The tool's OWN success, before post-edit diagnostics/hooks below flip IsError: a write
	// that landed but fails gofmt/a hook still changed the file, and the council must see
	// that (broken) change — so change capture keys off this, not the post-diagnostics flag.
	toolOK := !res.IsError

	// Post-edit diagnostics + PostToolUse hooks: feed problems back so the agent
	// self-corrects (built-in autoformat runs here too).
	if !res.IsError && fileModifiers[tc.Name] {
		path := pathArg(tc.Args)
		if h := a.runPostToolHooks(ctx, workdir, tc.Name, path); h != "" {
			res.Content = appendToContent(res.Content, "\n\n"+h)
			res.IsError = true
		}
		diag, advice := a.diagnose(ctx, workdir, path)
		if diag != "" {
			res.Content = appendToContent(res.Content, "\n\n[diagnostics]\n"+diag)
			res.IsError = true
		}
		// A missing language server is an environment note, not an edit failure: the
		// edit succeeded, so attach the install hint without flipping IsError.
		if advice != "" {
			res.Content = appendToContent(res.Content, "\n\n[lsp] "+advice)
		}
	}

	// Loop guard bookkeeping: cache this call's result (so a later blocked repeat can be
	// handed it) and, on a successful file mutation, bump the epoch so identical follow-up
	// commands (e.g. re-running the test) are no longer treated as a no-progress repeat.
	mutatedReset := false // did mutated() reset the progress counters THIS call?
	if guard != nil && guardFP != "" {
		guard.record(guardFP, string(res.Content))
		if !res.IsError && fileModifiers[tc.Name] {
			mutatedReset = guard.mutated(pathArg(tc.Args), canonicalArgs(tc.Args))
		}
		// A successful bash write bumps the epoch (the tool-agnostic twin of an edit); a
		// successful non-write, non-inspect command (python/pytest/./run …) is execution
		// evidence for the current deliverable version. Together they drive the structural
		// unverifiedDeliverable signal that replaced the fabrication phrase scan.
		if !res.IsError && tc.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				guard.noteBashWrite(ba.Command)            // authored a file → epoch bump
				guard.noteBashExec(ba.Command, guardNovel) // ran a program → execution evidence (independent of any redirect)
			}
		}
		// A FAILED exercising command tabus the deliverable's current state: "this exact set
		// of file contents was tried and its test failed", so a later edit that circles back
		// to it is flagged (see checkTabu). Inspect-only failures (a bad `ls`/`grep`) are not
		// deliverable evidence and are skipped inside noteExerciseFail.
		if res.IsError && tc.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				guard.noteExerciseFail(ba.Command, string(res.Content))
			}
		}
		// Completion-banner spin: count consecutive pure no-op banners (echo/printf/true/:) an
		// agent spams to keep the turn alive after declaring done; ANY real action resets it. It
		// runs here for every call that reached guard bookkeeping, INCLUDING an IsError bash — a
		// failed real command is still a real action that must reset the spin. Banners are always
		// bash and never error, so they always reach this point. cmd is "" for non-bash tools (or
		// unparsable bash args) → not a banner → resets.
		spinCmd := ""
		if tc.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				spinCmd = ba.Command
			}
		}
		// Environment-wait signal: count a bash call that only waits/polls (any exit code — a
		// poll to a not-yet-ready endpoint FAILS while it waits) so stallIsWait can suppress the
		// futile stuck-recovery coder spawn on a stall that is really an external wait. Runs here
		// (not the success-only block above) precisely because failing polls must be counted.
		guard.noteBashWait(spinCmd)
		guard.noteSpin(tc.Name, spinCmd)
	}
	// Record the agent's before→after change for the council. Gate on the tool's own success
	// (toolOK), NOT res.IsError — a write that landed but failed gofmt/a hook is exactly the
	// broken change the council should scrutinize, and must not read as a no-op turn.
	if guard != nil && changePath != "" && toolOK && fileModifiers[tc.Name] {
		rel := relForChange(workdir, changePath)
		after := readForChange(workdir, changePath)
		guard.recordChange(rel, changeBefore, after)
		// Self-regression check: warn (don't block) when this edit undoes the agent's own
		// earlier change by returning the file to a state it already held this turn. A revert is
		// not progress, so retract the counter reset mutated() just applied — otherwise an
		// implement↔revert oscillation dodges the stall force-stop by zeroing sinceProgress on
		// every swing. Retract only when THIS call's mutated() actually reset (block above gates
		// on res.IsError, this one on toolOK — they can diverge).
		if warn, regressed := guard.noteEdit(rel, changeBefore, after); warn != "" || regressed {
			if warn != "" {
				res.Content = appendToContent(res.Content, "\n\n[self-edit check] "+warn)
			}
			if regressed && mutatedReset {
				guard.retractProgress()
			}
		}
		// Tabu check: this edit may have returned the deliverable to a state whose test already
		// failed this turn (a proven-bad approach the agent is circling back to). Advisory only.
		if tabu := guard.checkTabu(); tabu != "" {
			res.Content = appendToContent(res.Content, "\n\n[tabu] "+tabu)
		}
	}

	a.appendPart(ctx, sid, actor, toolMsgID, session.RoleTool, session.Part{
		ID: "p_" + newID(), Kind: session.PartToolResult, ToolResult: &res,
	})
}
