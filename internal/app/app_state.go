package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// sessionState holds all per-session state for one session, consolidating what used to
// be ~18 separate map[session.SessionID]X fields on App. One entry is created lazily on
// first access (state/stateLocked) and lives for the process lifetime — a nil/zero field
// is the "absent" signal (e.g. cancel==nil means no in-flight run), matching the old
// "key absent" semantics. All fields are guarded by App.mu; child sessions get their own
// entry just like top-level ones. Turn-scoped fields are zeroed by resetForNewTopLevel;
// the rest live for the whole session.
type sessionState struct {
	// Whole-session lifetime.
	cancel           context.CancelFunc     // in-flight run's cancel (Interrupt); nil = not running
	meta             session.Session        // session metadata cache
	todos            []session.Todo         // per-session plan
	stage            string                 // current loop stage for event tagging
	lastPromptTokens int                    // real prompt_tokens from the last turn
	observedEvents   int                    // event high-water mark of the last turn_finished observation (stale-answer guard)
	pendingInterject []pendingInterjection  // interjections queued to run as their own turn
	turnControl      turnControl            // pending mid-turn routing/replan signal
	perms            map[string]chan string // pending permission decisions by call id
	questions        map[string]chan string // pending ask_user picks by call id
	grants           map[string]bool        // "always" grants per tool
	pendingAsk       chan string            // channel for a subagent's pending ask answer (parent)
	bg               *bgGroup               // background subagent tracking (parent)
	report           *subReport             // filed final report (subagent session)
	userLabel        string                 // display name for the user in the transcript (plugin set_user_label); "" = "you"
	// deferredAbandoned is the set of interjection origin MessageIDs that were queued in a
	// PRIOR process (F5 ledger) and never resolved — reconstructed once from the log on the
	// first run after load (deferredHydrated). Unlike pendingInterject (in-memory, lost on a
	// kill) these stay masked from the live turn context for the whole session so a stranded
	// interjection is not silently mixed into the next request; resetForNewTopLevel does NOT
	// clear them (the abandonment is a whole-session fact, not a per-turn one).
	deferredAbandoned map[string]bool
	deferredHydrated  bool
	// recoverySeed marks a child session spawned by the stuck-recovery lifeline: runLoop seeds
	// its turnState as already-recovered so the child cannot fire its OWN redecomposeStuck,
	// capping recovery to one executor per run tree (recoveryRunCapEnabled). Set once at spawn
	// time; not turn-scoped (a recovery child never re-enters resetForNewTopLevel — it runs at
	// depth>0).
	recoverySeed bool
	// grounded marks that the explore-first orient pass (maybeOrient) has run for this session,
	// so its deterministic environment grounding is injected exactly ONCE — at the first cold,
	// write-capable top-level turn. Session-scoped (a whole-session fact, like the two above):
	// resetForNewTopLevel does NOT clear it, since later turns already carry the environment in
	// context and re-scanning would burn budget for no new signal. See orientEnabled.
	grounded bool
	// activeSeedMsgID is the MessageID of the user prompt that SEEDS the currently
	// running top-level turn (set at step 0, loop.go). If that turn is cancelled before
	// answering, the cancel path marks this prompt abandoned (TypePromptAbandoned) so it
	// can't hijack a later, unrelated request. Overwritten at each turn's seed; the cancel
	// handler guards against staleness by re-checking it is still the unanswered seed, so
	// it is safe outside the turn-scoped reset block.
	activeSeedMsgID string
	// Turn-scoped (zeroed by resetForNewTopLevel).
	criteria          string                     // elicited acceptance criteria this turn
	minedNote         string                     // specmine result this turn (soft contract; shown to the termination council)
	seedPrompt        string                     // subagent: the spawn/unit prompt THIS child was seeded with (see seedTurnTask)
	deliverableChecks []council.DeliverableCheck // plan-audit per-step executable checks this turn
	estSteps          int                        // planner's advisory step estimate this turn
	interjectSeen     map[string]bool            // interjection MessageIDs detected this turn (masked from turnTask/council)
	awaitExplorers    bool                       // planner dispatched read-only explorers as this turn's primary work
	autoOrchestrate   bool                       // whether auto-orchestration has been triggered this session
	// Per-turn retrieval memoization. Both lookups key on the last user prompt, which is
	// constant across a turn — without this every loop step re-scans the whole experience
	// store and re-queries every plugin context provider (each with a 5s timeout) for an
	// identical query. Keyed by the query text, so a new prompt misses naturally; also
	// cleared by resetForNewTopLevel, a successful Propose (a memory the agent just saved
	// must show up), and RegisterContextProvider (a new provider must be consulted).
	expPtrQ, expPtr string // experience pointer cache: query key + rendered line
	ragQ, ragText   string // plugin-RAG cache: query key + assembled block
	// Ephemeral interjection notices. These used to be PERSISTED PromptSubmitted facts,
	// which outlived their interjection: every later turn (and a session reload) still
	// saw a stale "a new request arrived and was QUEUED" note carrying a copy of the
	// prompt — the model could re-act on an already-resolved interjection, and the
	// reload view rendered the note as a fake user bubble. Now they ride only the
	// per-step volatile context: interjectNotes is keyed by the queued interjection's
	// origin MessageID and pruned the moment that id leaves the deferred set (resolved
	// by route/drain/resurface), and asideNoteOnce (the dispatch-case "you may answer
	// briefly" nudge) is consumed by the next step's request build.
	interjectNotes map[string]string
	asideNoteOnce  string
}

