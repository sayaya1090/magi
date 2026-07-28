package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The panel used to show what magi INTENDED: the acceptance criteria a council wrote before the
// work, and the per-step checks it authored to prove them. Both are gone, and what belongs in that
// space is the other thing magi has — its own record of what happened. A check written in advance
// can be wrong about the work; a record of the calls magi granted cannot be wrong about what it
// granted. So the panel shows facts and leaves the reading to whoever is watching.

// Observation is what magi's record says about a run, split for display: files its tools wrote,
// commands that exercised something and ended clean, ones that ended in a failing status, and ones
// whose status it could not read (a masked exit, or none at all).
type Observation struct {
	Changed  []string
	RanClean []string
	Failed   []string
	Unknown  []string
}

// Empty reports that there is nothing to show — no writes and no commands.
func (o Observation) Empty() bool {
	return len(o.Changed) == 0 && len(o.RanClean) == 0 && len(o.Failed) == 0 && len(o.Unknown) == 0
}

// observationTTL bounds how stale the panel's copy can be. The underlying read walks the session
// log, which a per-frame render must not do; a second is under the eye's threshold for a status
// panel and turns a redraw storm into one read.
const observationTTL = time.Second

// observationEntry is one session's memoized view and when it was taken. It lives on the session's
// own state, not in a package map: a map keyed by session id and never swept grows for the life of
// the process, and a second App in the same process would silently share it.
type observationEntry struct {
	obs Observation
	at  time.Time
}

// Observation returns the display form of magi's record for a session, memoized for observationTTL
// so a redrawing panel does not re-read the log on every frame.
func (a *App) Observation(ctx context.Context, sid session.SessionID) Observation {
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok && st.observed != nil && time.Since(st.observed.at) < observationTTL {
		obs := st.observed.obs
		a.mu.Unlock()
		return obs
	}
	a.mu.Unlock()

	o := a.observe(ctx, sid)
	var out Observation
	out.Changed = append(out.Changed, o.changed...)
	for _, c := range o.cmds {
		switch {
		case c.unclear:
			out.Unknown = append(out.Unknown, c.cmd)
		case !c.exec:
			// Inspection only — it printed state, it did not exercise anything. Counting `ls` as a
			// run is the churn this exists to see through, so it is not listed as one.
		case c.exit == 0:
			out.RanClean = append(out.RanClean, c.cmd)
		default:
			out.Failed = append(out.Failed, fmt.Sprintf("%s (exit %d)", c.cmd, c.exit))
		}
	}
	a.mu.Lock()
	a.stateLocked(sid).observed = &observationEntry{obs: out, at: time.Now()}
	a.mu.Unlock()
	return out
}
