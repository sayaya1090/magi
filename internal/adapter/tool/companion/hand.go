package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Handing a piece of work to another companion, and getting the answer back.
//
// # What this is not, and why it came back
//
// An earlier version of this — ask_companion — was removed, for one reason precisely: it named its
// recipient as free text with no list of them given anywhere, and its description told the model to
// go and run `companions` first, which is advice and not a mechanism. Asked to "ssh in and do
// something", a model addressed a companion called "ssh", which does not exist. This tree has
// written that shape down before: a set the model is expected to look up rather than be SHOWN is a
// set it guesses at.
//
// Two things are different now and both are mechanisms rather than wording. The roster is in the
// tool's own description, built from the companions actually published when this magi started. And
// an address that matches nobody is refused WITH the roster, so a wrong guess costs one turn and
// ends with the list in front of the model rather than with a request delivered nowhere.
//
// # Asynchronous, which is the whole point
//
// The call returns as soon as the work is handed over. It does not wait, and could not: an MCP
// tool call in this tree is bounded at sixty seconds, and the work being handed over is the kind
// that takes minutes. The asker carries on with the rest of its task and the answer arrives in its
// conversation when the other companion finishes — merged into the turn still running, the way a
// steer is, rather than queued as a new request. See internal/app/handoff.go for the way back.
//
// # magi does not decide who
//
// The model picks. This resolves the address it was given, refuses anything ambiguous, and never
// ranks candidates or takes the closest match. That line is where the previous delegation
// machinery went wrong — it decided, on the model's behalf, how work should be split and who
// should get it — and what is left here is transport plus the refusals that keep a wrong guess
// from becoming a turn in somebody else's workspace.
//
// # The request is not rewritten
//
// The label saying who sent it goes on its own line ABOVE the request, and the request itself is
// copied byte for byte. It was two message parts until the wire was checked: the daemon protocol
// carries one text field and joins parts without a separator, so "its own part" would have arrived
// glued to the first word. Every recorded failure of handing work to another agent in this tree
// began with somebody's words arriving altered.
//
// # It chains exactly one level, and only through a hub
//
// A companion that was asked to do something cannot pass it on — with one exception, because a
// team lead splitting a piece of work across its own team is the reason to have a team lead. So a
// HUB, asked something, may hand parts of it to companions in ITS OWN team. Nobody else may hand
// on anything.
//
// That bounds the depth at two hops without a counter: a member cannot re-dispatch because it is
// not a hub, a hub cannot reach outside its team, and Team is one field, so no chain of hubs can
// form. Read off the transcript rather than tracked in memory, so it survives a restart, an attach
// from elsewhere and a resumed session — three things a counter in this process gets wrong.
type Hand struct {
	Reader    func() fleet.Reader
	ConfigDir string
	Self      string // this companion's socket, so it does not hand work to itself
	Called    string // what this companion is called, for the label the receiver sees
	Team      string // the group it belongs to; a hub may hand work to this team and no other
	Hub       bool   // whether it answers for its team, which is what lets it hand work on at all
	Cache     *fleet.Cache
	// Roster is who was published when this magi started, one line each, for the description.
	// A snapshot, like every other peer list built at startup: a companion that appears later is
	// not in it, and the refusal path reads the LIVE list, so a stale description costs a turn
	// rather than an unreachable companion.
	Roster string
}

// DispatchedBy renders the label that opens a handed-over request. It lives in fleet because the
// view that reads handoffs back off the logs needs the same sentence, and two spellings of one
// marker is one of them silently not matching.
func DispatchedBy(who string) string { return fleet.DispatchedBy(who) }

func (Hand) Name() string { return "hand_off" }

func (h Hand) Description() string {
	who := strings.TrimSpace(h.Roster)
	if who == "" {
		who = "(nobody else is running here right now, so this will refuse)"
	}
	return "Hand a piece of THIS task to another companion and keep working. It returns at once; " +
		"their answer arrives in this conversation when they finish, and you fold it into what you " +
		"have. Do not wait for it and do not ask twice.\n\n" +
		"Who there is:\n" + who + "\n\n" +
		"They do not see your conversation, your files or your reasoning — only the words you " +
		"write here and their own workspace — so write a complete instruction that stands on its " +
		"own. Refused if the name matches nobody or several, if they are mid-turn, or if this turn " +
		"was itself handed to you by somebody else."
}

