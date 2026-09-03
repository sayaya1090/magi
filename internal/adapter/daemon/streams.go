// Streaming and lifecycle methods: the ones serveConn answers itself.
//
// They were six `if req.Method == "…"` blocks inside a 265-line loop, and their names were also
// written by hand into acceptedMethods — which is the list that went stale and denied that methods
// this daemon accepts existed. A table means the names exist once.

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/sayaya1090/magi/internal/core/session"
)

// wire is what a stream door writes through, plus the handles only serveConn holds.
//
// A door in `answers` gets none of this: it returns one Response and the loop writes it. These six
// need more — many frames, or the daemon's own stop and restart, or the machine's home directory —
// which is exactly why they are a table rather than six exceptions.
type wire struct {
	enc *json.Encoder
	// sc is the read side, for the two doors that watch for the peer hanging up. Without a reader
	// for it, a stream whose link died holds a goroutine until the daemon stops, because there is
	// nothing to write and therefore nothing to fail.
	sc            *bufio.Scanner
	home          string
	stop, restart func()
}

// after says what serveConn does with the connection next.
//
// The blocks this replaces mixed `return` (the peer is gone, end the connection) with `continue`
// (answered, read the next request). Lifted into functions, that difference has to become a value —
// and getting one backwards leaves a connection that never closes or one that closes silently,
// neither of which a test notices easily.
type after int

const (
	next after = iota // answered; read the next request on this connection
	done              // this connection is finished
)

// stream is a method that cannot answer with a single Response, or needs something no Engine has.
type stream struct {
	run func(context.Context, Engine, Request, wire) after
	// why it is not in `answers`. Never empty: an entry with no reason is one nobody decided to put
	// here, and that is how this table grows back into the chain it replaced.
	why string
}

// streams is the second of the three tables serveConn consults.
var streams = map[string]stream{
	"roster":     {run: streamRoster, why: "reads the machine's home directory, which no Engine method answers"},
	"shutdown":   {run: streamShutdown, why: "holds this daemon's own stop"},
	"restart":    {run: streamRestart, why: "holds this daemon's own restart"},
	"update":     {run: streamUpdate, why: "answers and then restarts, so it needs both handles"},
	"watch":      {run: streamWatch, why: "gives the connection over and writes a frame per change"},
	"transcript": {run: streamTranscript, why: "gives the connection over and writes a frame per event"},
}

// streamRoster answers who is out there.
//
// Not in `answers` because that table's shape is (ctx, Engine, Request), and this is a fact about
// the MACHINE — the home directory this listener sits in — which no Engine method answers. It
// writes one frame like an ordinary door; the table it is in is about what it needs, not how much
// it writes. See roster.go.
func streamRoster(ctx context.Context, eng Engine, req Request, w wire) after {
	if w.enc.Encode(answerRoster(w.home)) != nil {
		return done // the peer is gone
	}
	return next
}

// streamShutdown answers, and then there is no listener to answer again.
//
// The reply goes out BEFORE the stop. Not because it would otherwise be lost — closing a listener
// does not touch connections already accepted, and Serve waits for in-flight handlers before it
// returns, so the write is safe either way. It is the ORDER that is honest: OK means "accepted",
// and answering after unwinding had begun would be claiming rather more than that.
//
// The stop itself must not be deferred. Deferring it waits for the peer to hang up, so a client
// that asked to shut down and then kept its connection open would leave the daemon running —
// precisely the state this call exists to get out of.
func streamShutdown(ctx context.Context, eng Engine, req Request, w wire) after {
	var resp Response
	if w.stop == nil {
		resp = Response{Err: "this daemon cannot be stopped remotely"}
		if w.enc.Encode(resp) != nil {
			return done
		}
		return next
	}
	wrote := w.enc.Encode(Response{OK: true}) == nil
	w.stop()
	if !wrote {
		return done
	}
	return next
}

