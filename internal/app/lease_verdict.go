package app

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// leaseVerdict decides what happens when a subagent's lease expires: how much more time it gets,
// and the reason that goes in the record either way.
//
// It is one function because it is one question — is this child working right now — and the answer
// is a LADDER, in this order: the cheap deterministic tests that can prove it, then the LLM judge
// for the case none of them can. Read top to bottom, the arms are the kinds of silence a working
// child produces, each one learned from a run that killed a child in it:
//
//	tool in flight    — a foreground build emits nothing until it returns
//	generating        — a model mid-response emits nothing until the first token
//	deliberating      — a council/planner side-call streams nothing at all
//	produced          — it changed something inside this very window
//	background CPU    — a job it launched and stopped polling is burning CPU
//	…otherwise        — nothing here can tell, so ask the judge
//
// Every arm extends by the same fixed amount, so ordering costs nothing but reading clarity, and
// none of them can hold a runaway open: the backstop caps the attempt regardless, and the caller
// clamps the extension to what is left of it.
//
// prod carries the productivity count at the PREVIOUS tick and is returned updated, which is what
// makes that arm one extension per burst of work rather than an open lease.
func (a *App) leaseVerdict(ctx context.Context, parent, child session.Session, prompt string,
	backstopLeft, elapsed time.Duration, prod int64) (time.Duration, string, int64) {
	// A spent backstop skips the judge entirely: no verdict can add time that does not exist.
	if backstopLeft <= 0 {
		return 0, "backstop spent", prod
	}
	switch {
	case a.toolInFlight(child.ID):
		// A foreground build (`make`, a long test run) emits no events and is not a poll/wait verb,
		// so the judge — and the deterministic wait-check — read a mid-build child as wedged and
		// kill it exactly when it is legitimately busy. The tool's own timeout (≤10m) bounds the
		// call.
		return a.leaseExtension(), "tool in flight (active work, not churn)", prod

	case a.generating(child.ID) && a.genFresh(child.ID):
		// Mid-response: no tool in flight, and nothing to see until the first token. On a slow
		// model this is where most of a child's wall time goes, so the lease timer lands here often
		// and every test below it was false — observed as four subagents whose last recorded event
		// is the provider's own `context canceled`, one three seconds after a successful write, and
		// one step that burned six consecutive attempts that way. genFresh is what keeps a WEDGED
		// backend out of this arm: a stream that has produced nothing goes to the judge.
		return a.leaseExtension(), "generating (mid-response, not churn)", prod

	case a.deliberating(child.ID):
		// Inside its own council/planner gate: sequential side-LLM calls that stream nothing and
		// hold no tool, but ARE active work. The round cap and the backstop bound a stuck gate.
		return a.leaseExtension(), "deliberating (council/plan side-calls, not churn)", prod

	case leaseProgressEnabled() && a.productiveCount(child.ID) > prod:
		// It produced new work inside this window. Restarting cannot get that work done faster — it
		// discards it and re-derives it from nothing, which is what the observed run did ten times
		// across two steps while keeping one report out of ten. The window advances with the count,
		// so producing once and then spinning earns exactly one extension for it.
		return a.leaseExtension(), "produced new work since the last lease tick", a.productiveCount(child.ID)

	case subagentProcLeaseEnabled() && a.childProcActive(child.ID):
		// Off-tool background work: a bash{background:true} job (a long transfer/build the model
		// launched and stopped polling) is invisible to toolInFlight and to the wait-majority check,
		// so the judge reads it as churn and KILLs — the remote-download spin. Sampling real CPU is
		// robust to a worker wrapping its work in a self-written script; idle/wedged jobs (flat CPU)
		// fall through to the judge.
		return a.leaseExtension(), "background process actively using CPU (not churn)", prod

	default:
		ext, note := a.judgeLease(ctx, parent, child, prompt, elapsed)
		return ext, note, prod
	}
}
