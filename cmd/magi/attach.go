package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// attached is the engine an attached UI talks to: its own App for everything it can work out for
// itself, and the daemon for the handful of things only the process holding the run can do.
//
// The split is not a compromise. The session log is append-only and the store is a port, so a
// second process reading the same directory reconstructs the same transcript — that is the store
// port used twice, which is what having ports is for. What it cannot reconstruct is the run: the
// goroutine, its cancel, the tool blocked waiting for an answer. Those five go over the wire.
//
// Embedding *app.App means every method not named below is the local one, and a method added to
// the boundary later keeps working here without being listed. The five are overridden by being
// written out, so the file reads as "these, and only these, leave the process".
type attached struct {
	*app.App
	c *daemon.Client
	// sock is the daemon socket path, kept so a call that must NOT hold the pooled connection (a slow
	// model call) can dial a one-shot of its own — the same reason the web console has s.alone.
	sock string
	// seen is the last answer to "what is running beside the turn", kept for a moment.
	//
	// The strip asks three questions per tick — which jobs, what has each written, which children —
	// and asks them of every job, so served one round trip each that would be a socket call per job
	// per 700ms. They are one reply on the wire and one field here. Nil is allowed: a caller that
	// has not set it up (a test) simply asks every time.
	seen *jobsSeen
}

// jobsSeen caches one jobs reply for jobsFresh.
type jobsSeen struct {
	mu sync.Mutex
	at time.Time
	// sid is the session this viewer joined, so the three strip calls — which carry no session
	// argument of their own — can name it on the wire.
	sid  session.SessionID
	jobs daemon.Jobs
}

// jobsFresh is how stale the strip's picture may be. Shorter than the strip's own 700ms poll, so
// every tick gets its own answer and the cache only collapses the several calls WITHIN a tick.
const jobsFresh = 400 * time.Millisecond

// jobs answers from the last reply when it is fresh, and asks otherwise.
func (a attached) jobs(sid session.SessionID) daemon.Jobs {
	if a.seen == nil {
		j, _ := a.c.Jobs(string(sid))
		return j
	}
	a.seen.mu.Lock()
	defer a.seen.mu.Unlock()
	if time.Since(a.seen.at) < jobsFresh {
		return a.seen.jobs
	}
	j, err := a.c.Jobs(string(sid))
	if err != nil {
		// Unreachable keeps the last picture rather than emptying the strip: a dropped poll is not
		// news that everything finished, and the view says the daemon is gone by its own means.
		return a.seen.jobs
	}
	a.seen.at, a.seen.jobs = time.Now(), j
	return j
}

// BackgroundJobs, BackgroundTail and SubagentJobs are the strip along the bottom of the screen: a
// pane per background command and per spawned child, live while it runs.
//
// All three read registers that live in the memory of the process that started the work — a
// background command is a PID somebody is waiting on, and the child register is the one thing a
// session log cannot tell you, because a log does not know it is over. The embedded App answered
// out of this process's own empty registers, so the strip was blank in every attached window while
// a five-minute build ran three feet away.
//
// The children's transcripts still do not cross: a child writes its own session and both processes
// read it. What crosses is which ones exist.
func (a attached) BackgroundJobs() []app.BackgroundJob {
	out := []app.BackgroundJob{}
	for _, b := range a.jobs(a.watching()).Background {
		out = append(out, app.BackgroundJob{
			ID: b.ID, Command: b.Command, Running: b.Running, Killed: b.Killed,
			Exit: b.Exit, Started: parseWhen(b.Started),
		})
	}
	return out
}

func (a attached) BackgroundTail(id string, max int) string {
	for _, b := range a.jobs(a.watching()).Background {
		if b.ID == id {
			if len(b.Tail) > max {
				return b.Tail[len(b.Tail)-max:]
			}
			return b.Tail
		}
	}
	return ""
}

func (a attached) SubagentJobs() []app.SubagentJob {
	out := []app.SubagentJob{}
	for _, c := range a.jobs(a.watching()).Children {
		out = append(out, app.SubagentJob{
			ID: c.ID, Tool: c.Tool, Task: c.Task, Running: c.Running, Steps: c.Steps,
			Err: c.Err, Started: parseWhen(c.Started), Ended: parseWhen(c.Ended),
		})
	}
	return out
}

