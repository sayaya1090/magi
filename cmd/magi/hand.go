package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

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
// # Which one is decided by what is at the path, not by what this process is called
//
// Local is preferred where it applies because spawning ssh to reach a socket in the next directory
// is a process per call for an identical answer. It is not a permission decision: the socket is
// owner-only, so who may speak to a daemon is the operating system's answer at connect time, on
// either path.
//
// "Is that hostname mine" was the wrong question. A config directory can be shared — two containers
// with a mount in common, two workstations with one network home — and sockets live in the config
// directory, so a companion calling itself something else can have its socket sitting right here,
// dialable, while a comparison against this machine's name sends the work out through ssh to reach
// a path in the next directory.
//
// The record published beside the socket is asked instead, because it is what is actually there. A
// record naming the machine the caller was told about is that companion. A record naming a
// different one is somebody else's path and must not be dialled: two machines belonging to one
// person keep their checkouts in the same places, so the dial would not fail — it would open the
// wrong workspace, and the work would arrive looking delivered.

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
	// CreateSession opens the side conversation a piece of handed-over work runs in.
	CreateSession(ctx context.Context, c command.CreateSession) (session.SessionID, error)
	// Running names a session with a turn in flight here, if there is one. Isolation is of the
	// CONVERSATION; the work is still one at a time, because two turns at once in one workspace
	// are two agents editing the same files with nothing between them.
	Running() (session.SessionID, bool)
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
	work takes
	// sid is the session this companion IS — the one a person attaches to. Handed-over work never
	// goes here: somebody watching their own conversation must not have another agent's request
	// appear in it, and a request borrows the workspace and the skills, not the conversation.
	sid       session.SessionID
	workdir   string
	configDir string
	// mine is one side session per ASKER, keyed by the label they send.
	mine *sideSessions
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
	if h.receipts == nil || h.work == nil || h.mine == nil {
		return "", errors.New("this companion cannot record a handover, so its answer could never " +
			"be collected")
	}
	// One at a time, whoever is running and whatever they are running. Conversations are isolated;
	// the WORK is not, because this is an agent that edits files and two turns at once in one tree
	// are two writers with nothing between them. The person's own session counts: a request
	// borrows the workspace, and borrowing it while somebody is using it is the same collision.
	if busy, yes := h.work.Running(); yes {
		return "", fmt.Errorf("this companion is working on something else (%s) and does one thing "+
			"at a time — the workspace is shared even when the conversations are not. Try it "+
			"later, or ask somebody else", busy)
	}
	if strings.TrimSpace(label) == "" {
		// An arrival with no label still gets one. A request with no attribution is
		// indistinguishable from something the person typed, and the no-chaining rule is read off
		// exactly this mark. It is also the key below, so an unlabelled asker gets one shared
		// side session rather than a new one every time.
		label = fleet.DispatchedFrom("another companion", "")
	}
	sid, serr := h.sessionFor(ctx, label)
	if serr != nil {
		return "", serr
	}
	// Where that session's log stands before the work goes in. The answer is the first turn that
	// finishes past this point, and it is a position in THEIR session — which is why the receipt
	// carries both.
	since, _, nerr := h.work.NewSince(ctx, sid, 0)
	if nerr != nil {
		return "", fmt.Errorf("this companion's transcript cannot be read, so an answer could not "+
			"be found again: %w", nerr)
	}
	// Minted BEFORE the work goes in. The other way round, a failure to mint would leave work being
	// done that nobody can ever ask about — and an unused receipt costs nothing, because it expires.
	id, gerr := h.receipts.Give(string(sid), since)
	if gerr != nil {
		return "", gerr
	}
	// The same actor a person's own prompt gets. Who actually asked is in the label, verbatim,
	// which is where every reader looks.
	if err := h.work.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: companion.Labelled(label, request)}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "handoff"},
	}); err != nil {
		return "", err
	}
	return id, nil
}

