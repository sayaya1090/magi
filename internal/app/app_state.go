package app

import (
	"time"

	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// sessionState holds all per-session state for one session, consolidating what used to
// be ~18 separate map[session.SessionID]X fields on App. One entry is created lazily on
// first access (state/stateLocked) and lives for the process lifetime — a nil/zero field
// is the "absent" signal (e.g. cancel==nil means no in-flight run), matching the old
// "key absent" semantics. All fields are guarded by App.mu; child sessions get their own
// entry just like top-level ones. Turn-scoped fields are zeroed by resetForNewTopLevel;
// the rest live for the whole session.
// bornFact is a session.created that has not been written yet: the actor that opened the session
// and the data it carried, kept together so the flush is one append with the original authorship.
type bornFact struct {
	actor event.Actor
	data  json.RawMessage
	// at is when the session was opened, not when the fact is written. A log whose created event
	// is stamped with the first prompt's time would say the session began when somebody spoke,
	// and the ordering the store sorts by is the sequence, not this.
	at time.Time
}

type sessionState struct {
	// Whole-session lifetime.
	cancel context.CancelFunc // in-flight run's cancel (Interrupt); nil = not running
	meta   session.Session    // session metadata cache
	// born is the session.created fact, held until the session has something in it.
	//
	// A conversation nobody has spoken in is not a conversation, and writing it to disk the moment
	// a process starts left every list of past work padded with sessions that had one event each:
	// the resume picker, the console's menu, `/sessions`. Every one of them then needed a name for
	// a thing with nothing in it.
	//
	// So the id is issued immediately — the daemon publishes it, the TUI opens on it, plugins are
	// wired to it — and the FACT waits. appendFact writes it just before whatever event finally
	// arrives, so the log's first two entries are created-then-prompt as they always were, and a
	// session that never gets a prompt leaves no file at all. nil once it has been written.
	born *bornFact
	// lookLang is the "reply in the reader's language" directive for LookOver, computed once
	// from this session's genuine user prompts and cached. LookOver fires on every pause in
	// typing, so — unlike the turn loop's language lock, which recomputes each step — reading
	// the whole event log per keystroke-pause is the cost this avoids. A session's user does
	// not switch language mid-edit; lookLangSet distinguishes "computed, English" (empty) from
	// "not computed yet".
	lookLang    string
	lookLangSet bool
	todos       []session.Todo // per-session plan
	// skillBase is the skill list this session's SYSTEM PROMPT names, frozen the first time that
	// prompt was built. skillSeen is every skill the session has been told about, including the
	// ones announced later.
	//
	// The list rides at the head of every request, so re-rendering it when the directory changes
	// rewrites position 0 and throws away the whole KV prefix — the same failure the language
	// directive above documents. And the directory does change mid-session, from more than one
	// direction: engram saves one it just learned, the agent writes one because the user asked
	// for it, the user drops a file in by hand. None of those should cost a re-read of the entire
	// conversation.
	//
	// So the head keeps the snapshot it opened with and anything discovered afterwards is
	// APPENDED — a one-line note per skill, once. The model ends up reading the frozen list plus
	// the notes, which is the same information in the order it arrived, and the next session's
	// prompt consolidates them for free.
	skillBlock    string
	skillBlockSet bool
	skillSeen     map[string]bool
	// The same arrangement for team memories, with one difference of source: a memory has no
	// timestamp, so "new" is read off the RETRIEVAL — the first match set of the session is the
	// baseline (the count pointer already advertises it), and an ID that enters the match set
	// later arrived by a side door: engram's sidecar landing a lesson, another companion feeding
	// the shared store, a person dropping a file in. Those are queued here and appended to the
	// transcript as one-line handles, once each — never folded into anything already sent.
	memSeen      map[string]bool
	memBaselined bool
	memPending   []string
	// The frozen prefix pieces — see prompt_frozen.go for why these are the only doors.
	turnSys    string
	turnSysSet bool
	// Keyed by the agent's identity: a workflow runs several agents through ONE session, each
	// with its own allowlist, and each holds its own catalog for as long as it runs. Freezing per
	// session flattened them into whoever came first — measured as verify losing bash.
	toolsFrozen map[string][]port.ToolSpec
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
	observedEvents   int                   // event high-water mark of the last turn_finished observation (stale-answer guard)
	foldedInterject  []string              // waiting messages folded into the turn now running (paired at finish)
	pendingInterject []pendingInterjection // interjections queued to run as their own turn
	turnControl      turnControl           // pending mid-turn routing / finish signal
	// councilRejects counts this turn's rejected completion declarations, and councilNoProgress
	// the CONSECUTIVE rejections with no file mutation between them (councilRejectEpoch is the
	// guard epoch at the last one). They power the rejection cap — see noteCouncilRejection.
	councilRejects     int
	councilNoProgress  int
	councilRejectEpoch int
	perms              map[string]chan string // pending permission decisions by call id
	questions          map[string]chan string // pending ask_user picks by call id
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
	// cancelSweep is the set of unanswered prompt ids as they stood the instant Interrupt was
	// called — and it is the ONLY set the cancel path may abandon.
	//
	// Cancelling clears the queue, which is right for what was already waiting: pressing stop
	// means "reset this context", not "drop the current task and keep the ones I forgot I
	// queued". But the sweep used to read the log when the turn finally tore down, which is
	// later — and anything typed in between is new intent, not queue. Measured: interrupt, then
	// a request a moment after it, and the sweep marked that request abandoned and wrote
	// "your newest request runs next" over the top of the request it had just discarded.
	// Nil means no cancel is in flight; empty-but-non-nil means a cancel with nothing to clear.
	cancelSweep map[string]bool
	// bornWait is closed when the in-flight session.created append (bear) finishes. A second
	// event racing the first through bornStore used to find born already nil, sail past, and win
	// the store's lock — landing before created and failing "first append must include
	// session.created" for a perfectly legitimate append.
	bornWait chan struct{}
	// liveTurnTask is the task the loop is CURRENTLY answering, kept where the council tool can read
	// it. The council recomputes the task from the transcript, and a redirect interjection masks its
	// own prompt from that view — so after a redirect the council judged against the ABANDONED
	// original goal and vetoed completion forever (a livelock, observed live). The loop writes the
	// re-anchored task here each step; the council prefers it over the recomputed one. Empty outside
	// a turn.
	liveTurnTask string
	// sandboxMode, when set, overrides the config's OS-sandbox mode for THIS session's tools —
	// a child spawned into its own checkout runs its shell under "workspace-write" so its writes
	// stay in that checkout, whatever the parent's global setting. Never weaker than the config:
	// effectiveSandbox keeps "read-only" when the config says so. Whole-session: a child's
	// confinement is part of what it was spawned as, not something a turn boundary revisits.
	sandboxMode string
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
// forget drops a held session.created, for the one case that writes the fact itself.
func (a *App) forget(sid session.SessionID) {
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok {
		st.born = nil
	}
	a.mu.Unlock()
}

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

// ForgetSessionState frees a session's in-memory state (its per-session maps) and usage counters.
// Used where a session is finished and will never be resumed: a spawned child evicts its own entry
// inline at the end of its run (spawn.go), and the meeting-close reap calls this on the participant
// session, which a reopen never reuses (it prepares a fresh one). Without it, a long-lived daemon
// keeps one sessionState per meeting participant for its whole life. Safe: a stray later access
// recreates an empty entry through stateLocked, so the delete cannot panic, and the transcript in
// the store is untouched — only the live in-memory state goes.
func (a *App) ForgetSessionState(sid session.SessionID) {
	a.mu.Lock()
	delete(a.states, sid)
	a.usage.forget(sid)
	a.mu.Unlock()
	// The liveness record too: it is created lazily per session and nothing else deletes it — the
	// eviction that once did (forgetLiveness) was lost in a refactor, reintroducing exactly the
	// "nothing ever deleted an entry" growth its own file documents.
	a.liveness.Delete(sid)
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
	// accepted — or when the rejection cap landed the turn over the members' heads. The loop ends
	// the turn at its next step — a turn ends because someone decided to end it, not because the
	// model happened to stop calling tools.
	finish bool
	// unverifiedReason rides with finish when the landing is the CAP's, not an acceptance: the
	// turn ends UNVERIFIED with this reason on the record. Empty on an accepted finish.
	unverifiedReason string
	reason           string
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

// setSandboxMode pins a session's OS-sandbox mode — spawnChild sets it on a child that got its own
// checkout, so the child's shell writes stay there.
func (a *App) setSandboxMode(sid session.SessionID, mode string) {
	a.mu.Lock()
	a.stateLocked(sid).sandboxMode = mode
	a.mu.Unlock()
}

// sandboxModeFor answers what OS-sandbox mode this session's tools run under: the config's, unless
// the session carries an override and the override is not WEAKER than the config.
func (a *App) sandboxModeFor(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok && st.sandboxMode != "" {
		return effectiveSandbox(a.cfg.Sandbox, st.sandboxMode)
	}
	return a.cfg.Sandbox
}

// sandboxOverridden reports whether this session carries the pinned isolated-child sandbox. It is
// what narrows the sandbox's temp allowance to the session's own scratch: the shared temp trees
// hold every sibling's clone, which is exactly what an isolated child must not write.
func (a *App) sandboxOverridden(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	return ok && st.sandboxMode != ""
}

// effectiveSandbox merges the config's sandbox mode with a per-session override. The override may
// tighten, never loosen: a config that says "read-only" keeps saying it, and "workspace-write"
// already confines at least as much as the override asks. Only an unconfined config ("" or "full")
// yields to the override — an isolated child's confinement is the contract its parallelism rests
// on, so an unconfined default does not undo it.
func effectiveSandbox(global, override string) string {
	if override == "" || global == "read-only" || global == "workspace-write" {
		return global
	}
	return override
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
	st.liveTurnTask = ""
	st.councilRejects, st.councilNoProgress, st.councilRejectEpoch = 0, 0, 0 // a new turn gets a fresh gate
	// A finish (or routing) signal a tool left in the DYING turn must not greet the next one:
	// the interrupt path returns from the step head before any drain runs, and a stale finish
	// here made the new turn's first gate adopt it — every tool call dropped as "already
	// declared" before the task did anything.
	st.turnControl = turnControl{}
	a.mu.Unlock()
}

// setLiveTurnTask records the task the loop is answering this step, so the council tool judges
// against the current goal (including a redirect's re-anchored one) rather than recomputing it from
// a transcript view that hides the redirect. turnTaskNow reads it back.
func (a *App) setLiveTurnTask(sid session.SessionID, task string) {
	a.mu.Lock()
	a.stateLocked(sid).liveTurnTask = task
	a.mu.Unlock()
}

func (a *App) turnTaskNow(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.liveTurnTask
	}
	return ""
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
	// What stop was allowed to clear, as it stood when stop was pressed. A timeout or a shutdown
	// cancels the same way without anyone pressing anything, and then there is no queue to reset:
	// this turn's own seed is over either way, but the rest of the log is somebody's waiting
	// request and stays.
	sweep := st.cancelSweep // 지우지 않는다: 정리의 마지막이 "누가 멈춤을 눌렀나"로 이것을 읽는다
	a.mu.Unlock()
	if sweep == nil {
		sweep = map[string]bool{}
		if mid != "" {
			sweep[mid] = true
		}
	}

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
	// …but only what was already standing when stop was pressed (sweep). A request that arrived
	// after it is the person's next one — the note below even promises it runs next, and the
	// sweep used to abandon exactly that request before writing the promise.
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		for _, id := range unansweredUserPromptIDs(evs) {
			if !sweep[id] {
				continue // typed after the press: new intent, not queue
			}
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