// streamRestart is shutdown with a successor: the same drain, then the process re-execs onto the
// binary now on disk instead of exiting.
//
// Answered before the drain for the same honesty as shutdown — OK means "accepted, and I am on my
// way down to come back up". The relaunch happens in the daemon loop after Serve returns and the
// socket and claim are released.
func streamRestart(ctx context.Context, eng Engine, req Request, w wire) after {
	var resp Response
	if w.restart == nil {
		resp = Response{Err: "this daemon cannot be restarted remotely"}
		if w.enc.Encode(resp) != nil {
			return done
		}
		return next
	}
	wrote := w.enc.Encode(Response{OK: true}) == nil
	w.restart()
	if !wrote {
		return done
	}
	return next
}

// streamUpdate runs a self-update and, if it committed a new build, restarts onto it — B1 wiring B5
// (Commit, with rollback) to B2 (restart).
//
// Answered inline because it does I/O (a download) and the caller waits for the outcome. Not on the
// fleet-door allowlist, so the narrowed remote entry cannot carry it; what remains is the local
// socket and anything the operator has deliberately piped to it (--relay over their own ssh) — the
// same boundary shutdown has.
//
// The restart here is IMMEDIATE, unlike the auto loop's idle-gated one, and that asymmetry is
// chosen: this is an operator pressing a button and watching, and holding their reply hostage to an
// idle moment that may be minutes away would read as a hang. A restart mid-turn costs the in-flight
// step; the log keeps the rest and the session resumes.
func streamUpdate(ctx context.Context, eng Engine, req Request, w wire) after {
	u, ok := eng.(Updater)
	if !ok {
		if w.enc.Encode(Response{Err: "this daemon cannot update itself"}) != nil {
			return done
		}
		return next
	}
	res, uerr := u.Update(ctx)
	if uerr != nil {
		if w.enc.Encode(Response{Err: uerr.Error()}) != nil {
			return done
		}
		return next
	}
	if res.Updated && w.restart != nil {
		// "or on the next start": Restart refuses when a w.stop is already draining (w.stop wins),
		// and this reply has already gone out by then — so it must not promise more than the
		// binary being on disk guarantees.
		wrote := w.enc.Encode(Response{OK: true, Out: "updated " + res.From + " → " + res.To +
			" — restarting (or on the next start, if this daemon is already stopping)"}) == nil
		w.restart()
		if !wrote {
			return done
		}
		return next
	}
	msg := res.Message
	switch {
	case res.Updated:
		// Updated but nobody wired a restarter (an embedder without one): saying "up to
		// date" would hide that the binary on disk changed and this process did not.
		msg = "updated " + res.From + " → " + res.To + ", but this daemon cannot w.restart " +
			"itself — w.restart it by hand to run the new build"
	case msg == "":
		msg = "already up to date"
	}
	if w.enc.Encode(Response{OK: true, Out: msg}) != nil {
		return done
	}
	return next
}

// streamWatch turns this connection into a stream.
//
// One request, then a frame every time something changes, then the end. The daemon writes without
// being asked again — which is the whole point: the answer to handed-over work arrives when it
// arrives, and the alternative was the asking side spawning a process across a network every three
// seconds for up to two hours to find out.
//
// It does not disturb the lockstep every other caller relies on, because a watcher gives this
// connection over to it and sends nothing else down it. A UI's connection, which interleaves calls
// under one mutex, never sees an unsolicited frame.
func streamWatch(ctx context.Context, eng Engine, req Request, w wire) after {
	taker, ok := eng.(Taker)
	if !ok {
		// Refused before the connection is given over to anything, so it is still an
		// ordinary exchange and stays open like any other refusal.
		if w.enc.Encode(Response{Err: "this daemon cannot be handed work"}) != nil {
			return done
		}
		return next
	}
	// The peer hanging up is the only thing that ends a watch nothing is happening in.
	// Read for it in the background: without this, a stream whose link died holds a
	// goroutine until the daemon stops, because there is nothing to write and therefore
	// nothing to fail. Anything actually read is discarded — a watcher has said its piece.
	wctx, hungUp := context.WithCancel(ctx)
	go func() {
		for w.sc.Scan() { //nolint:revive // draining, not reading
		}
		hungUp()
	}()
	werr := taker.Watch(wctx, req.Name, func(h Handover) error {
		return w.enc.Encode(Response{OK: true, Handover: &h})
	})
	hungUp()
	if werr != nil {
		// Said the way every other refusal is said, and the only discarded write in this
		// file. It is discarded because this connection ends on the next line whatever
		// happens: a write that fails means the peer left before hearing why, which is
		// where it was going anyway. Checking it would be a check with one outcome.
		_ = w.enc.Encode(Response{Err: werr.Error()})
	}
	return done // this connection was a stream; it ends with it
}

