package companion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Handing work to a companion on another machine.
//
// # It is the same handoff, through the same door
//
// Nothing about the delivery is different over there. The daemon that would have taken the work
// from a neighbour's dial takes it from a pipe instead, and applies the same rules it applies to a
// terminal attached beside it: is it mid-turn, where does its log stand, what does a label look
// like. So the refusal, the label above the request and the definition of "their answer" are one
// implementation — the thing that would otherwise have drifted first.
//
// This used to be a subcommand run over ssh, which re-derived on the far side, from whichever
// config directory that login landed in, what the daemon it eventually reached already knew about
// itself. Everything awkward about crossing a machine came from that. See cmd/magi/relay.go.
//
// # The way back is asked for, because it cannot be read
//
// A local wait reads the receiver's log: the answer is written where every answer is written and
// nothing has to be sent. That is the property the whole design rests on, and it does not survive
// the machine boundary — their log is a directory on their disk.
//
// So the same question is ASKED instead, down the same kind of pipe: has a turn finished past the
// point the work landed, and what did they say. One question, two ways of putting it, chosen by
// where the log is. The far side computes the answer with the same code the local watch does.
//
// # The far side pushes, down the pipe this side opened
//
// It used to poll, on the reasoning that a reply channel would need the far machine to reach THIS
// one and a laptop that can ssh to a build box is routinely not reachable from it. That reasoning
// describes a different design. The pipe is already open and already carries bytes both ways, so
// the far daemon writes down the connection its caller made — no reverse reachability, nothing
// listening here, nothing for a firewall to be asymmetric about.
//
// What polling cost was not latency. The wait's clock is three seconds, which is right for reading
// a log file on this disk and is a process spawned across a network otherwise: roughly two and a
// half thousand of them per two-hour wait, all but the last learning nothing.

// Reach opens the daemon protocol to a companion on another machine.
//
// A function rather than a spawn in here, because this package must not learn how to run processes
// — and because a test can then hand work across a machine boundary that does not exist.
//
// The context bounds the crossing. It is the only bound there is: the client speaks over a pipe
// rather than a socket, and a pipe has no deadline to set, so whatever the caller does to end the
// process is what stops a wedged link from holding a wait open.
type Reach func(ctx context.Context, host, socket string) (*daemon.Client, error)

// crossTimeout bounds one crossing. Long enough for a slow link and a cold connection, short enough
// that a machine that has gone away does not hold a tool call or a probe tick open.
const crossTimeout = 30 * time.Second

