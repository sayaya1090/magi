package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Elastic subagent attempt cap.
//
// The configured SubagentTimeout is a BASE calibrated for a nominal model: tight
// enough that a churning subagent (event-active, so the stall watchdog never
// fires) can't eat the parent's wall clock, generous enough that focused work
// finishes. But real model speed varies by an order of magnitude — a slow local
// model can spend minutes on a single legitimate generation, and a fixed cap
// would cut it mid-stream on every attempt while being needlessly loose for a
// fast cloud model. So the cap flexes with OBSERVED model speed: the effective
// cap budgets subagentCapRounds LLM round trips at the model's recent average
// latency, clamped to [base/2, base×subagentCapMaxFactor].
//
// attemptCap is the single decision point for how long an attempt may run.
// Today's policy is deterministic (no judgment); a future refinement — a lease
// that inspects the attempt for churn signals at expiry, or a model-judgment
// termination — plugs in HERE without touching the supervisor loop.
const (
	subagentCapRounds    = 6 // round trips a focused subagent attempt is budgeted for
	subagentCapMaxFactor = 3 // stretch ceiling: base × this
	llmLatWindow         = 8 // per-model latency samples kept (ring)
)

// llmLatencies records recent LLM round-trip durations per model ID.
type llmLatencies struct {
	mu      sync.Mutex
	byModel map[string][]time.Duration // ring, newest appended, capped at llmLatWindow
}

func (l *llmLatencies) record(model string, d time.Duration) {
	if model == "" || d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byModel == nil {
		l.byModel = map[string][]time.Duration{}
	}
	r := append(l.byModel[model], d)
	if len(r) > llmLatWindow {
		r = r[len(r)-llmLatWindow:]
	}
	l.byModel[model] = r
}

// avg returns the model's mean recent round trip, falling back to the mean
// across ALL models (a routed child model may not have samples yet, but the
// gateway's overall speed is still a better prior than nothing). 0 = no data.
func (l *llmLatencies) avg(model string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.byModel[model]; len(r) > 0 {
		return meanDur(r)
	}
	var all []time.Duration
	for _, r := range l.byModel {
		all = append(all, r...)
	}
	if len(all) == 0 {
		return 0
	}
	return meanDur(all)
}

func meanDur(r []time.Duration) time.Duration {
	var sum time.Duration
	for _, d := range r {
		sum += d
	}
	return sum / time.Duration(len(r))
}

// attemptCap returns the effective hard cap for one subagent attempt running
// the given model. See the file comment: base when unobserved, otherwise
// subagentCapRounds×avg clamped to [base/2, base×subagentCapMaxFactor].
func (a *App) attemptCap(model string) time.Duration {
	a.mu.Lock() // SetSubagentTimeout mutates the base at runtime
	base := a.cfg.SubagentTimeout
	a.mu.Unlock()
	avg := a.llmLat.avg(model)
	if avg == 0 {
		return base
	}
	dyn := time.Duration(subagentCapRounds) * avg
	if lo := base / 2; dyn < lo {
		return lo
	}
	if hi := time.Duration(subagentCapMaxFactor) * base; dyn > hi {
		return hi
	}
	return dyn
}

// SubagentTimeoutInfo reports the configured base and the current effective cap
// for the session's model (the /subagent command's view).
func (a *App) SubagentTimeoutInfo(ctx context.Context, sid session.SessionID) string {
	model := a.sessionInfo(ctx, sid).Model.Model
	a.mu.Lock()
	base := a.cfg.SubagentTimeout
	a.mu.Unlock()
	eff := a.attemptCap(model)
	avg := a.llmLat.avg(model)
	if avg == 0 {
		return fmt.Sprintf("subagent timeout: base %s (no model-speed samples yet — elastic cap inactive, range %s–%s)",
			base, base/2, time.Duration(subagentCapMaxFactor)*base)
	}
	return fmt.Sprintf("subagent timeout: base %s, effective %s (%d× recent avg LLM round trip %s, clamped to %s–%s)",
		base, eff, subagentCapRounds, avg.Round(time.Millisecond), base/2, time.Duration(subagentCapMaxFactor)*base)
}

// SetSubagentTimeout updates the base cap at runtime (the /subagent command).
// The elastic range follows the new base immediately; in-flight attempts keep
// the cap they started with.
func (a *App) SetSubagentTimeout(d time.Duration) error {
	if d < 30*time.Second {
		return fmt.Errorf("subagent timeout %s too small (min 30s)", d)
	}
	a.mu.Lock()
	a.cfg.SubagentTimeout = d
	a.mu.Unlock()
	return nil
}
