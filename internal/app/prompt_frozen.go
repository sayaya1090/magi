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
	specs := a.toolSpecs(agent)
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
