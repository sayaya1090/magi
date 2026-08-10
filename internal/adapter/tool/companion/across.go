package companion

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// # Nothing is pushed back, deliberately
//
// The receiver does not send anything and does not know it is being waited on. A reply channel
// would need the far machine to reach THIS one, and a laptop that can ssh to a build box is
// routinely not reachable from it. Polling from the side that already proved it can cross is the
// only direction that is always available.

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
	if xerr := env.Expect(port.Elsewhere{
		Who: target.Name + " on " + target.Host, Session: receipt, Request: request,
		Answer: h.answerFrom(target, receipt),
		Probe:  h.probeAcross(target, receipt),
	}); xerr != nil {
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

// answerFrom asks the far machine whether the work is finished, and for what was said.
func (h Hand) answerFrom(target fleet.Agent, receipt string) func() (string, bool) {
	reach := h.Reach
	return func() (string, bool) {
		a, err := askAcross(reach, target, receipt)
		if err != nil || !a.Done {
			return "", false
		}
		return a.Answer, true
	}
}

// probeAcross asks the far machine whether anybody is still doing the work.
func (h Hand) probeAcross(target fleet.Agent, receipt string) func() (string, bool) {
	reach := h.Reach
	name := target.Name + " on " + target.Host
	return func() (string, bool) {
		a, err := askAcross(reach, target, receipt)
		if err != nil {
			var refused daemon.Refused
			if errors.As(err, &refused) {
				// It answered, and does not know this receipt. Its memory of taking the work is
				// gone, which means it restarted — and a restart did not finish the turn it was
				// in. An ending, and the one ending a running daemon cannot report about itself.
				return name + " restarted without finishing what you handed over. Nothing will " +
					"come back. What it had done is in its transcript; the rest was not done.", true
			}
			// A machine that did not answer is not a machine that lost the work. Saying nothing
			// leaves the wait running, which is right: a link fails for a dropped wifi connection
			// far more often than for a companion that has died.
			//
			// The gap this leaves is a daemon that died and stayed dead: the crossing fails, and
			// that is indistinguishable from a link that is down, so the wait runs to its timeout.
			// Reported honestly rather than guessed at.
			return "", false
		}
		if a.News == "" {
			return "", false
		}
		return strings.Replace(a.News, target.Name, name, 1), a.Over
	}
}

// askAcross opens a crossing, asks the one question, and hangs up.
//
// One pipe per question rather than one held open for the life of the wait. A held connection is a
// process per outstanding handoff that dies with the first dropped link and has to be rebuilt
// anyway — and the answer to "are you done" is not worth keeping an ssh alive for two hours.
func askAcross(reach Reach, target fleet.Agent, receipt string) (daemon.Handover, error) {
	if reach == nil {
		return daemon.Handover{}, errNoReach
	}
	ctx, cancel := context.WithTimeout(context.Background(), crossTimeout)
	defer cancel()
	cl, err := reach(ctx, target.Host, target.Socket)
	if err != nil {
		return daemon.Handover{}, err
	}
	defer cl.Close()
	return cl.Handed(receipt)
}

var errNoReach = errors.New("this magi has no way to reach another machine")