// sessionFor is the side session belonging to one asker, opened the first time they ask.
//
// Same workspace, so the skills, the experience store and the memory are all there — a request
// borrows the capability. Only the conversation is its own.
func (h handover) sessionFor(ctx context.Context, label string) (session.SessionID, error) {
	h.mine.mu.Lock()
	defer h.mine.mu.Unlock()
	if sid, ok := h.mine.by[label]; ok {
		return sid, nil
	}
	sid, err := h.work.CreateSession(ctx, command.CreateSession{Workdir: h.workdir})
	if err != nil {
		return "", fmt.Errorf("this companion could not open a conversation for the work: %w", err)
	}
	h.mine.by[label] = sid
	return sid, nil
}

// sideSessions is one conversation per asker.
//
// Per asker and not per request, which settles two things at once: work from different askers
// never shares a history, and a second request from the same asker continues where the first left
// off — which is what a re-ask needs to be worth anything. The label is the key because it is
// derived from who is asking, is stable for them, and already crosses verbatim.
//
// A pointer, like the receipts beside it: the engine that holds this is copied by value into the
// socket's serve loop, and a map two copies disagreed about would be two askers sharing a session
// or one asker getting a new one every time.
type sideSessions struct {
	mu sync.Mutex
	by map[string]session.SessionID
}

func newSideSessions() *sideSessions { return &sideSessions{by: map[string]session.SessionID{}} }

// Handed satisfies daemon.Taker: what became of work this companion was handed.
//
// Two of the endings StateOf can report — killed, and stopped — cannot come from here, because a
// daemon that answers this is running by construction. The caller learns about a companion that
// died from the crossing failing, or from this daemon coming back not knowing the receipt.
func (h handover) Handed(ctx context.Context, receipt string) (daemon.Handover, error) {
	if h.receipts == nil || h.work == nil {
		return daemon.Handover{}, errors.New("this companion has taken no work from anybody")
	}
	sid, since, ok := h.receipts.Where(receipt)
	if !ok {
		// One answer for unknown and for expired, and none for "which piece of work was that". A
		// door that distinguishes them answers questions about work the caller did not hand over,
		// which is the whole reason there is a receipt.
		return daemon.Handover{}, errors.New("no handover here with that receipt — it was never " +
			"made here, or this companion has restarted since, or it has expired")
	}
	return h.state(ctx, session.SessionID(sid), since), nil
}

// state is what became of the work, from the position its receipt stands for.
//
// One definition and two clocks: Handed says it when asked, Watch says it when something happens.
// Written twice, a companion blocked on a person would be reported by one of them and not the
// other, and which you got would depend on how old the magi asking was.
func (h handover) state(ctx context.Context, sid session.SessionID, since int64) daemon.Handover {
	if done, answer := h.work.AnswerSince(ctx, sid, since); done {
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
	sid, since, ok := h.receipts.Where(receipt)
	if !ok {
		return errors.New("no handover here with that receipt — it was never made here, or this " +
			"companion has restarted since, or it has expired")
	}
	// The session the RECEIPT names, not this companion's own. Subscribing to the published one
	// would listen to the conversation a person is having and push when THAT moved.
	events, stop, err := h.work.Subscribe(ctx, session.SessionID(sid), since)
	if err != nil {
		return fmt.Errorf("this companion cannot follow its own work: %w", err)
	}
	defer stop()

	var said daemon.Handover
	for {
		now := h.state(ctx, session.SessionID(sid), since)
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
	if answersHere(host, socket) {
		return daemon.Dial(socket)
	}
	p, err := relayTo(ctx, host, socket)
	if err != nil {
		return nil, err
	}
	return daemon.Over(p), nil
}

// answersHere reports whether that socket is that companion, on this machine.
//
// A record is published beside every socket, so the question is answerable without dialling and
// without trusting this process's idea of its own name. No record at that path means nothing of
// ours is there; a record naming a different machine means the path is somebody else's.
func answersHere(host, socket string) bool {
	if strings.TrimSpace(host) == "" {
		// Nothing said where it is, so here is the only place to look. A sighting always carries a
		// host; this is an address assembled locally, which by definition is about this machine.
		return true
	}
	in, err := daemon.Published(socket)
	if err != nil {
		return false
	}
	return strings.EqualFold(in.Host, host)
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
