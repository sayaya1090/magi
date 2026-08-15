package app

import (
	"time"

	"context"
	"encoding/json"
	"fmt"

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
	cancel context.CancelFunc // in-flight run's cancel (Interrupt); nil = not running
	meta   session.Session    // session metadata cache
	todos  []session.Todo     // per-session plan
	// turnNotes are what the AGENT itself asked to be reminded of before this turn ends
	// (remember{scope:"turn"}). magi stores them verbatim and hands them back at the finish
	// seams — it never reads, summarises, or ranks them. Turn-scoped: a new top-level request
	// is new work, and last turn's reminders would be answering a question nobody asked.
	turnNotes        []string
	lastPromptTokens int       // real prompt_tokens from the last turn
	turnStart        time.Time // when the current turn began (check_untouched.go: "did this predate the run")
	// worldBase is the workspace index taken when this turn started: path → size, for the files
	// the snapshot's own walk would look at. The finish snapshot is the difference against it,
	// which is the only way a DELETED file — or a write that preserved its mtime — shows up at
	// all. Replaced each turn; nil until one starts.
	worldBase        fileIndex
	observedEvents   int                    // event high-water mark of the last turn_finished observation (stale-answer guard)
	foldedInterject  []string               // waiting messages folded into the turn now running (paired at finish)
	pendingInterject []pendingInterjection  // interjections queued to run as their own turn
	turnControl      turnControl            // pending mid-turn routing / finish signal
	perms            map[string]chan string // pending permission decisions by call id
	questions        map[string]chan string // pending ask_user picks by call id
	// asking is what those two are waiting FOR, so another process can be told. The channels
	// above are enough to deliver an answer and say nothing about the question; a viewer in a
	// different process cannot see the transient event that announced it, because a transient
	// event is delivered to this process's bus and nowhere else. Keyed the same way (call id /
	// question id) so a resolved prompt leaves with its channel.
	asking map[string]Ask
	// doing is the latest live progress note a long-running tool published, and the call it came
	// out of. Kept for exactly the reason `asking` is: it rides a TRANSIENT event, delivered to
	// this process's bus and nowhere else, so a viewer in another process cannot see it at all —
	// and "what is it actually doing" is the question somebody has about a turn that has been open
	// for five minutes. Turn-scoped: last turn's note is not news about this one.
	doing     string
	doingCall string
	// handoffs is the work this session has out with other companions and has not heard back on.
	// Whole-session rather than turn-scoped: a peer can answer after the turn that asked ends, and
	// clearing the record at a turn boundary would make the watch still running invisible.
	handoffs []pendingHandoff
	// answered is what came back and has not been judged yet. Kept apart from handoffs, which is
	// what is still OUT: one drives a note about an unfinished piece, the other a question about
	// a finished one, and folding them together would make a turn that is waiting look like a
	// turn that owes an opinion.
	answered []answeredHandoff
	grants   map[string]bool // "always" grants per tool
	// handedFrom is who asked, when this conversation was opened for work another COMPANION
	// handed over rather than for a person at a keyboard. Empty means the ordinary kind.
	//
	// It changes what approval means here — see requestPermission — and it is set once at creation
	// and never cleared: where a conversation came from does not change. The NAME and not just a
	// flag, because the person being asked to approve a command needs to know whose errand it is.
	handedFrom string
	userLabel  string // display name for the user in the transcript (plugin set_user_label); "" = "you"
	// deferredAbandoned is the set of interjection origin MessageIDs that were queued in a
	// PRIOR process (F5 ledger) and never resolved — reconstructed once from the log on the
	// first run after load (deferredHydrated). Unlike pendingInterject (in-memory, lost on a
	// kill) these stay masked from the live turn context for the whole session so a stranded
	// interjection is not silently mixed into the next request; resetForNewTopLevel does NOT
	// clear them (the abandonment is a whole-session fact, not a per-turn one).
	deferredAbandoned map[string]bool
	deferredHydrated  bool
	// activeSeedMsgID is the MessageID of the user prompt that SEEDS the currently
	// running top-level turn (set at step 0, loop.go). If that turn is cancelled before
	// answering, the cancel path marks this prompt abandoned (TypePromptAbandoned) so it
	// can't hijack a later, unrelated request. Overwritten at each turn's seed; the cancel
	// handler guards against staleness by re-checking it is still the unanswered seed, so
	// it is safe outside the turn-scoped reset block.
	activeSeedMsgID string
	// Turn-scoped (zeroed by resetForNewTopLevel).
	scratch       *turnScratch    // the turn's scratch directory (created at depth 0, inherited by every child of that turn)
	interjectSeen map[string]bool // interjection MessageIDs detected this turn (masked from turnTask/council)
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
	// BoundarySeen marks an entry a finish boundary has already been past while it stayed queued.
	// Only those are re-decided when the next turn starts: an entry the CURRENT turn's boundary has
	// not reached yet is not a leftover, and taking it early would move work out from under the
	// drain that owns it.
	BoundarySeen bool
	// AnsweredAtSeq is the event seq at which the agent claimed, via route_interjection
	// action "answered", that its reply already covers this request. 0 means no claim.
	//
	// The claim stops the pending-interjection note at once — that note is the thing the user
	// sees as "still queued" — but it does not resolve the entry, because a claim is not an
	// answer. At the finish boundary magi checks the one fact it can measure: whether any
	// assistant TEXT was persisted after this seq. Something said is not proof it answers the
	// question; nothing said is proof it does not, and that is the case worth catching, because
	// the alternative is a user request dropped on an assertion.
	AnsweredAtSeq int64
}

// turnControl is a mid-turn control signal a tool records for the running loop to
// drain at its next step. The loop owns turnTask/councilTurn/guard (stack-local),
// so a tool cannot mutate them directly; it leaves this signal instead and the loop
// applies the reground. route routes a queued user interjection (queue|redirect|
// append); finish is the agent's accepted completion declaration.
type turnControl struct {
	route   string // "", "queue", "redirect", or "append"
	routeID string // the request id the route targets (route_interjection request_id); "" = oldest queued
	// finish is set when the agent declared the task complete (the council tool) and the council
	// accepted. The loop ends the turn at its next step — a turn ends because someone decided to
	// end it, not because the model happened to stop calling tools.
	finish bool
	reason string
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
	st.expPtrQ, st.expPtr = "", ""
	st.turnNotes = nil
	st.doing, st.doingCall = "", ""
	st.ragQ, st.ragText = "", "" // retrieval caches are turn-scoped even when the prompt text repeats
	a.mu.Unlock()
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

	// Every unanswered user prompt in the log, not just the seed and the detected queue. A message
	// Steer'd into the log a moment before the interrupt is in neither activeSeedMsgID nor
	// pendingInterject — the loop had not reached the step boundary that detects it — so it survived
	// the cancel and ran later, ahead of the user's newer request. Cancel means reset this context;
	// draining the whole unanswered set is what makes that true.
	// Read AFTER the queue loop's abandonments above, so the detected interjections are already
	// marked and excluded here — this catches the seed and any undetected Steer'd prompt without
	// double-abandoning what the queue already handled.
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		for _, id := range unansweredUserPromptIDs(evs) {
			d, _ := json.Marshal(event.PromptAbandonedData{MsgID: id})
			_ = a.appendFact(ctx, sid, event.TypePromptAbandoned,
				event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
			if id != mid {
				drained++ // an undetected queued prompt; count it toward the "N queued also cleared" note
			}
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
