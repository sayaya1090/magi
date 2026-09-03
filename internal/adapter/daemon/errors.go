// The errors this package hands back, and what a caller can do with each.
package daemon

import (
	"errors"
	"fmt"
)

// Refused is a daemon's own answer that it will not do the thing.
//
// Distinguished from a transport failure on purpose. Both arrive as an error and they want opposite
// reactions: a refusal is a sentence to show whoever asked, so they can pick somebody else, while a
// broken link is a machine to go and fix. Collapsed into one, the advice for the second ("it needs
// magi on its PATH") ends up printed under a companion that answered perfectly clearly.
type Refused struct{ Why string }

func (r Refused) Error() string { return r.Why }

// ErrGone is a companion whose daemon is not running — as distinct from one that could not be
// reached at all.
//
// The distinction can only be drawn on the far machine. From here a crossing that fails looks the
// same whether the network went, the login failed, or the process died, and those are a wait to
// keep running and a wait to end. So whatever carries the protocol across is expected to report
// this when it learns it, and the wire itself never carries it: a daemon that is not there cannot
// say so.
var ErrGone = errors.New("that companion is not running")

// gone is "that companion is not running", said in the words for the case at hand.
//
// Both of the sentences above are ErrGone and neither used to say so: the message was built with
// fmt.Errorf and the syscall underneath was dropped, so a caller could match the WORDS or nothing.
// One did — the console's "is it off?" check keyed on errno and therefore worked on macOS, where
// connecting to a leftover file gives ENOTSOCK and falls through this branch, and failed on Linux,
// where it gives ECONNREFUSED and lands here. That is a difference no caller should have to know.
func gone(format string, a ...any) error { return goneErr{msg: fmt.Sprintf(format, a...)} }

// ErrNotASocket is "there is something at that path and it was never a companion's door".
//
// Its own sentinel, wrapping ErrGone so every caller that only wants "cannot be reached" keeps
// working: the two are the same unreachability and different facts. A dead daemon is a thing to
// restart; a plain file where a socket belongs is a thing to look at, and a client that restarts
// on the first would otherwise do it forever on the second.
var ErrNotASocket = errors.New("that path is not a companion's socket")

func notSocket(format string, a ...any) error {
	return notSocketErr{msg: fmt.Sprintf(format, a...)}
}

type notSocketErr struct{ msg string }

func (e notSocketErr) Error() string { return e.msg }

// Is answers both questions truthfully: this is not a socket, AND nothing is reachable there.
func (e notSocketErr) Is(target error) bool {
	return target == ErrNotASocket || target == ErrGone
}

type goneErr struct{ msg string }

func (e goneErr) Error() string { return e.msg }

// Unwrap, so errors.Is(err, ErrGone) is the question a caller asks, and the sentence stays the one
// a person reads.
func (e goneErr) Unwrap() error { return ErrGone }