// handAcross sends the request to another machine and arranges for the answer to be fetched.
func (h Hand) handAcross(ctx context.Context, target fleet.Agent, request string, env port.ToolEnv) session.ToolResult {
	if h.Reach == nil {
		return errText(fmt.Sprintf("%s is on %s and this magi has no way to reach another "+
			"machine. Name somebody here, or do it yourself", target.Name, target.Host))
	}
	if target.Host == "" || target.Socket == "" {
		// A member with no hostname or no socket cannot be reached and cannot have been sighted
		// properly. Said rather than attempted, because a pipe to nowhere is a confusing failure.
		return errText(target.Name + " is on a machine that never said how to reach it, so there " +
			"is no way to hand it anything")
	}
	cctx, cancel := context.WithTimeout(ctx, crossTimeout)
	defer cancel()
	cl, err := h.Reach(cctx, target.Host, target.Socket)
	if err != nil {
		return errText(fmt.Sprintf("could not reach %s on %s: %v. That machine needs magi on its "+
			"PATH and this one needs to be able to `ssh %s`", target.Name, target.Host, err, target.Host))
	}
	defer cl.Close()

	receipt, herr := cl.Hand(fleet.DispatchedFrom(h.who(), h.Machine), request)
	if herr != nil {
		var refused daemon.Refused
		if errors.As(herr, &refused) {
			// Their refusal, in their words. Not reworded here, and not dressed with advice about
			// ssh: the reasons a companion cannot take work are the same sentences the local path
			// produces, and a paraphrase would be a second vocabulary for one set of facts.
			return errText(refused.Why)
		}
		return errText(fmt.Sprintf("%s did not take it: %v", target.Host, herr))
	}
	if receipt == "" {
		return errText(target.Host + " took the work but gave no receipt for it, so there would be " +
			"no way to bring an answer back. Treat it as not sent")
	}

	// Registered AFTER the crossing, unlike the local path, and for the reason the local path
	// registers before: the wait is keyed on the receipt, and the receipt does not exist until the
	// far side has answered. The gap this opens is a receiver that finishes between their submit
	// and this line — which cannot lose the answer, because the answer is fetched by position in
	// their log rather than by watching for an event to go past.
	l := h.watchAcross(target, receipt)
	if xerr := env.Expect(port.Elsewhere{
		Who: target.Name + " on " + target.Host, Session: receipt, Request: request,
		Answer: l.answer(), Probe: l.probe(), Ready: l.ready, Done: l.stop,
	}); xerr != nil {
		l.stop() // nobody is going to call Done for a wait that was never registered
		return errText(fmt.Sprintf("%s has the work, but the answer cannot be waited for here "+
			"(%v) — read their transcript on %s", target.Name, xerr, target.Host))
	}
	where := target.Workdir
	if where == "" {
		where = "their workspace"
	}
	return okText(fmt.Sprintf("Handed to %s on %s, working in %s. Carry on with the rest of your "+
		"task — their answer will arrive here when they finish, quoting what you asked. Do not "+
		"wait for it and do not send it again.", target.Name, target.Host, where))
}


// listening is one pipe held open for the life of a wait, and what the far side has said down it.
//
// The wait used to ask. Its tick is three seconds — sized for reading a log file two microseconds
// away — and across a machine every one of those ticks spawned a process: some two and a half
// thousand of them over a two-hour wait, learning nothing on all but the last.
//
// The socket was already open in both directions; only the protocol was one-way. So the far daemon
// pushes, and this holds what it pushed for the two closures the wait already reads. The wait keeps
// its own clock and its own idea of what an answer is. It is simply told when to look.
type listening struct {
	mu    sync.Mutex
	got   daemon.Handover
	ended bool // the far side said there is nothing more coming

	// ready nudges the wait. Buffered one, and a full buffer is dropped on the floor: a nudge
	// carries no news, so a second one before the first was read would say nothing new.
	ready chan struct{}
	stop  context.CancelFunc
}

func (l *listening) heard(h daemon.Handover) {
	if h == (daemon.Handover{}) {
		return // nothing to say is not a thing to wake anybody for
	}
	l.mu.Lock()
	l.got = h
	if h.Done || h.Over {
		l.ended = true
	}
	l.mu.Unlock()
	select {
	case l.ready <- struct{}{}:
	default:
	}
}

func (l *listening) state() daemon.Handover {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.got
}

func (l *listening) over() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ended
}

// answer and probe are the two questions the wait asks, answered from what was pushed rather than
// by crossing a machine to find out. Both are non-blocking reads of a struct.
func (l *listening) answer() func() (string, bool) {
	return func() (string, bool) {
		s := l.state()
		if !s.Done {
			return "", false
		}
		return s.Answer, true
	}
}

func (l *listening) probe() func() (string, bool) {
	return func() (string, bool) {
		s := l.state()
		return s.News, s.Over
	}
}

// firstRetry and slowestRetry bound how hard a dropped link is chased.
//
// Backing off matters because the commonest cause is a machine that has gone away for hours, and
// the cost of trying is a process. The slowest is also the rate an older magi — one that took the
// work but cannot stream — is polled at, which is late but is not the storm this replaced.
const (
	firstRetry   = 5 * time.Second
	slowestRetry = 2 * time.Minute
)

