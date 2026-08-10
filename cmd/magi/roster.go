package main

import (
	"errors"
	"sync"
	"time"
)

// Keeping the list of companions in hand_off's description current.
//
// # It cannot simply be read
//
// A tool's description is rebuilt on every step of every turn (internal/app/prompt.go), and reading
// the fleet means dialling every published socket. A healthy daemon answers in well under a
// millisecond; a wedged one takes the probe's full timeout. So a synchronous read here is a
// per-step cost that is usually nothing and occasionally seconds — paid at the moment somebody is
// waiting for the model to say something, and paid worst exactly when something is already wrong.
//
// # It cannot simply be taken once
//
// Which is what it did. A daemon that came up before its cluster had converged advertised an empty
// list and went on advertising it for the life of the process: asked to hand work to a companion
// that joined a minute later, the model answered that no such companion exists. Observed across
// five machines. The refusal path reads the live list, so this was supposed to cost a turn — but a
// model shown nobody does not guess, it declines, and the turn is never spent.
//
// # So it is neither
//
// A read takes a snapshot and never waits. A snapshot older than rosterLife starts one refresh in
// the background and still returns what it has, so the step that noticed pays nothing and the next
// one gets the new list. Being a few seconds stale costs a refusal that names the right companions;
// being frozen costs a companion that can never be addressed at all.

// rosterLife is how long a list is served before a read starts a new one. Short enough that a
// companion coming up is advertised within a step or two, long enough that a turn taking twenty
// steps a minute does not dial the fleet twenty times.
const rosterLife = 15 * time.Second

type liveRoster struct {
	// build reports the list and how many companions it names, or why it could not. The error
	// matters: a listing that failed is this machine having a bad moment, and "nobody else is
	// running" is a real answer — told apart, a failed read keeps the last list; collapsed, it
	// advertises an empty cluster, which is the frozen-empty failure again in miniature.
	//
	// The count travels with the lines because a caller cannot get it back out of them, and what
	// it decides is whether anything that exists to inform a CHOICE is worth the tokens: with one
	// candidate there is no choice, and the extra paragraph is weight in every prompt of every
	// step for nothing.
	build func() (string, int, error)

	mu   sync.Mutex
	text string
	n    int
	at   time.Time
	busy bool // one refresh at a time: a slow fleet must not become a pile of goroutines
}

// newLiveRoster takes the first list synchronously, because startup is the one moment there is
// nothing to serve and nobody waiting on a step.
func newLiveRoster(build func() (string, int, error)) *liveRoster {
	l := &liveRoster{build: build, at: time.Now()}
	l.text, l.n, _ = build()
	return l
}

func (l *liveRoster) get() (string, int) {
	l.mu.Lock()
	text, n, stale := l.text, l.n, !l.busy && time.Since(l.at) >= rosterLife
	if stale {
		l.busy = true
	}
	l.mu.Unlock()
	if stale {
		go l.redo()
	}
	return text, n
}

func (l *liveRoster) redo() {
	text, n, err := l.build()
	l.mu.Lock()
	if err == nil {
		l.text, l.n = text, n
	}
	// at moves either way: a read that failed should be tried again on the next window, not
	// retried on every step until it succeeds.
	l.at, l.busy = time.Now(), false
	l.mu.Unlock()
}

// errNoReader is a magi with no store to read the fleet from — a shape that should not occur, and
// is reported rather than rendered as an empty cluster.
var errNoReader = errors.New("this magi has no way to read the published companions")