// stateLocked returns the session's state, creating it on first use. The caller MUST
// hold a.mu. The nil-map guard lets zero-value App literals (used in tests) be safe;
// production always goes through New, which pre-allocates the map.
func (a *App) stateLocked(sid session.SessionID) *sessionState {
	if a.states == nil {
		a.states = map[session.SessionID]*sessionState{}
	}
	st := a.states[sid]
	if st == nil {
		st = &sessionState{}
		a.states[sid] = st
	}
	return st
}

// stateIf returns the session's state without creating one — the read/liveness path that
// preserves the old "key absent" semantics (ok==false means nothing was ever recorded).
// The caller MUST hold a.mu.
func (a *App) stateIf(sid session.SessionID) (*sessionState, bool) {
	st, ok := a.states[sid]
	return st, ok
}

// metaLocked returns a session's cached metadata, preserving the old `a.sessions[sid]`
// present/absent semantics: ok is true only for a session that was actually created
// (its meta.ID is set), never for a state entry that a lazy per-session write created.
// The caller MUST hold a.mu.
func (a *App) metaLocked(sid session.SessionID) (session.Session, bool) {
	st, ok := a.stateIf(sid)
	if !ok || st.meta.ID == "" {
		return session.Session{}, false
	}
	return st.meta, true
}

// pendingInterjection is a mid-turn user message parked to run as its own turn once
// the current one ends. MsgID is the PromptSubmitted event that carried it: while the
// interjection sits in the queue, that event is masked from the LIVE-judgment views
// (the running turn's model context and the council's per-turn scan) so it can neither
// merge into the current turn nor reset the council's turn-boundary window. It becomes
// visible again the moment it leaves the queue (drained, or absorbed via route_interjection).
type pendingInterjection struct {
	MsgID string
	Text  string
}

// turnControl is a mid-turn control signal a tool records for the running loop to
// drain at its next step. The loop owns turnTask/councilTurn/guard (stack-local),
// so a tool cannot mutate them directly; it leaves this signal instead and the loop
// applies the reground. route routes a queued user interjection (queue|redirect|
// append); replan is the agent's own "this plan is unworkable" declaration.
type turnControl struct {
	route   string // "", "queue", "redirect", or "append"
	routeID string // the request id the route targets (route_interjection request_id); "" = oldest queued
	replan  bool
	reason  string
}

// realPromptTokens returns the actual prompt token count from the last turn (0
// if not yet known).
func (a *App) realPromptTokens(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.lastPromptTokens
	}
	return 0
}

func (a *App) setPromptTokens(sid session.SessionID, n int) {
	a.mu.Lock()
	a.stateLocked(sid).lastPromptTokens = n
	a.mu.Unlock()
}

