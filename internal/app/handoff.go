package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Getting an answer back from work handed to another companion.
//
// # What was missing, exactly
//
// Handing work over already worked: a request crossed to another daemon and that daemon did it.
// What did not exist was the way back. The tool's own answer used to end "they do not report back,
// so read their transcript later", which leaves an agent two bad choices — stop and poll a screen
// it cannot see, or carry on and forget. Neither is delegation; the first is a blocking call
// written by hand and the second is losing the work.
//
// # The reply is not a channel
//
// Nothing is sent back. The answer is written where every answer is written — the doer's own
// transcript — and this only goes and reads it. That is what makes it survivable: if this daemon
// is killed while the peer works, the work still happens, the answer is still written down, and
// `companions` still shows it. Only the delivery is lost, which is the right half to be fragile.
//
// It also means there is no protocol to keep in step. The peer does not know it is being waited on
// and does not have to do anything differently, so a companion running an older magi answers a
// newer one without either of them negotiating.
//
// # Merged, not queued
//
// The answer arrives as an ordinary message in the asker's conversation, attributed to the
// companion that wrote it — the same seam every other mid-turn note uses (see inject.go). The
// agent re-reads the conversation at each step, so a turn still running picks it up where it is
// and carries on. It is deliberately NOT the interjection path: an interjection is a new request
// to be answered on its own, and this is a piece of the request already being worked on.

// handoffPoll is how often the peer's log is checked.
//
// The check is a read of the tail of an append-only file that the store answers from its own cache
// with a binary search, so it is cheap; what it must not be is chatty, because the answer to "has
// a companion finished a piece of work" changes on the scale of minutes.
const handoffPoll = 3 * time.Second

// handoffCap bounds the wait. Not a deadline on the work — the peer keeps going and its answer
// stays in its transcript — but on this goroutine and on the asker's expectation. An agent told
// nothing for two hours has long since finished the turn it was going to merge the answer into,
// and a note arriving after that would land in whatever it happens to be doing next.
const handoffCap = 2 * time.Hour

// handoffProbe is how often the peer is ASKED about, as opposed to read about.
//
// Slower than the log poll on purpose: reading their log is a cached tail read, and asking about
// them means dialling their daemon. The questions it answers — is that process still there, is it
// blocked on a person — change on the scale of minutes, and a dial every three seconds against
// every companion somebody has work out with is a cost paid to learn nothing.
var handoffProbe = 30 * time.Second

// pendingHandoff is one piece of work out with somebody else.
type pendingHandoff struct {
	Who     string
	Request string
	Since   time.Time
}

// Expect registers work being done in another session and starts watching for its answer.
//
// The watch is a goroutine on a context this App's Close cancels, NOT on the tool call's context:
// the point of the whole thing is that the call returns immediately, so a watch tied to it would
// be cancelled at the moment it became useful.
func (a *App) Expect(sid session.SessionID, actor event.Actor, e port.Elsewhere) error {
	their := session.SessionID(strings.TrimSpace(e.Session))
	if their == "" {
		return fmt.Errorf("no session to read the answer from")
	}
	ctx := a.bgContext()

	// Where their log is NOW. The answer is the first turn that finishes after this point: they
	// were idle when the work was handed over (the tool refuses otherwise), so the next finish is
	// this request's. Taken before the goroutine starts, or a fast peer could finish between the
	// spawn and the first read and the watch would sit through the turn after it instead.
	//
	// Skipped when the answer is fetched rather than read: their session id indexes a store on
	// another machine, and the position that matters came back from THEIR store when the work
	// landed. There is nothing here to take a position in.
	//
	// Honest about what this is: belt, not mechanism. Removing it changes no observable behaviour
	// today, because asking this store about a session it has never seen answers zero rather than
	// failing, and because the fetched answer never consults the position anyway. It stays because
	// the day that read DOES fail — a store error, a session id that collides with a local one —
	// is the day a remote hand-off would be refused after the work had already been sent.
	var since int64
	if e.Answer == nil {
		var err error
		since, _, err = a.NewSince(ctx, their, 0)
		if err != nil {
			return fmt.Errorf("cannot read %s's transcript, so nothing would come back: %w", e.Who, err)
		}
	}

	a.mu.Lock()
	st := a.stateLocked(sid)
	st.handoffs = append(st.handoffs, pendingHandoff{Who: e.Who, Request: e.Request, Since: time.Now()})
	a.mu.Unlock()

	go a.watchHandoff(ctx, sid, actor, e, their, since)
	return nil
}

