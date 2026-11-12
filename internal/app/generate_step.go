package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// stepResponse is one model response for one loop step, with the ids its parts were streamed
// under and the clock the latency meter reads. evs comes back because a context-overflow recovery
// re-reads the session after compacting it.
type stepResponse struct {
	res                             streamStep
	msgID, textPartID, reasonPartID string
	start                           time.Time
	evs                             []event.Event
}

// generateStep produces one model response for a step, together with the two recoveries that
// belong to getting one rather than to acting on it: a request too large for the backend, which is
// answered by folding older history into a summary and re-issuing, and a backend that accepts the
// request and then says nothing, which is answered by re-issuing the same request.
//
// It was inline in runLoop, which meant the step loop's body opened with sixty lines about HTTP
// before reaching anything about the agent. Both recoveries loop, both are bounded, and neither has
// anything to say to the rest of the step — so they are one function, and runLoop reads as: get a
// response, record it, run its tools, decide whether the turn is over.
func (a *App) generateStep(ctx context.Context, tc turnCtx, agent AgentSpec, agentActor event.Actor,
	evs []event.Event, step, cumOut int) (stepResponse, error) {
	sid := tc.s.ID
	req, evs := a.buildStepRequest(ctx, tc, evs, step, cumOut)

	var stepStart time.Time // set per attempt below, so a stalled retry does not bias the meter
	var res streamStep
	var msgID, textPartID, reasonPartID string
	ctxCompactRetries := 0
	// compactForRetry folds older history into a summary and rebuilds the request, for a backend
	// that refused this one for size. Bounded + charged so a request that stays too large after
	// every fold still surfaces the error.
	compactForRetry := func(cause error) bool {
		if !ctxCompactRetryEnabled() || ctxCompactRetries >= maxCtxCompactRetries {
			return false
		}
		fresh, rerr := a.store.Read(ctx, sid, 0)
		if rerr != nil {
			return false
		}
		// The refusal often names the real request size; the fold below sizes its kept tail by the
		// bytes/4 estimate, which on Korean/JSON-heavy history ran at about half the truth (measured:
		// ~487k estimated against a ~1,035k request). Learn the ratio first so the fold keeps what the
		// wire can carry; without it every retry folded too little and the ladder ran out.
		if real := overflowRequestTokens(cause); real > 0 {
			a.learnEstimateScale(sid, real, estimateTokens("", reconstruct(fresh)))
		}
		if !a.compactNow(ctx, tc.s, agent, agentActor, fresh) {
			return false
		}
		// The FOLDED log, and its error is not decoration. Read answers (nil, err) when the log
		// cannot be read, and this dropped it — so an unreadable log rebuilt the request from no
		// conversation at all and re-issued THAT. Measured end to end: the retry went out with zero
		// messages, the backend answered it, and the turn recorded turn.finished with no error. The
		// recovery for "the request was too big" was to ask the model nothing and call the reply an
		// answer. Unreadable here means the recovery cannot happen, so the overflow that started it
		// is still the news: the caller falls through to its error path.
		folded, ferr := a.store.Read(ctx, sid, 0)
		if ferr != nil {
			return false
		}
		ctxCompactRetries++
		a.emitToolProgress(sid, agentActor, "", agent.Name, "context too large for the model — compacting and retrying")
		req, evs = a.buildStepRequest(ctx, tc, folded, step, cumOut)
		return true
	}
	// waitTransient answers a busy backend (see transient.go): announce the wait, then sleep it.
	// false when the ladder is spent or the turn was cancelled during the wait.
	// The ladder is bounded twice: by count (maxTransientRetries) and by cumulative wait
	// (transientWaitBudget). transientStop says why it stopped short, for the record.
	transientRetries := 0
	var transientWaited time.Duration
	var transientStop string
	waitTransient := func(cause error) bool {
		if ctx.Err() != nil {
			return false
		}
		if isGateHeld(cause) {
			// The shared gate held this request back; nothing reached the backend. Sit out the wait
			// it named against the wait budget, without spending an attempt.
			wait, left := classifyBackend(cause).RetryAfter, transientWaitBudget-transientWaited
			if left <= 0 || wait > left {
				transientStop = fmt.Sprintf("the backend is cooling down for %s more, longer than the %s left of this step's retry budget",
					wait.Round(time.Second), left.Round(time.Second))
				return false
			}
			transientWaited += wait
			a.emitToolProgress(sid, agentActor, "", agent.Name, fmt.Sprintf("backend cooling down \u2014 waiting %s before asking again (stop the turn to cancel)",
				wait.Round(100*time.Millisecond)))
			return sleepCtx(ctx, wait)
		}
		if transientRetries >= maxTransientRetries {
			return false
		}
		cls := classifyBackend(cause)
		left := transientWaitBudget - transientWaited
		if cls.RetryAfter > left {
			// Never ask earlier than the backend said: stop resumably instead.
			transientStop = fmt.Sprintf("the backend asked to wait %s, longer than the %s left of this step's retry budget",
				cls.RetryAfter.Round(time.Second), left.Round(time.Second))
			return false
		}
		if left <= 0 {
			transientStop = fmt.Sprintf("the %s retry budget is spent", transientWaitBudget.Round(time.Second))
			return false
		}
		d := transientDelay(transientRetries, cls.RetryAfter)
		if d > left {
			d = left
		}
		transientRetries++
		transientWaited += d
		a.emitToolProgress(sid, agentActor, "", agent.Name, fmt.Sprintf("backend %s (%s) — retrying in %s (attempt %d of %d; stop the turn to cancel)",
			cls.Kind, shortCause(cause), d.Round(100*time.Millisecond), transientRetries, maxTransientRetries))
		return sleepCtx(ctx, d)
	}
	for sAttempt := 0; ; sAttempt++ {
		// A cancelable child ctx so the reasoning-spin and stall guards can stop a response
		// mid-stream (see consumeStream). Cancelling streamCtx (not ctx) leaves the turn's context
		// intact. stepStart is reset each attempt so a stalled retry doesn't bias the latency meter.
		stepStart = time.Now()
		streamCtx, streamCancel := context.WithCancel(ctx)
		// Mark the generation in flight for the whole call, error paths included: the lease
		// supervisor reads this to know the child is working through a silence that emits no
		// events (see enterGen).
		a.enterGen(sid)
		// A run with nothing to run on. An App can legitimately be built without a provider — the
		// console builds one as a READER, to derive rows from logs — and until now anything that
		// started a turn on such an App dereferenced nil here and took the process down. A turn
		// that cannot be run is an error to report, not a crash: the same shape as a backend that
		// refuses, which every caller of this already knows how to handle.
		prov := a.providerFor(agent)
		if prov == nil {
			a.leaveGen(sid)
			streamCancel()
			return stepResponse{}, fmt.Errorf("this magi has no model backend configured, so there is nothing to run this turn on")
		}
		stream, err := prov.StreamChat(streamCtx, req)
		if err != nil {
			a.leaveGen(sid)
			streamCancel()
			// Context-overflow is recoverable: the backend rejected the request purely for
			// size, so fold older history into a summary and re-issue rather than ending the
			// turn. This is recovery (the run continues), not a cosmetic landing — the error is
			// caused by size and compaction directly removes size. Bounded + charged so a
			// request that stays too large after every fold still surfaces the error.
			if isContextOverflow(err) && compactForRetry(err) {
				continue
			}
			if isTransientBackend(err) {
				if waitTransient(err) {
					continue
				}
				if ctx.Err() != nil {
					return stepResponse{}, ctx.Err()
				}
			}
			return stepResponse{}, a.stopOnBackend(ctx, sid, agentActor, err, transientRetries, transientWaited, transientStop)
		}
		msgID = "m_" + newID()
		textPartID = "p_" + newID()
		reasonPartID = "p_" + newID()
		var serr error
		res, serr = a.consumeStream(streamCtx, sid, agentActor, stream, msgID, streamCancel)
		a.leaveGen(sid)
		streamCancel()
		if serr != nil {
			return stepResponse{}, serr
		}
		// Every answered request is a measurement: the backend counted what we sent. Compare it with
		// our own count of the SAME request, so the next trigger and fold size in real tokens before
		// the context overflows — not only after a refusal.
		if res.usage != nil && res.usage.In > 0 {
			a.observeEstimateScale(sid, res.usage.In, estimateChatRequest(req))
		}
		// The same size refusal, delivered inside a stream that had produced nothing yet (a CLI
		// shim answers 200 first). Same recovery; recorded only if it cannot help.
		if res.overflow != nil {
			if compactForRetry(res.overflow) {
				continue
			}
			a.emitError(ctx, sid, agentActor, res.overflow.Error())
			return stepResponse{}, res.overflow
		}
		// The same busy-backend refusal, delivered inside the stream before any output.
		if res.transient != nil {
			if waitTransient(res.transient) {
				continue
			}
			if ctx.Err() != nil {
				return stepResponse{}, ctx.Err()
			}
			return stepResponse{}, a.stopOnBackend(ctx, sid, agentActor, res.transient, transientRetries, transientWaited, transientStop)
		}
		// Stall-retry: the stream produced NO output for firstTokenBound() (a hung/silent backend).
		// Re-issue the SAME request rather than strand the turn until the wall clock — a transient
		// stall recovers on retry, and nothing was committed (the stall fires only before the first
		// token), so re-generating is safe. maxStreamStallRetries is the only bound on this ladder:
		// there is no step budget to charge (the step cap came out with the delegation machinery),
		// so a permanently silent backend surfaces as the error below after the retries, not sooner.
		if res.stalled && sAttempt < maxStreamStallRetries && ctx.Err() == nil {
			a.emitToolProgress(sid, agentActor, "", agent.Name, fmt.Sprintf("no response for %s — retrying the request", firstTokenBound()))
			continue
		}
		if res.stalled {
			// A backend silent on every try is a transient failure like a 503: it ends in the common
			// shape (kind timeout, resumable, how to carry on), not as a bare error with no way forward.
			serr := &port.BackendError{Kind: port.KindTimeout, Cause: fmt.Errorf("llm: no response after %d stalled attempt(s) (backend silent ~%s each)", sAttempt+1, firstTokenBound())}
			return stepResponse{}, a.stopOnBackend(ctx, sid, agentActor, serr, transientRetries, transientWaited,
				"the backend stayed silent before its first token on every attempt")
		}
		break
	}
	return stepResponse{res: res, msgID: msgID, textPartID: textPartID, reasonPartID: reasonPartID,
		start: stepStart, evs: evs}, nil
}
