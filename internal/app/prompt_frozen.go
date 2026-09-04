package app

import (
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The two frozen pieces of every request's PREFIX, and the only doors to them.
//
// A prompt cache matches from the first byte, so whatever sits ahead of the conversation — the
// system prompt, the tool catalog — multiplies everything behind it: change one byte there and the
// whole transcript is re-billed. That rule has now been rediscovered here three times, each time
// from a bill (the repeat-collapse rewrite, the "small" volatile block, the skill list re-rendered
// at position 0). Each fix pinned its own seam; this file pins the DOORS.
//
// The discipline is the one the rest of the codebase already converged on:
//
//   - what rides in the prefix is FROZEN — per turn for the system prompt (the language lock may
//     legitimately move between turns), per session for the tool catalog (a tool cannot be called
//     from an announcement; it must be in the request's tools array, so mid-session arrivals wait
//     for the next session)
//   - what changes rides behind the frozen part, appended, or in the volatile tail
//
// buildStepRequest and everything downstream take these accessors, never the raw builders. The
// raw builders stay unexported and are called exactly once per freeze window; a new call site
// reaching for them directly is caught by TestPrefixBuildersHaveOneDoor, which fails the build
// the way the arch ratchets do — the property held by a test that names the door, not by
// convention.

// stepSystemFor returns the session's system prompt for the CURRENT TURN, building it on first
// use and returning the same bytes for every step after that. resetTurnPrompt opens the next
// window at turn start.
func (a *App) stepSystemFor(sid session.SessionID, agent AgentSpec, workdir string, evs []event.Event) string {
	a.mu.Lock()
	st := a.stateLocked(sid)
	if st.turnSysSet {
		sys := st.turnSys
		a.mu.Unlock()
		return sys
	}
	a.mu.Unlock()
	sys := a.buildStepSystem(sid, agent, workdir, evs)
	a.mu.Lock()
	st = a.stateLocked(sid)
	// First writer wins: two racing steps of one turn must agree, and the second build would be
	// byte-identical anyway (that was the old contract); keeping the first makes it structural.
	if !st.turnSysSet {
		st.turnSys, st.turnSysSet = sys, true
	}
	sys = st.turnSys
	a.mu.Unlock()
	return sys
}

// resetTurnPrompt closes the previous turn's freeze window. Called at turn start — the one
// sanctioned moment the head may change, because a new turn begins with a new user message and
// the prefix beyond it is not yet written.
func (a *App) resetTurnPrompt(sid session.SessionID) {
	a.mu.Lock()
	a.stateLocked(sid).turnSysSet = false
	a.stateLocked(sid).turnSys = ""
	a.mu.Unlock()
}

// sessionToolSpecs returns the tool catalog advertised to THIS AGENT in this session, frozen at
// its first request and held for the session's whole life. Keyed by the agent because a workflow
// runs several agents through one session, each with its own allowlist and its own prefix.
//
// Per session and not per turn, deliberately: the catalog is the one prefix piece a mid-run change
// cannot be announced around — a tool absent from the request's tools array cannot be called no
// matter what the transcript says about it — and the survey of what every other harness does
// lands on the same rule ("fixed tool catalog per session"). A plugin that hot-reloads a new tool
// mid-session reaches the NEXT session; this one keeps its catalog and its cache.
func (a *App) sessionToolSpecs(sid session.SessionID, agent AgentSpec) []port.ToolSpec {
	key := agent.Name + "\x00" + strings.Join(agent.Tools, ",")
	a.mu.Lock()
	st := a.stateLocked(sid)
	if specs, ok := st.toolsFrozen[key]; ok {
		a.mu.Unlock()
		return specs
	}
	a.mu.Unlock()
	specs := a.toolSpecs(sid, agent)
	a.mu.Lock()
	st = a.stateLocked(sid)
	if st.toolsFrozen == nil {
		st.toolsFrozen = map[string][]port.ToolSpec{}
	}
	if have, ok := st.toolsFrozen[key]; ok {
		specs = have
	} else {
		st.toolsFrozen[key] = specs
	}
	a.mu.Unlock()
	return specs
}

// notePromptShape records what the request just assembled was made of, so the turn's finish can
// carry it. Measured here because here is the only place that holds all five pieces at once: the
// system prompt and the tool catalog exist only in this process's memory, and a reader replaying
// the log can measure the conversation and nothing else.
func (a *App) notePromptShape(sid session.SessionID, model, sys string, msgs []session.Message, specs []port.ToolSpec) {
	sh := event.PromptShape{Window: a.contextWindow(model), System: len(sys) / 4, Tools: toolSpecTokens(specs)}
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartText {
				sh.Talk += len(p.Text)
			}
			if p.ToolCall != nil {
				sh.Calls += len(p.ToolCall.Name) + len(p.ToolCall.Args)
			}
			if p.ToolResult != nil {
				sh.Results += len(p.ToolResult.Content)
			}
		}
	}
	// Each kind is summed in characters first and divided once, so each number is that kind's own
	// best estimate. The five will not add up to estimateTokens exactly — five roundings instead of
	// one, at most three characters lost per kind — and that is the right trade: the screen labels
	// each piece, so each piece has to be right about itself. The total beside them comes from the
	// provider's own count anyway, which these were never going to match.
	sh.Talk, sh.Calls, sh.Results = sh.Talk/4, sh.Calls/4, sh.Results/4
	a.mu.Lock()
	a.stateLocked(sid).shape = sh
	a.mu.Unlock()
}

// promptShape answers the last recorded make-up, and whether there is one. A session that has not
// assembled a request has no shape, and a zeroed one would read as "this companion is holding
// nothing" — which is never true of a request that was actually sent.
func (a *App) promptShape(sid session.SessionID) (event.PromptShape, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sh := a.stateLocked(sid).shape
	return sh, sh != event.PromptShape{}
}

// shapeOf is what a turn.finished carries, or nil when this process assembled no request for the
// session — a turn that failed before its first call has no shape, and writing a zeroed one would
// tell every screen the companion is holding nothing.
func shapeOf(a *App, sid session.SessionID) *event.PromptShape {
	sh, ok := a.promptShape(sid)
	if !ok {
		return nil
	}
	return &sh
}