// ParkedWork is what is waiting for the running turn to end, from the process that holds the
// queue. Read here, this viewer would answer out of its own empty one — and an empty queue on the
// screen somebody just typed into reads as "that went nowhere".
//
// Both kinds arrive as one list in the order they will run, which is the order the daemon puts
// them in: what the person parked first, then work another companion handed over. The label goes
// in front of a handover's text, because on this panel the row is all there is to say it.
func (a attached) ParkedWork() []app.Parked {
	out := []app.Parked{}
	for _, q := range a.jobs(a.watching()).Queued {
		text := q.Text
		if q.Kind == "handover" && q.From != "" {
			text = q.From + ": " + text
		}
		out = append(out, app.Parked{Text: text})
	}
	return out
}

// watching is the session this viewer is attached to.
//
// The strip's three calls take no session argument — the registers they read are process-wide, and
// in the daemon they are read without one — so the id is recovered from the socket rather than
// threaded through an engine interface that has no room for it.
func (a attached) watching() session.SessionID {
	if a.seen != nil && a.seen.sid != "" {
		return a.seen.sid
	}
	return ""
}

// parseWhen reads a wire timestamp, or the zero time. A job whose start did not parse is still a
// job: the strip shows it with no age rather than not at all.
func parseWhen(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Submit and Steer start or redirect work. Both must happen where the loop runs; a local Submit
// would start a SECOND agent against the same store, and the two would fight over one workspace.
func (a attached) Submit(ctx context.Context, c command.SubmitPrompt) error {
	return a.c.Submit(ctx, c)
}

func (a attached) Steer(ctx context.Context, c command.SubmitPrompt) error {
	return a.c.Steer(ctx, c)
}

// Interrupt cancels the live turn. The cancel function is in the daemon's memory and nowhere else,
// so a local call would return cleanly and stop nothing — the worst shape a control has.
func (a attached) Interrupt(ctx context.Context, c command.Interrupt) error {
	return a.c.Interrupt(ctx, c)
}

// RespondPermission and RespondQuestion answer a tool that is blocked. The channel it waits on
// belongs to the daemon; answering locally would leave it waiting forever while the screen showed
// the question resolved.
func (a attached) RespondPermission(ctx context.Context, c command.RespondPermission) error {
	return a.c.RespondPermission(ctx, c)
}

func (a attached) RespondQuestion(ctx context.Context, c command.RespondQuestion) error {
	return a.c.RespondQuestion(ctx, c)
}

// Rewind and Compact REWRITE the log the daemon owns.
//
// Run locally they were worse than useless: this process would truncate or rewrite the file under a
// daemon whose sequence counter and in-memory turn state know nothing about it, so the next thing
// the daemon appended would carry a number the file had already passed. And the turn the user meant
// to undo would keep running, because the goroutine driving it is over there.
func (a attached) Rewind(ctx context.Context, sid session.SessionID, n int) (int64, error) {
	return a.c.Rewind(ctx, sid, n)
}

// EditSchedule writes here and then tells the daemon.
//
// Both halves are necessary and neither is the other's. The jobs live in a config file on a
// filesystem both processes can see, so writing locally is right — sending the edit over the socket
// would be asking the daemon to do a thing this process can do perfectly well. But the daemon holds
// the ARMED set in memory, and it learned it by reading that file; without the second call it would
// go on firing yesterday's schedule and the person who just changed it would watch nothing happen.
func (a attached) EditSchedule(workdir string, c port.ScheduleChange) (string, error) {
	out, err := a.App.EditSchedule(workdir, c)
	if err != nil {
		return out, err
	}
	// Best effort, and deliberately not an error: the file is written and correct. A daemon that
	// went away between the write and this call is a reason to say the edit lands on its next
	// start, not a reason to report that the edit failed.
	if rerr := a.c.ReloadCron(); rerr != nil {
		return out + " (the running daemon did not acknowledge it; it will pick this up when it next starts)", nil
	}
	return out, nil
}

func (a attached) Compact(ctx context.Context, c command.Compact) error {
	return a.c.Compact(ctx, c)
}

// SuggestPrompt spends the daemon's composer profile and reads the daemon's current session, so it
// goes to the daemon — done here it would run against this process's throwaway App, which has no
// provider, and return nothing while the daemon holds the model that could answer.
//
// On a DEDICATED one-shot connection, not the pooled a.c: this is a model call bounded at seconds,
// and the pooled client holds its mutex across the whole round trip — routed through a.c, a slow
// composer profile would freeze the jobs-strip poll, submit and status behind it for the daemon's
// whole deadline. The web console avoids exactly this with s.alone; the attached TUI does the same
// here. Falls back to the pooled client only when there is no socket to dial (a test).
func (a attached) SuggestPrompt(ctx context.Context, sid session.SessionID, prefix string) (string, error) {
	if a.sock == "" {
		return a.c.Suggest(prefix)
	}
	cl, err := daemon.Dial(a.sock)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	return cl.Suggest(prefix)
}

// SetModel and SetPermission change how the engine runs, and locally they changed a copy nobody was
// using: the daemon kept generating with the old model while the header showed the new name. A
// control that reports success and does nothing is the worst shape a control has.
//
// The signatures return nothing, which is the screen's contract and not this file's to change — so
// a failure to reach the daemon is logged rather than swallowed. It is the same information the
// next call over the socket will surface anyway, at the point the user does something that matters.
func (a attached) SetModel(sid session.SessionID, modelID string) {
	if err := a.c.SetModel(sid, modelID); err != nil {
		log.Printf("magi: the daemon did not take the model change: %v", err)
		return
	}
	a.App.SetModel(sid, modelID) // keep this process's own view in step, for what it renders
}

func (a attached) SetPermission(p string) {
	if err := a.c.SetPermission(p); err != nil {
		log.Printf("magi: the daemon did not take the permission change: %v", err)
		return
	}
	a.App.SetPermission(p)
}

// pollInterval is how often an attached UI re-reads the store. The daemon's bus is in the daemon's
// memory, so this is the only way live work reaches a second process. Fast enough that a tool call
// appears while you are still looking at it; slow enough that watching costs nothing.
const pollInterval = 300 * time.Millisecond

// Subscribe is the one READ that cannot be answered locally the ordinary way.
//
// App.Subscribe hands back the store's past plus the in-process bus's future, and this process's
// bus is empty — the events are published in the daemon. What both processes DO share is the log,
// so this polls it: ask for everything after the last sequence seen, forward whatever is new, wait,
// ask again.
//
// Polling rather than a second event socket, for the same reason only five calls cross at all. The
// log is append-only and already the record of what happened; a parallel stream of the same facts
// would be a second copy to keep honest, and the first thing to disagree after a reconnect.
func (a attached) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	// The first read is synchronous so a caller that gets a channel also knows the session exists —
	// the same contract App.Subscribe has, where a bad session id is an error and not an empty feed.
	first, stopFirst, err := a.App.Subscribe(ctx, sid, fromSeq)
	if err != nil {
		return nil, nil, err
	}
	pctx, stopPolling := context.WithCancel(ctx)
	cancel := func() { stopPolling(); stopFirst() }
	out := make(chan event.Event)
	go func() {
		defer close(out)
		seq := fromSeq
		send := func(src <-chan event.Event) bool {
			for e := range src {
				if e.Seq != 0 && e.Seq <= seq {
					continue // already delivered on an earlier poll
				}
				if e.Seq > seq {
					seq = e.Seq
				}
				select {
				case out <- e:
				case <-pctx.Done():
					return false
				}
			}
			return true
		}
		// The first subscription's live half never fires here (nothing publishes in this process),
		// but its past half is the backlog, so it is drained like any other poll.
		if !send(drainPast(pctx, first)) {
			return
		}
		tick := time.NewTicker(pollInterval)
		defer tick.Stop()
		asked := ""     // the prompt already put on screen, so it is drawn once and not every tick
		askedKind := "" // and which kind it was, so the right "it is over" can be sent when it goes
		note := ""      // the live note already on screen, so an unchanged one is not re-sent each tick
		user := ""      // the label already delivered, so an unchanged one is not re-sent each tick
		lost := false   // whether the screen has already been told the daemon stopped answering
		for {
			select {
			case <-pctx.Done():
				return
			case <-tick.C:
			}
			// The prompt the daemon is blocked on is NOT in the log — it is a question about what
			// should happen, not a record of what did, and the event announcing it went to the
			// daemon's bus. Without this the attached screen shows a run that simply stopped, with
			// the answer it is waiting for one keystroke away and no way to know.
			p := a.pendingPrompt(sid, asked)
			ev, id, doing := p.ev, p.id, p.doing
			drawing, cleared, reachable := p.drawing, p.cleared, p.reachable
			// A daemon that dies leaves this view looking alive: the log is still readable, so the
			// transcript keeps rendering the last thing that happened and nothing says the engine
			// is gone. The only way to find out was to type something and watch it fail. Say it
			// once, on the poll that notices.
			if !reachable && !lost {
				lost = true
				select {
				case out <- daemonLostEvent(sid):
				case <-pctx.Done():
					return
				}
			}
			if reachable {
				lost = false
			}
			switch {
			case drawing:
				asked, askedKind = id, p.kind
				select {
				case out <- ev:
				case <-pctx.Done():
					return
				}
			case cleared:
				// Answered — here, in a browser, or by policy — and the screen showing it has to
				// be told. Without this the modal stayed up over a turn that had moved on, and
				// the only way out of it was to answer a question nobody was waiting on.
				if asked != "" {
					select {
					case out <- answeredElsewhere(sid, asked, askedKind):
					case <-pctx.Done():
						return
					}
				}
				asked, askedKind = "", "" // the next prompt gets through
			}
			// Only on a change, and an empty one counts: the note going away is what takes the
			// line off the screen when the call it described returns. Sending the same text every
			// poll would work and would also mean the transcript's live region announced itself to
			// a screen reader twice a second for as long as a wait lasted.
			if reachable && doing != note {
				note = doing
				select {
				case out <- progressEvent(sid, doing):
				case <-pctx.Done():
					return
				}
			}
			// What this daemon calls the person, when a plugin has renamed them. Only on a change,
			// like the note above, and delivered as the event the screen already knows how to read.
			if reachable && p.user != user {
				user = p.user
				select {
				case out <- userLabelEvent(sid, p.user):
				case <-pctx.Done():
					return
				}
			}
			// And the mode it is on, written into this process's own App because that is what the
			// header reads on every frame — a socket call there would be one per frame. It can
			// change from the other window or from the console now, so a copy taken at attach
			// would go stale while somebody watched it.
			if reachable && p.perm != "" {
				a.App.SetPermission(p.perm)
			}
			ch, stop, err := a.App.Subscribe(pctx, sid, seq)
			if err != nil {
				continue // a transient read failure is not the end of watching
			}
			ok := send(drainPast(pctx, ch))
			stop() // release this poll subscription before the next one
			if !ok {
				return
			}
		}
	}()
	return out, cancel, nil
}

