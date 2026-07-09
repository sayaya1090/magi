package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Fork creates a new, independent session that shares sid's history up to upToSeq
// (0 = the whole history), then can diverge — the basis for A/B branching of a
// loop (loop-engineering pain #4). The original is untouched; continuing the fork
// does not affect it. Returns the new session id.
func (a *App) Fork(ctx context.Context, sid session.SessionID, upToSeq int64) (session.SessionID, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	var batch []event.Event
	for _, e := range evs {
		if upToSeq > 0 && e.Seq > upToSeq {
			break
		}
		batch = append(batch, e) // Append re-assigns seq and the session id for newSid
	}
	// The first event must be session.created (the store derives the workdir from
	// it for a fresh session). Read returns it as event 1 of any real session.
	if len(batch) == 0 || batch[0].Type != event.TypeSessionCreated {
		return "", fmt.Errorf("nothing to fork from %s", sid)
	}
	newSid := session.SessionID("s_" + newID())
	if _, err := a.store.Append(ctx, newSid, batch...); err != nil {
		return "", err
	}
	// Register in memory so the fork is immediately usable (mirror the origin's
	// session fields under the new id).
	s := a.sessionInfo(ctx, sid)
	s.ID = newSid
	s.Created = time.Now()
	// Copy Meta so the fork doesn't share the origin's map (future per-session
	// writes mustn't cross-contaminate).
	if s.Meta != nil {
		m := make(map[string]string, len(s.Meta))
		for k, v := range s.Meta {
			m[k] = v
		}
		s.Meta = m
	}
	a.mu.Lock()
	a.stateLocked(newSid).meta = s
	a.mu.Unlock()
	return newSid, nil
}

// Replay branches the session just before its last user turn and returns the new
// fork plus that turn's prompt text, so the caller can re-run the SAME input on
// the branch — re-running a trajectory to compare against the original (pain #5),
// e.g. after switching model/config or to probe non-determinism. The original is
// untouched. Returns an empty prompt when there is no user turn to replay.
func (a *App) Replay(ctx context.Context, sid session.SessionID) (session.SessionID, string, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", "", err
	}
	// Find the last turn-STARTING user prompt. A mid-run steer is also an
	// ActorUser prompt.submitted, so we only take the first user prompt after a
	// boundary (session start, turn.finished, or a turn-ending error) and skip the
	// rest of that turn's steers — otherwise we'd fork inside an unfinished turn.
	var lastSeq int64
	var prompt string
	expectStart := true // the first user prompt (after session.created) starts a turn
	for _, e := range evs {
		switch {
		case e.Type == event.TypeTurnFinished || e.Type == event.TypeError:
			expectStart = true
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			if expectStart {
				var d event.PromptSubmittedData
				if json.Unmarshal(e.Data, &d) == nil {
					lastSeq = e.Seq
					prompt = partsText(d.Parts)
				}
				expectStart = false // later user prompts this turn are steers — skip
			}
		}
	}
	if lastSeq == 0 || strings.TrimSpace(prompt) == "" {
		return "", "", nil // nothing to replay
	}
	// Fork at the point just before the last user turn so re-submitting reruns it.
	fork, err := a.Fork(ctx, sid, lastSeq-1)
	if err != nil {
		return "", "", err
	}
	return fork, prompt, nil
}

// sessionStats are the aggregate shape of a session's trajectory, for diffing.
type sessionStats struct {
	turns, steps, tools, errs, council int
	tokensIn, tokensOut                int
	final                              string
}

// summarizeSession totals a session's per-turn shape. `final` is a coarse
// heuristic (a turn with usage but no continuing council reads as "done", even if
// it errored) — fine as a symmetric A/B summary, not an authoritative outcome.
func summarizeSession(evs []event.Event) sessionStats {
	turns := scanTurns(evs)
	var s sessionStats
	s.turns = len(turns)
	for _, t := range turns {
		s.steps += t.steps
		s.tools += t.tools
		s.errs += t.errs
		s.council += len(t.council)
		if t.usage != nil {
			s.tokensIn += t.usage.In
			s.tokensOut += t.usage.Out
		}
	}
	if n := len(turns); n > 0 {
		last := turns[n-1]
		switch {
		case len(last.council) > 0 && strings.Contains(last.council[len(last.council)-1], "done"):
			s.final = "done"
		case len(last.council) > 0:
			s.final = "continue"
		case last.usage != nil:
			s.final = "done"
		default:
			s.final = "—"
		}
	}
	return s
}

// SessionDiff renders a structural comparison of two sessions' trajectories — how
// each loop unfolded (steps, tools, council rounds, tokens) — so a forked A/B can
// be compared (pain #5).
func (a *App) SessionDiff(ctx context.Context, aSid, bSid session.SessionID) (string, error) {
	aEvs, err := a.store.Read(ctx, aSid, 0)
	if err != nil {
		return "", err
	}
	bEvs, err := a.store.Read(ctx, bSid, 0)
	if err != nil {
		return "", err
	}
	return diffSessions(aSid, aEvs, bSid, bEvs), nil
}

// diffSessions is the pure rendering (events → comparison text).
func diffSessions(aID session.SessionID, aEvs []event.Event, bID session.SessionID, bEvs []event.Event) string {
	sa, sb := summarizeSession(aEvs), summarizeSession(bEvs)
	var b strings.Builder
	fmt.Fprintf(&b, "Session diff\n%-16s %-14s %-14s\n", "", "A "+short(aID), "B "+short(bID))
	row := func(label string, av, bv string) {
		mark := "  "
		if av != bv {
			mark = "≠ " // flag a difference
		}
		fmt.Fprintf(&b, "%s%-14s %-14s %-14s\n", mark, label, av, bv)
	}
	row("turns", strconv.Itoa(sa.turns), strconv.Itoa(sb.turns))
	row("steps", strconv.Itoa(sa.steps), strconv.Itoa(sb.steps))
	row("tool calls", strconv.Itoa(sa.tools), strconv.Itoa(sb.tools))
	row("errors", strconv.Itoa(sa.errs), strconv.Itoa(sb.errs))
	row("council", strconv.Itoa(sa.council), strconv.Itoa(sb.council))
	row("final", orDash(sa.final), orDash(sb.final))
	row("tokens i/o", fmt.Sprintf("%d/%d", sa.tokensIn, sa.tokensOut), fmt.Sprintf("%d/%d", sb.tokensIn, sb.tokensOut))
	return strings.TrimRight(b.String(), "\n")
}

func short(id session.SessionID) string {
	s := string(id)
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
