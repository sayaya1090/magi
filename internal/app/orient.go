package app

import (
	"context"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// maybeOrient runs the explore-first grounding pass: once per session, it lands the workspace's
// deterministic layout and setup files (repoContext) into the MAIN agent's context as a factual
// system message, BEFORE the planner preflight, and directs the agent to check the task's real
// prerequisites (tools, deps, files, services, access) and provision what is missing — rather than
// presuming a build/test step, since not every task builds code. The planner reads the session
// window (runPlanner → plannerWindow), so this single injection grounds both the executor and the
// plan in the real environment instead of the instruction prose alone.
//
// Gated by orientEnabled and, at the call site, to the first cold, write-capable top-level turn.
// It is deterministic and hard-bounded (repoContext's own entry/byte caps), and injects FACTS —
// the files that actually exist — not speculative instructions, so a clean run is not misdirected.
func (a *App) maybeOrient(ctx context.Context, s session.Session) {
	// Fire exactly once per session: claim the grounded flag under the lock so a re-entrant or
	// concurrent call (another planEligible seam) can't inject twice.
	a.mu.Lock()
	st := a.stateLocked(s.ID)
	if st.grounded {
		a.mu.Unlock()
		return
	}
	st.grounded = true
	a.mu.Unlock()

	// A single short clause is handled by the main agent in one shot; grounding it wastes context
	// budget on files the trivial task won't touch. Mirrors maybePlanPreflight's triviality skip.
	if isTrivialPrompt(a.lastUserPrompt(ctx, s.ID)) {
		return
	}

	repo := strings.TrimSpace(repoContext(s.Workdir))
	if repo == "" || repo == "(empty)" || repo == "(unavailable)" {
		return // nothing worth grounding on (empty/unreadable workdir)
	}

	text := "# Working environment (oriented once, before you start)\n\n" + repo +
		"\n\n---\nThis is the workspace as it exists now. Before starting, work out what THIS task actually " +
		"requires — the tools, dependencies, files, services, or access it needs — and check each is present; " +
		"set up or create what is missing instead of assuming it, and build on what is already here rather than " +
		"adding parallel structures. Not every task is a build task, so ground in the task's real prerequisites, " +
		"not a presumed compile/test step."
	_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "orient"}, text)
}