// drainPast takes only what a subscription already has and then stops waiting on it.
//
// App.Subscribe's channel stays open on the in-process bus, which in this process never speaks —
// so reading it to completion would block forever. The backlog arrives without a gap between
// events; a pause means there is nothing more in the log yet, which is exactly when this poll is
// finished and the next one should start.
func drainPast(ctx context.Context, src <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event)
	go func() {
		defer close(out)
		idle := time.NewTimer(pollInterval / 3)
		defer idle.Stop()
		for {
			select {
			case e, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(pollInterval / 3)
			case <-idle.C:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// pendingPrompt turns the daemon's answer to "what are you blocked on?" into the event the screen
// already knows how to draw.
//
// Synthesised rather than forwarded: the daemon's transient events never leave its process, so
// there is nothing to forward. What comes over the wire is the request's own fields, and this
// rebuilds the same payload the TUI would have received had the engine been in this process — the
// same call id, so the answer the user gives goes back to the tool that is waiting for it.
//
// The three outcomes are distinct on purpose. drawing says a prompt is new and here it is; the id
// alone says "still the same one, already on screen"; and cleared says the daemon has nothing
// pending, which is what lets the next prompt through even if it reuses an id.
//
// A FAILED status is none of those. Treating it as "nothing pending" would clear the marker, and
// the next poll would redraw a prompt that is already on screen — one dropped packet turning into
// two stacked modals over the same question.
// pulse is one poll's answer. A struct because it was six return values and the two facts added
// here would have made it eight, every caller unpacking positions it does not use.
type pulse struct {
	// ev is the prompt to put on screen, set when drawing.
	ev event.Event
	// id is the prompt now on screen — the new one, or the one already there.
	id string
	// doing is what a running tool last said about itself.
	doing string
	// kind is what the pending prompt is: a permission, or a question. Kept because the two are
	// put away by different events, and the answer that ends one arrives when it is already gone.
	kind string
	// user is what the daemon calls the person, and perm is the mode it is on. Both are properties
	// of that process, both can change while this view is open, and both were previously read out
	// of this process's own empty copy.
	user string
	perm string
	// drawing, cleared and reachable are three distinct outcomes on purpose. drawing says a prompt
	// is new and here it is; id alone says "still the same one, already on screen"; cleared says
	// the daemon has nothing pending, which is what lets the next prompt through even if it reuses
	// an id. A FAILED status is none of those — treating it as "nothing pending" would clear the
	// marker and the next poll would redraw a prompt that is already up.
	drawing   bool
	cleared   bool
	reachable bool
}

func (a attached) pendingPrompt(sid session.SessionID, drawn string) pulse {
	st, err := a.c.Status(string(sid))
	if err != nil {
		return pulse{id: drawn} // unknown: change nothing
	}
	p := pulse{doing: st.Doing, user: st.User, perm: st.Permission, reachable: true}
	if st.Asking != nil {
		p.kind = st.Asking.Kind
	}
	switch w := st.Asking; {
	case w == nil:
		p.cleared = true
	case w.ID == drawn:
		p.id = w.ID
	default:
		ev, err := w.Event(sid)
		if err != nil {
			p.id = drawn
			break
		}
		p.ev, p.id, p.drawing = ev, w.ID, true
	}
	return p
}

// answeredElsewhere is what a viewer needs when a prompt it is showing was answered somewhere else.
//
// A permission decision is a fact and reaches a viewer through the log by itself. A question's
// answer is not: it goes straight down a channel to the tool that was waiting, so the only thing
// that says the question is over is the daemon no longer reporting it — which is what the poll
// notices. Each kind gets the event its own surface already listens for.
func answeredElsewhere(sid session.SessionID, callID, kind string) event.Event {
	if kind == "question" {
		d, _ := json.Marshal(event.QuestionAnsweredData{CallID: callID})
		return event.Event{
			SessionID: sid, Type: event.TypeQuestionAnswered, Data: d,
			Actor: event.Actor{Kind: event.ActorSystem, ID: "attach"},
		}
	}
	// "elsewhere" rather than allow or deny: this process does not know which was chosen, and a
	// viewer that guessed would put a decision in somebody's mouth. The screen needs only to stop
	// asking; the transcript gets the real decision from the log.
	d, _ := json.Marshal(event.PermissionDecidedData{CallID: callID, Decision: "elsewhere"})
	return event.Event{
		SessionID: sid, Type: event.TypePermissionDecided, Data: d,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "attach"},
	}
}

// userLabelEvent carries a plugin's name for the person across to a viewer.
//
// The label is set in the daemon's memory and announced on the daemon's bus, so a view in another
// process never heard it — the transcript said "you" while the window beside it said who they had
// logged in as. Synthesised from the status reply, exactly as the progress note is, and only when
// it changes.
func userLabelEvent(sid session.SessionID, label string) event.Event {
	d, _ := json.Marshal(event.UserLabelData{Label: label})
	return event.Event{
		SessionID: sid, Type: event.TypeUserLabelChanged, Data: d,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "attach"},
	}
}

// progressEvent rebuilds a running tool's live note as the transient event the screen already
// draws — the same synthesis pendingPrompt does, for the same reason and out of the same reply.
//
// The `⏳` line has been in the renderer since wait_for was written, and in an ATTACHED view it
// could never once have appeared: it is fed by a transient event, transient events go to the
// engine's bus, and the engine is in the other process. So the one view most likely to be watching
// a twenty-minute wait — a viewer opened precisely because something is taking a long time — was
// the view that showed nothing about it.
func progressEvent(sid session.SessionID, text string) event.Event {
	d, _ := json.Marshal(event.ToolProgressData{Text: text})
	return event.Event{
		SessionID: sid, Type: event.TypeToolProgress, Data: d,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "attach"},
	}
}

// daemonLostEvent is what the screen shows when the engine it is watching stops answering.
//
// An error event rather than a quiet indicator: the transcript is the record this view has, and
// "the process running this session is gone" belongs in it next to whatever it last did. It is not
// written to the store — this process must not put words in another's log — so it lives exactly as
// long as the view does, which is the right lifetime for a fact about a connection.
func daemonLostEvent(sid session.SessionID) event.Event {
	d, _ := json.Marshal(event.ErrorData{
		Code: "daemon",
		Message: "the daemon driving this session stopped answering — it exited, was killed, or " +
			"its socket went away. The transcript below is what it had done up to that point; " +
			"start one again with `magi --daemon` in this directory.",
	})
	return event.Event{
		SessionID: sid, Type: event.TypeError, Data: d,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "attach"},
	}
}