// watchAcross holds a connection to the far companion, and keeps holding one.
//
// Reconnection is why the receipt is the handle rather than a session and a number: a link that
// drops is re-watched with the same receipt, and the far side answers from where the work actually
// is rather than from where this side last thought it was.
func (h Hand) watchAcross(target fleet.Agent, receipt string) *listening {
	ctx, stop := context.WithCancel(context.Background())
	l := &listening{ready: make(chan struct{}, 1), stop: stop}
	reach, name := h.Reach, target.Name+" on "+target.Host
	go func() {
		wait := firstRetry
		for ctx.Err() == nil && !l.over() {
			if l.follow(ctx, reach, target, receipt, name) {
				return
			}
			// The link went, or the far side is too old to stream. Neither is a companion that
			// lost the work, so nothing is said and the wait carries on — a connection drops for a
			// closed laptop lid far more often than for a daemon that died.
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if wait *= 2; wait > slowestRetry {
				wait = slowestRetry
			}
		}
	}()
	return l
}

// follow runs one connection until it ends, and reports whether there is anything left to listen
// for.
func (l *listening) follow(ctx context.Context, reach Reach, target fleet.Agent, receipt, name string) (done bool) {
	cl, err := reach(ctx, target.Host, target.Socket)
	if err != nil {
		return false
	}
	defer cl.Close()

	heard := false
	werr := cl.Watch(receipt, func(got daemon.Handover) bool {
		heard = true
		l.heard(rename(got, target.Name, name))
		return !(got.Done || got.Over)
	})
	if werr == nil {
		return true // the stream ended cleanly: the far side has nothing more to say
	}
	if errors.Is(werr, daemon.ErrGone) {
		// Reached the machine and found no companion. The one failure that is a fact rather than a
		// silence, and it can only come from over there — from here a died daemon and a dropped
		// link are the same missing bytes.
		l.heard(daemon.Handover{Over: true, News: name + " is no longer running, and it stopped " +
			"without finishing what you handed over. Nothing will come back."})
		return true
	}
	var refused daemon.Refused
	if !errors.As(werr, &refused) || heard {
		return false // a broken link, or one that broke mid-stream
	}
	// It refused the watch. Two very different things say that and only one of them ends a wait: a
	// daemon that does not know this receipt, and a daemon too old to know this method. They are
	// told apart by asking the older question, which every version that could have taken the work
	// answers — which is why that question is still on the interface.
	//
	// On a connection of its own, because a watch gives its connection over to the stream and the
	// far side ends it along with the stream. Asking down this one would ask nothing and report a
	// closed pipe, which is the third answer and the wrong one.
	got, herr := ask(ctx, reach, target, receipt)
	switch {
	case herr == nil:
		// It knows the receipt and not the method: an older magi. There is nothing to stream, so
		// what it said now is all there is until the next attempt, on the backoff's clock.
		l.heard(rename(got, target.Name, name))
		return l.over()
	case errors.As(herr, &refused):
		// Its memory of taking the work is gone, so it restarted — and a restart did not finish
		// the turn it was in. An ending, and the one a running daemon cannot report about itself.
		l.heard(daemon.Handover{Over: true, News: name + " restarted without finishing what you " +
			"handed over. Nothing will come back. What it had done is in its transcript; the rest " +
			"was not done."})
		return true
	}
	return false // could not reach it to ask, so nothing has been established
}

// ask puts the one question on a connection of its own and hangs up.
func ask(ctx context.Context, reach Reach, target fleet.Agent, receipt string) (daemon.Handover, error) {
	cl, err := reach(ctx, target.Host, target.Socket)
	if err != nil {
		return daemon.Handover{}, err
	}
	defer cl.Close()
	return cl.Handed(receipt)
}

// rename says who and where in news written by a companion about itself. It says "design"; the
// asker may be waiting on several and needs "design on buildbox".
func rename(h daemon.Handover, was, now string) daemon.Handover {
	if h.News != "" {
		h.News = strings.Replace(h.News, was, now, 1)
	}
	return h
}
