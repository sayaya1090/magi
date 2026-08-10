package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Reaching another companion's daemon, wherever it is.
//
// # One function, because there is one question
//
// "What can you do", "take this work" and "what became of it" are all things a companion knows
// about ITSELF. Each used to be a subcommand run over ssh, and each was a fresh process that read a
// config directory, listed the daemons in it, resolved a name and dialled a socket — to work out
// what the daemon it finally reached already knew.
//
// So there is no separate remote path any more. There is a client, and two ways of getting a pipe
// to hold it: a direct dial when the companion is here, a relay when it is not. Everything above
// this line speaks the same protocol either way.
//
// # The host comparison is a shortcut, not the rule
//
// Local is preferred where it applies because spawning ssh to a socket in the next directory is a
// process per call for an identical answer. It is not a permission decision: the socket is
// owner-only, so who may speak to a daemon is the operating system's answer at connect time, on
// either path.
//
// It is also the wrong discriminator in one known case — containers on one host sharing a socket
// directory can dial each other directly, and a hostname says they cannot. The real question is
// whether that socket answers HERE. Left as it is for now, because being wrong costs one ssh hop
// and not a wrong answer.

// takes is what a handover needs from the companion it is part of.
//
// Narrow on purpose. The daemon that answers is the whole agent, and a test of "did the work land
// and can it be asked about" should not have to build one — nor should it start a real turn to find
// out that a receipt was minted.
type takes interface {
	fleet.Reader
	// AnswerSince is "they finished, and this is what they said". The same call a local wait makes
	// on a peer's log: that phrase has three edge cases in it, and two implementations of it would
	// differ on one.
	AnswerSince(ctx context.Context, sid session.SessionID, since int64) (bool, string)
	Submit(ctx context.Context, cmd command.SubmitPrompt) error
	// Subscribe is how a watch learns that something happened without asking. The alternative was
	// the far side asking across a network on a three-second timer, which is what this replaces.
	Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error)
}

// handover is the part of a daemon that takes work from other companions.
//
// There is no session argument anywhere in it, and that is the whole shape of the thing: whoever is
// asking connected to THIS companion, and a companion is one conversation. What used to be a name
// resolved on arrival, against a config directory belonging to whichever account the login landed
// as, is now the socket the caller already reached.
type handover struct {
	work      takes
	sid       session.SessionID
	configDir string
	// receipts is what this daemon has taken, and the only way to ask about any of it. nil is
	// possible in a partly-built engine; the methods say so rather than crash.
	receipts *daemon.Receipts
}

// Hand satisfies daemon.Taker: work handed in from another machine, taken by the companion doing it.
//
// Every refusal here is an error, and every error is a refusal — the caller reached this daemon and
// this daemon answered. A link that broke fails earlier, in the client, and reads differently there
// on purpose: one is a companion to ask later, the other is a machine to go and fix.
func (h handover) Hand(ctx context.Context, label, request string) (string, error) {
	if strings.TrimSpace(request) == "" {
		return "", errors.New("a request needs something to do")
	}
	if h.receipts == nil || h.work == nil {
		return "", errors.New("this companion cannot record a handover, so its answer could never " +
			"be collected")
	}
	// Whether it can take anything right now, decided by the same predicate that guards a handoff
	// between neighbours. Asked about ITSELF: nothing was addressed by name, so there is nothing to
	// resolve — only whether the process that was reached is in a state to take work. Its own
	// record, read from its own store, as its own user.
	list, lerr := fleet.List(ctx, h.work, h.configDir, "")
	if lerr != nil {
		return "", fmt.Errorf("this companion cannot read its own record, so it will not take work "+
			"it may be in no state to do: %w", lerr)
	}
	me, found := fleet.Agent{}, false
	for _, a := range list {
		if a.Session == string(h.sid) {
			me, found = a, true
			break
		}
	}
	if !found {
		return "", errors.New("this companion is not published, so nothing can be handed to it")
	}
	if refused := companion.Ready(me); refused != "" {
		return "", errors.New(refused)
	}
	// Where the log stands before the work goes in. This is the whole definition of the answer —
	// the first turn that finishes past this point — and it is what keeps the hour of finished
	// turns behind it from being handed back as a reply.
	//
	// Before rather than after because that is what the sentence says. Submit returns before the
	// turn it starts has ended, so after would usually give the same number; usually is not a
	// property, and the day it is not, the way back returns the answer to something else.
	since, _, nerr := h.work.NewSince(ctx, h.sid, 0)
	if nerr != nil {
		return "", fmt.Errorf("this companion's transcript cannot be read, so an answer could not "+
			"be found again: %w", nerr)
	}
	// Minted BEFORE the work goes in. The other way round, a failure to mint would leave work being
	// done that nobody can ever ask about — and an unused receipt costs nothing, because it expires.
	id, gerr := h.receipts.Give(since)
	if gerr != nil {
		return "", gerr
	}
	if strings.TrimSpace(label) == "" {
		// An arrival with no label still gets one. A request with no attribution is
		// indistinguishable from something the person typed, and the no-chaining rule is read off
		// exactly this mark.
		label = fleet.DispatchedFrom("another companion", "")
	}
	// The same actor a handoff between neighbours produces. That one arrives through this daemon's
	// own socket and is recorded as an attached caller; this one arrives through a pipe to the same
	// socket. Who actually asked is in the label, verbatim, which is where every reader looks.
	if serr := h.work.Submit(ctx, command.SubmitPrompt{
		SessionID: h.sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: companion.Labelled(label, request)}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "attach"},
	}); serr != nil {
		return "", serr
	}
	return id, nil
}