// streamTranscript is the second method that turns a connection into a stream, framed like watch on
// purpose: one request line, then the connection is given over, then it ends when the peer hangs
// up. Two streaming styles in one protocol would be two things to get right in every client.
//
// It carries a conversation — everything already in the log, then everything that happens next —
// for the clients that have no way to read the log themselves. See Transcriber for why a READ is on
// this socket at all.
func streamTranscript(ctx context.Context, eng Engine, req Request, w wire) after {
	tr, ok := eng.(Transcriber)
	if !ok {
		// Refused before the connection is given over, so it is still an ordinary exchange
		// and this connection stays open, like every other refusal in this loop.
		if w.enc.Encode(Response{Err: "this daemon cannot read out a transcript"}) != nil {
			return done
		}
		return next
	}
	sid := session.SessionID(req.Session)
	// An invented id used to stream infinite silence — the store answers an unknown
	// session with emptiness, not an error, because "no events yet" is also what the
	// CURRENT conversation looks like before its first words (born-lazy). The one party
	// that can tell those apart is the engine's own listing, which includes the unborn
	// current on top; a daemon without that door keeps the old behaviour.
	if k, ok := eng.(ConversationKeeper); ok {
		metas, kerr := k.SessionsHere(ctx)
		if kerr == nil && !slices.ContainsFunc(metas, func(m session.SessionMeta) bool { return m.ID == sid }) {
			// Off the listing is not yet "not there": the listing is TOP-LEVEL only, and
			// this same socket advertises child session ids (jobs), whose transcripts are
			// on disk and readable. So the store is asked second — a session with any
			// events is real, whoever its parent is — and only an id that is neither
			// listed nor ever written to is refused. An unborn CHILD (spawned, no events
			// yet) would land here too; today a spawn's first act is appending, so the
			// window is the same one the born-lazy current already accepts.
			// And a store that COULD NOT answer (nerr != nil) serves rather than
			// refuses — a real child must not be refused over a transient read
			// failure, at the accepted price that an invented id under the same
			// failure streams the old silence.
			if _, known, nerr := tr.NewSince(ctx, sid, 0); nerr == nil && !known {
				if w.enc.Encode(Response{Err: fmt.Sprintf("no conversation %q in this workspace — `sessions` lists them", sid)}) != nil {
					return done
				}
				return next
			}
		}
	}
	since, note := answerable(ctx, tr, sid, req.Since)
	// The peer hanging up is the only thing that ends a transcript nothing is happening
	// in, exactly as for watch: with no reader for the hang-up, a stream whose link died
	// holds a goroutine until the daemon stops, because there is nothing to write and so
	// nothing to fail. Anything actually read is discarded — a reader has said its piece.
	rctx, hungUp := context.WithCancel(ctx)
	go func() {
		for w.sc.Scan() { //nolint:revive // draining, not reading
		}
		hungUp()
	}()
	evs, unsubscribe, serr := tr.Subscribe(rctx, sid, since)
	if serr != nil {
		hungUp()
		_ = w.enc.Encode(Response{Err: serr.Error()})
		return done
	}
	// Said BEFORE the first event, because it changes what the events that follow mean: a
	// client that asked for a tail and is being sent a whole conversation has to know, or
	// it appends the beginning of the session to the end of what it is showing.
	if note != "" && w.enc.Encode(Response{OK: true, Why: note}) != nil {
		hungUp()
		unsubscribe()
		return done
	}
	for e := range evs {
		frame := e
		if w.enc.Encode(Response{OK: true, Event: &frame}) != nil {
			break // the peer is gone
		}
	}
	hungUp()
	unsubscribe()
	return done // this connection was a stream; it ends with it
}
