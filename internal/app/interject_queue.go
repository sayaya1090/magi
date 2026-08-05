package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// enqueueLateInterjections re-reads the store at the finish boundary and queues any
// genuine user prompt that appeared past the baseline (handled) — a steer that landed
// after this step's top-of-loop scan but before the turn committed (final-step stream,
// or a council round that then voted done). Such a prompt was never enqueued and is
// invisible to the run goroutine's last-message-role safety net, so without this it is
// silently lost. Enqueuing (and masking) it makes the pending-interjection drain run it
// as its own fresh top-level turn — the same disposition an ordinary deferred steer gets
// — instead of dropping it. Mirrors the top-of-loop deferral (skip empty / == turnTask).
func (a *App) enqueueLateInterjections(ctx context.Context, sid session.SessionID, handled int, turnTask string) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return
	}
	prompts := userPromptEntries(evs)
	if len(prompts) <= handled {
		return
	}
	// If the last message is itself a user prompt, the run goroutine's hasUnansweredUserPrompt
	// safety net already re-runs the loop, whose top-of-loop scan handles every late prompt —
	// enqueuing here too would run it a second time (a spurious duplicate turn). Act ONLY when a
	// late prompt is buried under a trailing non-user message: the exact blind spot of that net.
	if msgs := reconstruct(evs); len(msgs) == 0 || msgs[len(msgs)-1].Role == session.RoleUser {
		return
	}
	task := strings.TrimSpace(turnTask)
	for _, it := range prompts[handled:] {
		if txt := strings.TrimSpace(it.Text); txt != "" && txt != task {
			a.markInterjectSeen(sid, it.MsgID)
			a.enqueueInterject(ctx, sid, it.MsgID, txt)
		}
	}
}

// signalTurnControl records a mid-turn routing/finish signal from a tool for the
// running loop to drain at its next step. It merges into any existing signal so a
// route and a later finish declaration in the same step window both survive.
func (a *App) signalTurnControl(sid session.SessionID, mut func(*turnControl)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tc := a.stateLocked(sid).turnControl
	mut(&tc)
	a.stateLocked(sid).turnControl = tc
}

// takeTurnControl returns and clears the pending control signal for a session.
func (a *App) takeTurnControl(sid session.SessionID) turnControl {
	a.mu.Lock()
	defer a.mu.Unlock()
	tc := a.stateLocked(sid).turnControl
	a.stateLocked(sid).turnControl = turnControl{}
	return tc
}

// enqueueInterject parks a mid-turn user interjection to be run as its own turn
// once the current turn ends (drained by the run goroutine's re-check). msgID is the
// PromptSubmitted event that carried it, so the loop can mask that event from the
// live-judgment views while the interjection stays queued (deferredInterjectIDs).
func (a *App) enqueueInterject(ctx context.Context, sid session.SessionID, msgID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	a.stateLocked(sid).pendingInterject = append(a.stateLocked(sid).pendingInterject, pendingInterjection{MsgID: msgID, Text: text})
	a.mu.Unlock()
	// Ledger the deferral so a reload can tell a still-queued interjection from a resolved
	// one after the in-memory queue is gone (F5). Written after the queue mutation and
	// outside a.mu — appendFact does store I/O and must not run under the app lock.
	a.recordDeferral(ctx, sid, msgID, false)
}

// dropQueued retires a queued message that will not run on its own: it is marked abandoned so
// seedPromptIdx never seeds a turn from it and the UI stops showing it as waiting, and ledgered
// resolved so a reload does not read it as still queued. Used for an exact repeat and for a work
// item merged into the one that carries the batch.
//
// Both writes are best-effort on purpose: they are bookkeeping for display and reload, and a store
// error must not abort a drain that has already decided what happens to the message.
func (a *App) dropQueued(ctx context.Context, sid session.SessionID, msgID string) {
	if msgID != "" {
		ad, _ := json.Marshal(event.PromptAbandonedData{MsgID: msgID})
		_ = a.appendFact(ctx, sid, event.TypePromptAbandoned, event.Actor{Kind: event.ActorSystem, ID: "loop"}, ad)
	}
	a.recordDeferral(ctx, sid, msgID, true)
}