func (Hand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"to":{"type":"string","description":"which companion, by the name listed in this tool's description"},
		"request":{"type":"string","description":"the whole instruction, standing on its own"}
	},"required":["to","request"],"additionalProperties":false}`)
}

func (h Hand) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		To      string `json:"to"`
		Request string `json:"request"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errText("invalid arguments: " + err.Error()), nil
	}
	in.To, in.Request = strings.TrimSpace(in.To), strings.TrimSpace(in.Request)
	if in.To == "" || in.Request == "" {
		return errText("a request needs somebody to do it and something to do"), nil
	}
	if h.Reader == nil || h.Reader() == nil {
		return errText("this magi cannot see the others, so it cannot hand work to one"), nil
	}
	// Refused BEFORE anything crosses. Without the way back this tool is the old one, and the old
	// one's answer was "they do not report back, read their transcript later" — which is the thing
	// that made handing work over not worth doing.
	if env.Expect == nil {
		return errText("this magi cannot bring an answer back, so handing work over would lose " +
			"it. Do the work here, or ask the person"), nil
	}
	relaying := wasDispatched(ctx, h.Reader(), env.SessionID)
	if relaying && !h.Hub {
		return errText("this turn was itself asked for by another companion, and a request is not " +
			"passed along. Do the part you can and say plainly in your answer what you could not " +
			"do and who you think should — the one who asked will read it."), nil
	}

	list, err := fleet.ListCached(ctx, h.Reader(), h.ConfigDir, h.Self, h.Cache)
	if err != nil {
		return errText("cannot read the published companions: " + err.Error()), nil
	}
	found := fleet.Resolve(list, in.To)
	switch len(found) {
	case 0:
		return errText(fmt.Sprintf("nobody here is called %q or does that. There is: %s",
			in.To, fleet.Roster(list))), nil
	case 1:
	default:
		return errText(fmt.Sprintf("%q matches %s — name one of them. Sending work to the wrong "+
			"workspace is not something to guess at", in.To, fleet.Names(found))), nil
	}
	target := found[0]
	switch {
	case relaying && (h.Team == "" || !strings.EqualFold(target.Team, h.Team)):
		return errText(fmt.Sprintf("this was asked of you by somebody else, so you can only pass "+
			"parts of it to your own team (%s). %s is not in it — answer with what you could do and "+
			"say who should do the rest", orNone(h.Team), target.Name)), nil
	case target.Here:
		return errText("that is you. Do it yourself, or name somebody else"), nil
	case target.State == fleet.Remote:
		// Refused BEFORE the dial, and this is not caution. A socket is a path, and two machines
		// belonging to one person keep their checkouts in the same places — so dialling a remote
		// companion's socket path here does not fail, it opens whichever local companion happens
		// to sit at that path. The work would arrive, in the wrong workspace, looking delivered.
		return errText(fmt.Sprintf("%s is on %s, another machine. Work is handed over a socket on "+
			"this filesystem and there is no way across yet, so this cannot be sent. Name somebody "+
			"here, or do it yourself", target.Name, target.Host)), nil
	case !target.Live:
		return errText(fmt.Sprintf("%s is not running, so there is nothing to hand the work to",
			target.Name)), nil
	case target.State == fleet.Working || target.State == fleet.Waiting:
		// Not a queue. A prompt sent to a running turn is re-read BY that turn — it would arrive
		// inside the work they are already doing rather than after it, which is a steer and not a
		// request. It is also what makes the way back readable: the answer is the next turn that
		// finishes over there, and that is only unambiguous if there was no turn open when this
		// arrived.
		return errText(fmt.Sprintf("%s is mid-turn (%s). A request sent now would land inside that "+
			"work rather than after it, so it is not sent — and its answer could not be told apart "+
			"from the answer to what they are already doing. Try them later, or ask somebody else",
			target.Name, fleet.Clip(firstLine(target.Task), 80))), nil
	}

	cl, derr := daemon.Dial(target.Socket)
	if derr != nil {
		return errText("cannot reach " + target.Name + ": " + derr.Error()), nil
	}
	defer cl.Close()

	// The watch is registered BEFORE the work is sent. The other way round, a peer quick enough to
	// finish in the gap would have its turn already closed by the time anybody looked, and the
	// watch would then sit waiting for the turn after it — an answer that never comes, about work
	// that was done.
	if xerr := env.Expect(port.Elsewhere{
		Who: target.Name, Session: target.Session, Request: in.Request,
		Probe: h.probeFor(target.Socket, target.Name),
	}); xerr != nil {
		return errText("nothing was sent: the answer could not be waited for (" + xerr.Error() +
			"), and handing work over without that loses it"), nil
	}
	if serr := cl.Submit(ctx, command.SubmitPrompt{
		SessionID: session.SessionID(target.Session),
		Parts:     []session.Part{{Kind: session.PartText, Text: DispatchedBy(h.who()) + "\n\n" + in.Request}},
	}); serr != nil {
		// The watch is left in place. It costs one goroutine that will time out, and the
		// alternative — a way to cancel it — is a second mechanism for the sake of a failed dial.
		return errText("could not hand it to " + target.Name + ": " + serr.Error()), nil
	}
	return okText(fmt.Sprintf("Handed to %s, working in %s. Carry on with the rest of your task — "+
		"their answer will arrive here when they finish, quoting what you asked. Do not wait for "+
		"it and do not send it again.", target.Name, target.Workdir)), nil
}