// watchHandoff waits for the peer's turn to end and writes what it said into the asker's
// conversation.
func (a *App) watchHandoff(ctx context.Context, sid session.SessionID, actor event.Actor,
	e port.Elsewhere, their session.SessionID, since int64) {

	deadline := time.NewTimer(handoffCap)
	defer deadline.Stop()
	tick := time.NewTicker(handoffPoll)
	defer tick.Stop()
	probe := time.NewTicker(handoffProbe)
	defer probe.Stop()
	told := "" // the last thing said about the peer, so one stuck state is reported once

	for {
		select {
		case <-ctx.Done():
			// This daemon is going away. Nothing is written: the work is still theirs and its
			// answer is still in their transcript, and a note appended by a process on its way out
			// would be read by whoever resumes this session as news.
			return
		case <-deadline.C:
			gaveUp := fmt.Sprintf("%s has not finished it after %s. The work is still theirs and "+
				"their answer will be in their transcript when it lands; nothing further will "+
				"arrive here.", e.Who, handoffCap)
			if err := a.deliverHandoff(ctx, sid, actor, e, gaveUp); err != nil {
				// The watch is over and the log refused it, so there is nowhere left to write this
				// down. The bus reaches a screen that is open right now, which is the only reader
				// still available — better than the silence of a discarded error.
				a.emitToolProgress(sid, actor, "", "hand_off",
					"could not record that "+e.Who+" never answered: "+err.Error())
			}
			return
		case <-probe.C:
			// Their log first, always. A peer that finished and then exited is finished — asking
			// about the process would report it gone and lose an answer that is written down.
			if done, answer := a.answerOf(ctx, e, their, since); done {
				if err := a.deliverHandoff(ctx, sid, actor, e, answer); err == nil {
					return
				}
				continue
			}
			news, over := a.probeHandoff(e)
			if news == "" || news == told {
				continue
			}
			told = news
			if !over {
				// Still possible, and worth knowing now: a companion blocked on a permission
				// prompt is waiting for a PERSON, and the asker is the only thing in a position to
				// say so to one.
				a.noteHandoff(ctx, sid, e, news)
				continue
			}
			if err := a.deliverHandoff(ctx, sid, actor, e, news); err != nil {
				told = "" // unwritten is untold; say it again next time round
				continue
			}
			return
		case <-tick.C:
		}
		done, answer := a.answerOf(ctx, e, their, since)
		if !done {
			continue
		}
		// A failed write is not a delivered answer. Their transcript still holds it and this loop
		// still has the deadline it started with, so the next tick tries again — where returning
		// here would mean the peer did the work, this process knew, and nobody was ever told.
		if err := a.deliverHandoff(ctx, sid, actor, e, answer); err != nil {
			continue
		}
		return
	}
}

// answerOf is the finished answer, read from the peer's log or asked for across a machine.
//
// One question with two ways of putting it, and the choice is made in exactly one place. The far
// side answers it by running handoffAnswer over there, so there is no second idea of what counts
// as an answer — only a second way of reaching the code that decides.
func (a *App) answerOf(ctx context.Context, e port.Elsewhere, their session.SessionID, since int64) (bool, string) {
	if e.Answer != nil {
		text, done := e.Answer()
		return done, text
	}
	return a.handoffAnswer(ctx, their, since)
}

// handoffAnswer reports whether the peer's turn has ended, and what it said.
//
// "Ended" is a turn.finished in THEIR log past the point the work was handed over. Not "they went
// idle": idle is derived from the same event and asking a second way would be a second answer to
// one question. Not their last message either — a line taken mid-turn is whatever they happened to
// be saying, which reads as a conclusion and is not one.
func (a *App) handoffAnswer(ctx context.Context, their session.SessionID, since int64) (bool, string) {
	evs, err := a.store.Read(ctx, their, since)
	if err != nil {
		return false, "" // an unreadable log is a reason to look again, not to give up
	}
	finished := false
	for _, ev := range evs {
		if ev.Type == event.TypeTurnFinished {
			finished = true
		}
	}
	if !finished {
		return false, ""
	}
	msgs, _, err := a.SessionState(ctx, their)
	if err != nil {
		return true, "They finished, but their transcript could not be read: " + err.Error()
	}
	if said := lastAssistantText(msgs); said != "" {
		return true, said
	}
	// Finished and said nothing. Reported rather than papered over as an empty answer: a blank
	// where a conclusion should be is itself the news.
	return true, "They finished without writing an answer."
}