// requeueInterjects puts items back at the FRONT of the queue, keeping their order and keeping
// them ahead of anything enqueued while they were out. Used when the drain is triaging one message
// at a time and the run context dies partway: what has not been looked at yet must be waiting where
// it was, not lost and not reordered behind newer arrivals.
func (a *App) requeueInterjects(sid session.SessionID, items []pendingInterjection) {
	if len(items) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	st.pendingInterject = append(append([]pendingInterjection{}, items...), st.pendingInterject...)
}

// dedupeInterjects drops entries whose text repeats one already in the batch and reports what
// survives, in arrival order. The dropped ones are recorded resolved and abandoned, the same way a
// coalesced-away entry is, so neither a reload nor seedPromptIdx brings them back.
//
// This is the one part of the old whole-batch coalescing worth keeping once each message is triaged
// on its own: the same sentence typed twice is one request, and answering it twice is noise. It is
// deliberately EXACT — near-duplicates ("how's it going", "?") are different sentences and each gets
// its own answer now.
func (a *App) dedupeInterjects(ctx context.Context, sid session.SessionID, q []pendingInterjection) []pendingInterjection {
	seen := map[string]bool{}
	out := make([]pendingInterjection, 0, len(q))
	for _, p := range q {
		t := strings.TrimSpace(p.Text)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, p)
			continue
		}
		a.dropQueued(ctx, sid, p.MsgID)
	}
	return out
}

// markInterjectAnswered records the agent's claim that its reply already covers this queued
// interjection, stamping the seq the claim was made at. The entry STAYS queued — the claim only
// silences the pending note and gives the finish boundary something measurable to test (was any
// assistant text persisted after this seq). No-op when the id is not queued, so a repeated or
// stale claim cannot resurrect an entry that already left.
// seq is read by the caller, not here: resolving it is store I/O and must not run under a.mu.
func (a *App) markInterjectAnswered(sid session.SessionID, msgID string, seq int64) {
	if msgID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	for i := range st.pendingInterject {
		if st.pendingInterject[i].MsgID == msgID {
			st.pendingInterject[i].AnsweredAtSeq = seq
			return
		}
	}
}

// recordDeferral appends one entry to the interjection deferral ledger (F5): Resolved:false
// when a prompt is queued as an interjection, Resolved:true when it later leaves the queue
// absorbed inline or by a route. Best-effort — a failed write only means a reload may re-run
// a stranded interjection (the prior behavior), never a crash. Empty msgID is a no-op.
func (a *App) recordDeferral(ctx context.Context, sid session.SessionID, msgID string, resolved bool) {
	if msgID == "" {
		return
	}
	data, _ := json.Marshal(event.InterjectionDeferredData{MessageID: msgID, Resolved: resolved})
	_ = a.appendFact(ctx, sid, event.TypeInterjectionDeferred, event.Actor{Kind: event.ActorSystem, ID: "interject"}, data)
}

// ensureDeferredHydrated reconstructs, once per session per process, the set of interjections
// a prior process left queued-but-unresolved (F5) and seeds them into deferredAbandoned so the
// live-view masks keep hiding them. The store read runs outside a.mu; a double-checked flag
// makes it idempotent and cheap on every later call. A read error leaves it un-hydrated so the
// next run retries rather than falsely concluding nothing was abandoned.
func (a *App) ensureDeferredHydrated(ctx context.Context, sid session.SessionID) {
	a.mu.Lock()
	done := a.stateLocked(sid).deferredHydrated
	a.mu.Unlock()
	if done {
		return
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return
	}
	abandoned := abandonedDeferrals(evs)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if st.deferredHydrated {
		return
	}
	st.deferredHydrated = true
	for id := range abandoned {
		if st.deferredAbandoned == nil {
			st.deferredAbandoned = map[string]bool{}
		}
		st.deferredAbandoned[id] = true
	}
}