// resetForNewTopLevel clears the per-task state that must not leak from a finished
// turn into a fresh top-level request: the plan/todos, cached acceptance criteria,
// and the advisory step estimate. Used by Submit and by the queued-interjection
// drain — a message that stayed queued runs as its OWN turn, so it must be judged on
// its own merits, not held to the previous task's contract (which made the council
// judge an unrelated queued request against the finished task's leftover plan).
func (a *App) resetForNewTopLevel(sid session.SessionID) {
	a.SetTodos(sid, nil) // takes a.mu itself
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.criteria = ""           // drop cached criteria; re-elicited at the next gate (D15)
	st.minedNote = ""          // …and the previous task's mined identifier/type requirements
	st.deliverableChecks = nil // …and the previous task's plan-audit executable checks
	st.estSteps = 0            // …and the previous task's advisory step estimate
	// Reset the interjection mask, but KEEP masking anything still WAITING in the
	// queue: a queued interjection's original PromptSubmitted must stay hidden
	// until it runs as its own turn — dropping its mask here would leak it into
	// the new turn's context and double-answer it.
	var keep map[string]bool
	for _, it := range st.pendingInterject {
		if keep == nil {
			keep = map[string]bool{}
		}
		keep[it.MsgID] = true
	}
	st.interjectSeen = keep
	st.awaitExplorers = false // the async-explorer wait is per-turn; the next turn starts clean
	st.expPtrQ, st.expPtr = "", ""
	st.ragQ, st.ragText = "", "" // retrieval caches are turn-scoped even when the prompt text repeats
	a.mu.Unlock()
	// The subagent budget is a RUNAWAY backstop (one turn spawning without bound), not a
	// lifetime meter: without this reset a long interactive session accumulates ordinary
	// planner/recovery spawns across turns until every later spawn fails with "agent budget
	// exhausted" for the rest of the process. A runaway plays out within one turn, so a
	// per-turn window loses none of the protection. Process-wide (spawnCount is App-level):
	// concurrent top-level sessions share the window, which still bounds any single runaway.
	a.spawnCount.Store(0)
}

// setAwaitExplorers marks (or clears) that the planner dispatched read-only explorers as
// this turn's primary work, so the loop's pre-model park engages until they report.
func (a *App) setAwaitExplorers(sid session.SessionID, v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateLocked(sid).awaitExplorers = v
}

// awaitingExplorers reports whether this turn is parked waiting for planner-dispatched
// read-only explorers. Distinguishes the async-explorer scenario (park pre-model, no
// findings-less review) from ordinary background delegation (interleave own work).
func (a *App) awaitingExplorers(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.awaitExplorers
	}
	return false
}

// setActiveSeed records the MessageID of the prompt seeding the current top-level turn,
// so a cancel can mark exactly that prompt abandoned (see abandonSeedOnCancel).
func (a *App) setActiveSeed(sid session.SessionID, msgID string) {
	a.mu.Lock()
	a.stateLocked(sid).activeSeedMsgID = msgID
	a.mu.Unlock()
}

// abandonSeedOnCancel marks the cancelled turn's seed prompt abandoned so it does not
// seed a later, unrelated request. It only writes the marker when that prompt is STILL
// the unanswered seed (guarding against a stale activeSeedMsgID from a turn that already
// answered or was superseded — seedPromptIdx already skips those). The cancelled prompt's
// text stays in the log/context, so a follow-up that augments it still has the history.
func (a *App) abandonSeedOnCancel(ctx context.Context, sid session.SessionID) {
	a.mu.Lock()
	st := a.stateLocked(sid)
	mid := st.activeSeedMsgID
	st.activeSeedMsgID = ""
	// Cancelling the running turn also clears the QUEUE: a user who presses Esc means
	// "reset this context", not "drop only the current task and keep the ones I already
	// forgot I queued". Draining here stops a stale queued interjection from seeding the
	// NEXT turn and surprising the user (who sent a fresh request expecting IT to run,
	// then watched a forgotten old one execute instead). The drained prompts stay in the
	// log — a later request that augments one still has its text — they just no longer SEED.
	queued := st.pendingInterject
	st.pendingInterject = nil
	a.mu.Unlock()

	// Abandon each drained interjection + resolve its deferral ledger entry, so neither
	// seedPromptIdx nor a reload re-runs it. Best-effort ordering: markers first.
	drained := 0
	for _, p := range queued {
		if p.MsgID == "" {
			continue
		}
		dd, _ := json.Marshal(event.PromptAbandonedData{MsgID: p.MsgID})
		_ = a.appendFact(ctx, sid, event.TypePromptAbandoned, event.Actor{Kind: event.ActorSystem, ID: "loop"}, dd)
		a.recordDeferral(ctx, sid, p.MsgID, true) // resolved: left the queue (abandoned)
		drained++
	}

	if mid != "" {
		if evs, err := a.store.Read(ctx, sid, 0); err == nil && promptUnanswered(evs, mid) {
			d, _ := json.Marshal(event.PromptAbandonedData{MsgID: mid})
			_ = a.appendFact(ctx, sid, event.TypePromptAbandoned,
				event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
		}
	}
	// Tell the user their queue was cleared, so the cancel's wider effect is not silent
	// (B-note): "cancelled + N queued request(s) also cleared". Only when something was
	// actually queued — a plain cancel with an empty queue stays quiet. Rendered via the
	// system-note path (ActorSystem PromptSubmitted → the TUI/headless "⟳ … note" line).
	if drained > 0 {
		_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"},
			fmt.Sprintf("cancelled — %d queued request(s) also cleared; your newest request runs next.", drained))
	}
}