// Handed satisfies daemon.Taker: what became of work this companion was handed.
//
// Two of the endings StateOf can report — killed, and stopped — cannot come from here, because a
// daemon that answers this is running by construction. The caller learns about a companion that
// died from the crossing failing, or from this daemon coming back not knowing the receipt.
func (h handover) Handed(ctx context.Context, receipt string) (daemon.Handover, error) {
	if h.receipts == nil || h.work == nil {
		return daemon.Handover{}, errors.New("this companion has taken no work from anybody")
	}
	since, ok := h.receipts.Since(receipt)
	if !ok {
		// One answer for unknown and for expired, and none for "which piece of work was that". A
		// door that distinguishes them answers questions about work the caller did not hand over,
		// which is the whole reason there is a receipt.
		return daemon.Handover{}, errors.New("no handover here with that receipt — it was never " +
			"made here, or this companion has restarted since, or it has expired")
	}
	return h.state(ctx, since), nil
}

// state is what became of the work, from the position its receipt stands for.
//
// One definition and two clocks: Handed says it when asked, Watch says it when something happens.
// Written twice, a companion blocked on a person would be reported by one of them and not the
// other, and which you got would depend on how old the magi asking was.
func (h handover) state(ctx context.Context, since int64) daemon.Handover {
	if done, answer := h.work.AnswerSince(ctx, h.sid, since); done {
		return daemon.Handover{Done: true, Answer: answer}
	}
	// Not finished. Then the other question: is anybody still doing it, or is this silence
	// permanent — which is the whole reason the waiting side asks at all.
	list, lerr := fleet.List(ctx, h.work, h.configDir, "")
	if lerr != nil {
		return daemon.Handover{}
	}
	news, over := companion.StateOf(list, string(h.sid))
	return daemon.Handover{News: news, Over: over}
}

// Watch satisfies daemon.Taker: the same answer, pushed when it changes.
//
// What this replaces is the asking side spawning a process across a network every three seconds for
// up to two hours — a tick sized for reading a log file two microseconds away, left driving an ssh.
//
// It looks BEFORE it waits, every time round. A turn that ended between the subscription and the
// first look would otherwise be an event nobody is listening for yet and a state nobody has read.
func (h handover) Watch(ctx context.Context, receipt string, say func(daemon.Handover) error) error {
	if h.receipts == nil || h.work == nil {
		return errors.New("this companion has taken no work from anybody")
	}
	since, ok := h.receipts.Since(receipt)
	if !ok {
		return errors.New("no handover here with that receipt — it was never made here, or this " +
			"companion has restarted since, or it has expired")
	}
	events, stop, err := h.work.Subscribe(ctx, h.sid, since)
	if err != nil {
		return fmt.Errorf("this companion cannot follow its own work: %w", err)
	}
	defer stop()

	var said daemon.Handover
	for {
		now := h.state(ctx, since)
		if now != said {
			said = now
			// Nothing to say is not a thing to send. A companion that was blocked and is now
			// working again goes back to the zero state, and a frame carrying it would arrive as
			// news with no words in it.
			if now != (daemon.Handover{}) {
				if say(now) != nil {
					return nil // the peer is gone; the work is unaffected and stays theirs
				}
			}
		}
		if now.Done || now.Over {
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case e, open := <-events:
				if !open {
					return nil
				}
				if !worthLooking(e.Type) {
					continue
				}
			}
			break
		}
	}
}

// worthLooking is the events that can change what became of handed-over work.
//
// Everything else — a token, a tool call, a plan edit — is the companion working, which is the
// state the watcher is already in and does not need telling about. It matters that this is a short
// list: a look costs rebuilding the transcript and probing the fleet, and doing that per token
// would make a watched companion slower than an unwatched one.
func worthLooking(t event.Type) bool {
	switch t {
	case event.TypeTurnFinished, event.TypePermissionRequested, event.TypeQuestionRequested,
		event.TypePermissionDecided, event.TypeError:
		return true
	}
	return false
}

// reachCompanion opens the daemon protocol to a companion, here or on another machine.
//
// The context bounds a remote crossing by killing the process that carries it. A local dial ignores
// it, the way every other local dial in this tree does: a unix socket either answers or does not.
func reachCompanion(ctx context.Context, host, socket string) (*daemon.Client, error) {
	if here := daemon.Host(); host == "" || (here != "" && strings.EqualFold(host, here)) {
		return daemon.Dial(socket)
	}
	p, err := relayTo(ctx, host, socket)
	if err != nil {
		return nil, err
	}
	return daemon.Over(p), nil
}

// describeCompanion answers "what can X do" for a companion anywhere in the cluster.
//
// Asked of the companion rather than worked out about it. A stopped companion therefore cannot be
// described, where the version that read a workspace could describe one — which is the right way
// round: this exists to be read before handing somebody work, and a companion that cannot be
// reached cannot be handed any.
func describeCompanion(ctx context.Context, host, socket string) (string, error) {
	cl, err := reachCompanion(ctx, host, socket)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	return cl.About()
}