// consumeInterjectByID removes the queued interjection with this MsgID (absorbed by a
// route this turn). Preferred over consumeInterject(text) when the MsgID is known: it is
// exact even when two interjections share text, and dropping it from the queue is what makes
// re-draining the same route signal a no-op — resolveRouteTarget then finds nothing.
func (a *App) consumeInterjectByID(ctx context.Context, sid session.SessionID, msgID string) {
	if msgID == "" {
		return
	}
	a.mu.Lock()
	q := a.stateLocked(sid).pendingInterject
	out := q[:0]
	removed := false
	for _, p := range q {
		if p.MsgID != msgID {
			out = append(out, p)
		} else {
			removed = true
		}
	}
	if len(out) == 0 {
		a.stateLocked(sid).pendingInterject = nil
	} else {
		a.stateLocked(sid).pendingInterject = out
	}
	a.mu.Unlock()
	// Ledger the absorbed interjection as resolved (F5) so a reload does not re-mask it.
	if removed {
		a.recordDeferral(ctx, sid, msgID, true)
	}
}

// resolveRouteTarget picks which queued interjection a route signal applies to. When the
// orchestrator named a request (idHint — a full request id or a short suffix of one, as
// surfaced by shortReqID), match it against the queued MsgIDs; otherwise fall back to the
// OLDEST queued interjection (FIFO, which is also the lowest sortable id). Returns "","" when
// nothing is queued — the interjection was already absorbed, so the route is a no-op. This is
// the routing fix: the route binds to a specific queued request, not to lastUserPromptText,
// so piled interjections neither get re-absorbed nor cross-applied.
func (a *App) resolveRouteTarget(sid session.SessionID, idHint string) (msgID, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q := a.stateLocked(sid).pendingInterject
	if len(q) == 0 {
		return "", ""
	}
	if idHint = strings.TrimSpace(idHint); idHint != "" {
		for _, p := range q {
			if p.MsgID == idHint || (len(idHint) >= 4 && strings.HasSuffix(p.MsgID, idHint)) {
				return p.MsgID, p.Text
			}
		}
	}
	return q[0].MsgID, q[0].Text
}

// shortReqID is the compact request handle shown to the model in an interjection directive
// (the last 8 chars of the MsgID). The model echoes it back as route_interjection request_id;
// resolveRouteTarget suffix-matches it. Full ids match too, so this is only for brevity.
func shortReqID(msgID string) string {
	if len(msgID) <= 8 {
		return msgID
	}
	return msgID[len(msgID)-8:]
}

// hasPendingInterject reports whether any interjection is queued for a session.
func (a *App) hasPendingInterject(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.stateLocked(sid).pendingInterject) > 0
}

// deferredInterjectIDs is the set of PromptSubmitted MessageIDs currently queued as
// interjections — the events to mask from the live-judgment views. Membership IS the
// mask lifetime: an interjection leaves the queue (drained or absorbed) exactly when it
// should become visible again, so no separate bookkeeping can drift out of sync.
func (a *App) deferredInterjectIDs(sid session.SessionID) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	q := st.pendingInterject
	ab := st.deferredAbandoned
	if len(q) == 0 && len(ab) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(q)+len(ab))
	// Interjections a prior process left queued-but-abandoned (F5) stay masked too.
	for id := range ab {
		ids[id] = true
	}
	for _, p := range q {
		if p.MsgID != "" {
			ids[p.MsgID] = true
		}
	}
	return ids
}

// liveEvents returns evs with currently-QUEUED interjection prompts removed, for the
// running turn's MODEL CONTEXT (reconstruct). A dispatch-visible interjection (one the
// orchestrator may answer while background subagents run, so it was never queued) stays
// visible here. Full history (SessionState, compaction) still sees every event.
func (a *App) liveEvents(sid session.SessionID, evs []event.Event) []event.Event {
	// Also drop the ORIGINAL prompt of any interjection that was later re-emitted as its
	// own turn (ResurfacedFrom): once resurfaced it leaves the deferred set, so without
	// this the model's context carries the same instruction twice — the stranded original
	// mid-history plus the resurfaced copy that actually seeds the turn. The display path
	// (SessionState) already dropped origins; this aligns the model's view with it. Seqs
	// of the remaining events are preserved, so compaction boundaries are unaffected.
	return filterDeferredEvents(dropResurfacedOrigins(evs), a.deferredInterjectIDs(sid))
}