// who is what the label says. A companion that declared no name is identified by its workspace,
// because "asked by another companion" with no way to tell which one is worse than a long path.
func (h Hand) who() string {
	if n := strings.TrimSpace(h.Called); n != "" {
		return n
	}
	if h.Self != "" {
		return "the companion at " + h.Self
	}
	return "another companion"
}

// wasDispatched reports whether this session's last request came from another companion.
//
// Read off the transcript rather than tracked in memory: the marker is in the log, so the rule
// survives a restart, an attach from elsewhere, and a resumed session — all three of which a
// counter held in this process would get wrong.
func wasDispatched(ctx context.Context, r fleet.Reader, sid session.SessionID) bool {
	msgs, _, err := r.SessionState(ctx, sid)
	if err != nil {
		// Unreadable log: refuse to chain rather than assume it is safe. The cost of being wrong
		// the cautious way is one refusal a person can override by asking directly.
		return true
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != session.RoleUser {
			continue
		}
		for _, p := range msgs[i].Parts {
			if strings.Contains(p.Text, fleet.DispatchMark) {
				return true
			}
		}
		return false // the most recent request is this session's own
	}
	return false
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "you are not in a team"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func okText(msg string) session.ToolResult {
	return session.ToolResult{Content: mustJSON(msg)}
}

// probeFor answers, later and from another goroutine, whether anybody is still doing the work.
//
// The engine that waits cannot ask this itself: whether a process is alive and whether it is
// blocked on a person are a dial and a question over a socket, and internal/app cannot import the
// packages that own those without closing a cycle. So the answer is supplied from here, where they
// are already in hand, as a closure the wait calls on its own clock.
//
// Two states end the wait and they are not the same news:
//
//   - The daemon is gone with the turn unfinished. Nobody is coming, and saying so beats two hours
//     of silence followed by "not finished", which is equally true of a peer still working.
//   - It finished a turn but not one this could be. Only reachable if the peer restarted onto a
//     different session; the request went to a log nothing is driving any more.
//
// Being BLOCKED is news and not an ending: a person may still answer it. It is worth saying at once
// because the asker is the only thing in a position to tell one.
//
// Never guesses. A probe that cannot reach the roster says nothing, and the wait continues — a
// companion reported dead because a listing failed is worse than one reported late.
func (h Hand) probeFor(socket, name string) func() (string, bool) {
	reader, cfgDir, self, cache := h.Reader, h.ConfigDir, h.Self, h.Cache
	return func() (string, bool) {
		if reader == nil || reader() == nil {
			return "", false
		}
		list, err := fleet.ListCached(context.Background(), reader(), cfgDir, self, cache)
		if err != nil {
			return "", false
		}
		for _, a := range list {
			if a.Socket != socket {
				continue
			}
			switch a.State {
			case fleet.Abandoned:
				return name + "'s daemon stopped answering with the work unfinished — it was " +
					"killed, crashed, or its machine went away. Nothing will come back. What it " +
					"had done is in its transcript; the rest was not done.", true
			case fleet.Stopped:
				return name + " is no longer running, and it stopped without finishing what you " +
					"handed over. Nothing will come back.", true
			case fleet.Waiting:
				return name + " is blocked waiting for a person: " + a.Asking +
					" — it will not get any further until somebody answers that.", false
			}
			return "", false
		}
		// Not in the listing at all: its record is gone, which is what stopping a companion does.
		return name + " is no longer published here, so nothing will come back. If it finished " +
			"before it went, the answer is in its transcript.", true
	}
}
