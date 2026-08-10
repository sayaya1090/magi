package companion

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Handing work to a companion published on THIS machine, as two steps anybody can take.
//
// # Why this is not inside the tool
//
// Because the tool is not the only caller any more. Work handed across machines arrives over ssh as
// `magi --hand <name>`, and that command does exactly what the tool does once the address has been
// resolved: check the target can take it, and put it in their session.
//
// Written once and reached two ways, rather than twice. The alternative was a second delivery path
// for remote work, and the two would have agreed on the day they were written — the mid-turn
// refusal would have been in one of them a month later, and the answer to a remotely-handed request
// would have landed inside whatever the receiver was already doing.
//
// # Two steps and not one
//
// Choosing the target and sending to it are separate calls because something has to happen in
// between, and it is different in the two places. The tool registers its wait first (a peer quick
// enough to finish in the gap would otherwise have its turn closed before anybody looked). The ssh
// command instead records where the receiver's log was, which is what it sends back as the point
// the answer will be found after.

// Target picks the companion an address names and says whether it can take work right now.
//
// The refusal is the return value that matters. Every one of them is a sentence a model reads and
// can act on — a wrong name comes back with the list, a busy companion comes back with what it is
// busy with — because the alternative is a tool that fails and leaves nothing to do next.
//
// here is the caller's own socket when the caller is on this machine, and empty when the request
// arrived from another one: nothing over there is "you".
func Target(ctx context.Context, r fleet.Reader, cache *fleet.Cache, configDir, here, to string) (fleet.Agent, string) {
	list, err := fleet.ListCached(ctx, r, configDir, here, cache)
	if err != nil {
		return fleet.Agent{}, "cannot read the published companions: " + err.Error()
	}
	found := fleet.Resolve(list, to)
	switch len(found) {
	case 0:
		return fleet.Agent{}, fmt.Sprintf("nobody here is called %q or does that. There is: %s",
			to, fleet.Roster(list))
	case 1:
	default:
		return fleet.Agent{}, fmt.Sprintf("%q matches %s — name one of them. Sending work to the "+
			"wrong workspace is not something to guess at", to, fleet.Names(found))
	}
	target := found[0]
	if target.Here {
		return target, "that is you. Do it yourself, or name somebody else"
	}
	return target, Ready(target)
}

// Ready says whether a companion can take work right now, and why not if it cannot.
//
// Split out from Target because the answer is needed from two sides. Somebody choosing a companion
// asks it about them; a companion asked directly, over the relay from another machine, asks it
// about ITSELF — there is no address to resolve in that case, only the question of whether the
// process that was reached is in a state to take anything.
//
// One predicate, so the two cannot come to differ on what "busy" means. Two of them, and work
// crossing a machine would land in a running turn on exactly the days the local path refused it.
func Ready(a fleet.Agent) string {
	switch {
	case !a.Live:
		return fmt.Sprintf("%s is not running, so there is nothing to hand the work to", a.Name)
	case a.State == fleet.Working || a.State == fleet.Waiting:
		// Not a queue. A prompt sent to a running turn is re-read BY that turn — it would arrive
		// inside the work they are already doing rather than after it, which is a steer and not a
		// request. It is also what makes the way back readable: the answer is the next turn that
		// finishes over there, and that is only unambiguous if there was no turn open when this
		// arrived.
		return fmt.Sprintf("%s is mid-turn (%s). A request sent now would land inside that work "+
			"rather than after it, so it is not sent — and its answer could not be told apart from "+
			"the answer to what they are already doing. Try them later, or ask somebody else",
			a.Name, fleet.Clip(firstLine(a.Task), 80))
	}
	return ""
}

// Send puts the request into the target's session, under a label saying who it came from.
//
// The label goes on its own line ABOVE the request and the request is copied byte for byte. It was
// two message parts until the wire was checked: the daemon protocol carries one text field and
// joins parts without a separator, so "its own part" would have arrived glued to the first word.
// Every recorded failure of handing work to another agent in this tree began with somebody's words
// arriving altered.
func Send(ctx context.Context, target fleet.Agent, label, request string) error {
	if target.State == fleet.Remote {
		// The guard is at the DIAL and not at the choosing, because choosing a companion on
		// another machine is a legitimate thing to do — it is how work crosses. What must never
		// happen is dialling its socket from here: a socket is a path, and two machines belonging
		// to one person keep their checkouts in the same places, so the path does not fail, it
		// opens whichever LOCAL companion sits at it. The work would arrive in the wrong
		// workspace, looking delivered.
		return fmt.Errorf("%s is on %s; work does not reach another machine through a socket here",
			target.Name, target.Host)
	}
	cl, err := daemon.Dial(target.Socket)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", target.Name, err)
	}
	defer cl.Close()
	return cl.Submit(ctx, command.SubmitPrompt{
		SessionID: session.SessionID(target.Session),
		Parts:     []session.Part{{Kind: session.PartText, Text: Labelled(label, request)}},
	})
}

// Labelled is the one shape a handed-over request takes.
//
// Written down once because there are two senders — this one, dialling a neighbour's socket, and a
// daemon putting into its own session work that arrived over a relay. Both must produce the same
// bytes: the marker the no-chaining rule is read off is in the label, and a receiver whose label
// arrived glued to the first word of the request is a receiver that can pass work on for ever.
func Labelled(label, request string) string { return label + "\n\n" + request }

// StateOf is what can be said about a companion mid-work, for somebody who cannot see it.
//
// The same three answers probeFor gives a local wait, packaged so they can be sent over a wire: is
// the work over with nothing coming, is there news worth passing on, and what is the news.
func StateOf(list []fleet.Agent, sid string) (news string, over bool) {
	for _, a := range list {
		if a.Session != sid {
			continue
		}
		switch a.State {
		case fleet.Abandoned:
			return a.Name + "'s daemon stopped answering with the work unfinished — it was " +
				"killed, crashed, or its machine went away. Nothing will come back. What it had " +
				"done is in its transcript; the rest was not done.", true
		case fleet.Stopped:
			return a.Name + " is no longer running, and it stopped without finishing what you " +
				"handed over. Nothing will come back.", true
		case fleet.Waiting:
			return a.Name + " is blocked waiting for a person: " + a.Asking +
				" — it will not get any further until somebody answers that.", false
		}
		return "", false
	}
	return "the companion that had it is no longer published, so nothing will come back. If it " +
		"finished before it went, the answer is in its transcript.", true
}

// orNone renders an empty team as a phrase rather than a blank.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "you are not in a team"
	}
	return s
}