// markInterjectSeen records that a MessageID is a mid-turn interjection detected this
// turn. Unlike the pending queue, this membership persists until the turn boundary
// (resetForNewTopLevel), so turnTask/council derivation stays masked from the
// interjection even after it leaves the queue (drained, or never queued because the
// orchestrator answered it inline during a background dispatch).
func (a *App) markInterjectSeen(sid session.SessionID, msgID string) {
	if msgID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if st.interjectSeen == nil {
		st.interjectSeen = map[string]bool{}
	}
	st.interjectSeen[msgID] = true
}

// interjectSeenIDs is the set of interjection MessageIDs detected this turn — the events
// to mask from turnTask/council derivation so neither is ever anchored on a mid-turn
// splice. Superset of deferredInterjectIDs (which drops entries as they drain).
func (a *App) interjectSeenIDs(sid session.SessionID) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return nil
	}
	m := st.interjectSeen
	ab := st.deferredAbandoned
	if len(m) == 0 && len(ab) == 0 {
		return nil
	}
	out := make(map[string]bool, len(m)+len(ab))
	// Abandoned deferrals from a prior process (F5) also stay masked from turnTask/council.
	for id := range ab {
		out[id] = true
	}
	for k := range m {
		out[k] = true
	}
	return out
}

// settleAnsweredClaims resolves every queued interjection the agent marked "answered", and
// returns the queue that remains. It runs at the finish boundary, before the drain coalesces
// the batch.
//
// The claim asserts the turn's reply already covers the request. magi does not judge whether it
// does — reading an answer for fit is the model's job, and a harness that overrules it would just
// be a worse model. What magi can measure is whether the turn SAID anything after the claim was
// made: assistant text persisted at a later seq. Something said clears the claim; nothing said
// revokes it, because a request silently dropped on an assertion is the one outcome the queue
// exists to prevent. A revoked claim is not an error and is not punished — the entry simply goes
// back to being an ordinary queued interjection and is triaged like the rest.
func (a *App) settleAnsweredClaims(ctx context.Context, sid session.SessionID) []pendingInterjection {
	a.mu.Lock()
	q := append([]pendingInterjection(nil), a.stateLocked(sid).pendingInterject...)
	a.mu.Unlock()
	claimed := false
	for _, p := range q {
		if p.AnsweredAtSeq != 0 {
			claimed = true
			break
		}
	}
	if !claimed {
		return q
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	var kept []pendingInterjection
	var settled []string
	for _, p := range q {
		if p.AnsweredAtSeq == 0 || !saidSomethingAfter(evs, p.AnsweredAtSeq) {
			p.AnsweredAtSeq = 0 // an unbacked claim is just a queued interjection again
			kept = append(kept, p)
			continue
		}
		settled = append(settled, p.MsgID)
	}
	a.mu.Lock()
	a.stateLocked(sid).pendingInterject = kept
	a.mu.Unlock()
	// Ledger each settled claim so a reload does not treat it as an abandoned (still-queued)
	// one and re-mask an exchange that was answered (F5) — the same bookkeeping every other
	// way out of the queue does.
	for _, id := range settled {
		a.recordDeferral(ctx, sid, id, true)
	}
	return kept
}

// saidSomethingAfter reports whether any non-empty assistant TEXT part was persisted after seq.
// Reasoning is excluded on purpose: thinking is not addressed to anyone, and a turn that only
// thought after claiming to have answered has told the user nothing. Tool calls are excluded for
// the same reason — running a command is not a reply.
func saidSomethingAfter(evs []event.Event, seq int64) bool {
	for _, e := range evs {
		if e.Seq <= seq || e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		if d.Role == session.RoleAssistant && d.Part.Kind == session.PartText &&
			strings.TrimSpace(d.Part.Text) != "" {
			return true
		}
	}
	return false
}