// lastAssistantText is the final thing an agent said in a transcript.
func lastAssistantText(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != session.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, p := range msgs[i].Parts {
			if p.Kind == session.PartText && strings.TrimSpace(p.Text) != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(p.Text)
			}
		}
		if out := strings.TrimSpace(b.String()); out != "" {
			return out
		}
	}
	return ""
}

// deliverHandoff writes the answer into the asker's conversation and clears the pending record.
//
// The error is returned rather than swallowed because the write IS the feature: a discarded failure
// here means the peer did the work, this process knew, and nobody was ever told. The caller retries.
func (a *App) deliverHandoff(ctx context.Context, sid session.SessionID, actor event.Actor,
	e port.Elsewhere, answer string) error {

	// The request is quoted back. A turn that handed out three pieces gets three answers in
	// whatever order the work finished, and without it the agent has three replies and no way to
	// tell which question each one settles.
	text := fmt.Sprintf("# %s answered\n\nYou asked them:\n\n> %s\n\n---\n\n%s\n\n---\n"+
		"This is a piece of the work you are already doing, not a new request. Fold it into what "+
		"you have and carry on.",
		e.Who, clipLine(oneLine(e.Request), 400), answer)

	// context.WithoutCancel because this outlives the tool call that started it by design.
	if err := a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorSystem, ID: "handoff:" + e.Who}, text); err != nil {
		return err
	}

	// Cleared only once it is actually in the log. Clearing first and then failing to write would
	// leave the finish gate silent about a piece the agent never received.
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok {
		kept := st.handoffs[:0]
		for _, h := range st.handoffs {
			if h.Who != e.Who || h.Request != e.Request {
				kept = append(kept, h)
			}
		}
		st.handoffs = kept
	}
	a.mu.Unlock()

	// And on the bus, so a screen watching this turn shows the arrival rather than only the next
	// step's worth of reasoning built on top of it.
	d, _ := json.Marshal(event.ToolProgressData{Name: "hand_off", Text: e.Who + " answered"})
	a.publishTransient(sid, event.TypeToolProgress, actor, d)
	return nil
}

// AnswerSince is handoffAnswer for a caller outside this package: `magi --handoff-state`, run over
// ssh by the machine that handed the work here.
//
// Exported rather than reimplemented over there. "Their turn has ended and this is what they said"
// is one definition — a turn.finished past a point, then the last assistant text, with a finished
// turn that said nothing reported as such — and a second copy of it on the far side of a wire is
// how a remote answer comes to mean something slightly different from a local one.
func (a *App) AnswerSince(ctx context.Context, their session.SessionID, since int64) (bool, string) {
	return a.handoffAnswer(ctx, their, since)
}

// PendingHandoffs names the work this session has out with other companions, oldest first.
//
// Read by the finish gate: a turn that ends with a piece still out has thrown that piece away, and
// the agent is the only thing that can decide whether that matters.
func (a *App) PendingHandoffs(sid session.SessionID) []pendingHandoff {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return nil
	}
	return append([]pendingHandoff(nil), st.handoffs...)
}

// probeHandoff asks whoever wired this up whether anybody is still doing the work.
//
// The engine cannot answer it alone. Whether a process is alive, and whether it is blocked on a
// person, are not in any log — they are a dial and a question over a socket, which belong to the
// packages that own those and which this one cannot import without closing a cycle. So the caller
// that had them supplies the answer, and a hand-off registered without one simply waits.
func (a *App) probeHandoff(e port.Elsewhere) (string, bool) {
	if e.Probe == nil {
		return "", false
	}
	return e.Probe()
}

// noteHandoff says something about work still out, without ending the wait.
//
// Separate from deliverHandoff because it must NOT clear the pending record: the piece is still
// out, the watch is still running, and clearing it would make the finish gate say a turn has
// everything it asked for while it is still waiting.
func (a *App) noteHandoff(ctx context.Context, sid session.SessionID, e port.Elsewhere, news string) {
	text := fmt.Sprintf("# About %s\n\nYou asked them:\n\n> %s\n\n---\n\n%s\n\n---\n"+
		"They have not answered yet and this is not their answer. Nothing more is needed from you "+
		"to receive it — it still arrives here on its own if it comes.",
		e.Who, clipLine(oneLine(e.Request), 400), news)
	if err := a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorSystem, ID: "handoff:" + e.Who}, text); err != nil {
		// Said on the bus instead. This is news rather than the answer, so a failure to record it
		// costs a screen a line — losing it silently is what must not happen.
		a.emitToolProgress(sid, event.Actor{Kind: event.ActorSystem, ID: "handoff"}, "", "hand_off",
			e.Who+": "+news)
	}
}
