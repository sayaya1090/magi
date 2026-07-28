package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
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
	guard *runGuard, evs []event.Event, step, cumOut int) (stepResponse, error) {
	sid := tc.s.ID
	req, evs := a.buildStepRequest(ctx, tc, evs, step, cumOut)

	stepStart := time.Now()
	var res streamStep
	var msgID, textPartID, reasonPartID string
	ctxCompactRetries := 0
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
		stream, err := a.providerFor(agent).StreamChat(streamCtx, req)
		if err != nil {
			a.leaveGen(sid)
			streamCancel()
			// Context-overflow is recoverable: the backend rejected the request purely for
			// size, so fold older history into a summary and re-issue rather than ending the
			// turn. This is recovery (the run continues), not a cosmetic landing — the error is
			// caused by size and compaction directly removes size. Bounded + charged so a
			// request that stays too large after every fold still surfaces the error.
			if ctxCompactRetryEnabled() && isContextOverflow(err) && ctxCompactRetries < maxCtxCompactRetries {
				if fresh, rerr := a.store.Read(ctx, sid, 0); rerr == nil && a.compactNow(ctx, tc.s, agent, agentActor, fresh) {
					ctxCompactRetries++
					a.emitToolProgress(sid, agentActor, "", agent.Name, "context too large for the model — compacting and retrying")
					evs, _ = a.store.Read(ctx, sid, 0)
					req, evs = a.buildStepRequest(ctx, tc, evs, step, cumOut)
					continue
				}
			}
			a.emitError(ctx, sid, agentActor, err.Error())
			return stepResponse{}, err
		}
		msgID = "m_" + newID()
		textPartID = "p_" + newID()
		reasonPartID = "p_" + newID()
		var serr error
		res, serr = a.consumeStream(streamCtx, sid, agentActor, stream, msgID, textPartID, reasonPartID, streamCancel)
		a.leaveGen(sid)
		streamCancel()
		if serr != nil {
			return stepResponse{}, serr
		}
		// Stall-retry: the stream produced NO output for streamStallTimeout (a hung/silent backend).
		// Re-issue the SAME request rather than strand the turn until the wall clock — a transient
		// stall recovers on retry, and nothing was committed (the guard fires only before the first
		// token), so re-generating is safe. noteStep charges the wasted attempt so a permanently
		// silent backend still trips the step budget instead of looping.
		if res.stalled && sAttempt < maxStreamStallRetries && ctx.Err() == nil {
			a.emitToolProgress(sid, agentActor, "", agent.Name, fmt.Sprintf("no response for %s — retrying the request", streamStallTimeout))
			continue
		}
		if res.stalled {
			serr := fmt.Errorf("llm: no response after %d stalled attempt(s) (backend silent ~%s each)", sAttempt+1, streamStallTimeout)
			a.emitError(ctx, sid, agentActor, serr.Error())
			return stepResponse{}, serr
		}
		break
	}
	return stepResponse{res: res, msgID: msgID, textPartID: textPartID, reasonPartID: reasonPartID,
		start: stepStart, evs: evs}, nil
}
