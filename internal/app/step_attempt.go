package app

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A step's retry ladder outlives the dispatch that started it.
//
// spawnResolved retries a failing subagent SubagentMaxRestarts times and then returns the failure.
// The caller — a step gate that still needs the step done — dispatches the same step again, and
// that second dispatch used to begin from nothing: attempt 0, no previous result, so the brief
// carried no failure reason and no tool trail, and the "Retry N" counter restarted at 1.
//
// Everything the exhausted attempts had established was therefore discarded at exactly the boundary
// where it was the only thing left. Observed live: ten workers across two plan steps, the ladder
// running R0→R1→R2 and then R0 with an empty trail again, twenty-eight minutes spent, and one
// report surviving out of the ten attempts.
//
// So the ladder is keyed to the STEP. A continuation starts with the last exhausted attempt's
// result in hand: the same trail digest the within-dispatch retries would have carried, and a retry
// number that tells the truth about how many attempts this step has cost. The entry is dropped as
// soon as any attempt succeeds — a step that gets an answer starts clean the next time it is run.

// workerPartHeader is the line plan_prompts.go writes above a worker's own task, and the anchor
// this key reads: what follows it is the plan step's text verbatim.
const workerPartHeader = "YOUR PART — do EXACTLY this one part of a larger plan, nothing more:\n"

// stepAttemptKey identifies the plan step a spawn is for, stably across re-dispatches.
//
// The task line under the worker's own header is the plan step's text verbatim, so it survives the
// curator rewriting everything around it — which the whole prompt does not. When that header is
// absent (an explorer, a mining pass, a recovery unit) the whole prompt is the identity: those are
// re-issued verbatim or not at all, so a prompt that differs is a different piece of work and
// correctly gets no carry-over.
func stepAttemptKey(parent session.SessionID, agent string, req port.SpawnRequest) string {
	id := req.Prompt
	if _, rest, ok := strings.Cut(req.Prompt, workerPartHeader); ok {
		if line := strings.TrimSpace(headLine(rest)); line != "" {
			id = line
		}
	}
	sum := sha1.Sum([]byte(id))
	return string(parent) + "\x00" + agent + "\x00" + hex.EncodeToString(sum[:8])
}

// headLine returns s up to its first newline.
func headLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// stepAttempt is what a spent ladder leaves for its continuation.
type stepAttempt struct {
	last port.SpawnResult
	n    int // attempts this step has already cost
}

// priorStepAttempt returns the exhausted ladder for this step, or a zero result and 0.
func (a *App) priorStepAttempt(key string) (port.SpawnResult, int) {
	if v, ok := a.stepAttempts.Load(key); ok {
		if sa, ok := v.(stepAttempt); ok {
			return sa.last, sa.n
		}
	}
	return port.SpawnResult{}, 0
}

// rememberStepAttempt records a spent ladder so the next dispatch of the same step continues it.
func (a *App) rememberStepAttempt(key string, last port.SpawnResult, n int) {
	a.stepAttempts.Store(key, stepAttempt{last: last, n: n})
}

// forgetStepAttempt drops a step's ladder once an attempt has succeeded.
func (a *App) forgetStepAttempt(key string) { a.stepAttempts.Delete(key) }

// forgetStepAttemptsFor drops every ladder belonging to one parent session, so a new top-level turn
// does not inherit the failures of the previous one.
func (a *App) forgetStepAttemptsFor(parent session.SessionID) {
	prefix := string(parent) + "\x00"
	a.stepAttempts.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok && strings.HasPrefix(s, prefix) {
			a.stepAttempts.Delete(s)
		}
		return true
	})
}

// stepLadderSpent reports whether a whole retry ladder has already been spent on this exact step,
// and how many attempts that came to.
//
// One ladder is the experiment: an attempt fails, and the retry gets the failure reason and the
// previous attempt's tool trail so it can take a different route. When that is exhausted, starting
// a second ladder runs the same experiment again at full price — observed as one plan step handed
// to six workers over twenty-eight minutes, none of which reported.
//
// A re-planned step whose task text differs keys to a fresh ladder and dispatches normally, so this
// only bites when the same part is re-emitted verbatim. MAGI_STEP_LADDER_CAP=0 restores the
// unbounded re-dispatch for A/B.
func (a *App) stepLadderSpent(parent session.SessionID, agent string, req port.SpawnRequest) (int, bool) {
	if envOff("MAGI_STEP_LADDER_CAP") {
		return 0, false
	}
	_, n := a.priorStepAttempt(stepAttemptKey(parent, agent, req))
	return n, n >= a.cfg.SubagentMaxRestarts+1
}
