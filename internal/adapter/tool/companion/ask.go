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

// Handing work to another companion.
//
// # No gateway
//
// Every companion on a machine already knows the others: they publish to one directory and anybody
// can read it. So a request goes straight to the daemon that will do it — no registry to run, no
// broker to be down, nothing in the middle to keep in step with the thing it fronts. The console is
// a viewer of the same records, not a hop.
//
// # magi does not decide who
//
// The model picks, from the roster the companions tool prints. This resolves the address it was
// given and refuses anything ambiguous; it never ranks candidates and never picks the closest
// match. That line is where the previous delegation machinery went wrong — it decided, on the
// model's behalf, how work should be split and who should get it — and the whole of what is left
// here is transport plus the refusals that keep a wrong guess from becoming a turn in somebody
// else's workspace.
//
// # The request is not rewritten
//
// The label saying who sent it goes on its own line ABOVE the request, and the request itself is
// copied byte for byte. It was two message parts until the wire was checked: the daemon protocol
// carries one text field and joins parts without a separator, so "its own part" would have arrived
// glued to the first word of the request. Every recorded failure of handing work to another agent
// in this tree began with somebody's words arriving altered, so this is asserted by equality on the
// whole message — label, blank line, request — rather than by "contains".
//
// # It chains exactly one level, and only through a hub
//
// A companion that was asked to do something cannot pass it on — with one exception, because a
// team lead splitting a piece of work across its own team is the reason to have a team lead. So:
// a HUB, asked something, may hand parts of it to companions in ITS OWN team. Nobody else may
// hand on anything.
//
// That bounds the depth at two hops without a counter. A member cannot re-dispatch because it is
// not a hub; a hub cannot reach outside its team, and Team is one field, so no chain of hubs can
// form. Read off the transcript rather than tracked in memory, so it survives a restart, an attach
// from elsewhere and a resumed session — three things a counter in this process gets wrong.
//
// What a plain member does instead of passing work on: answer with the part it could do and say
// what it could not, and who it thinks should. The person or hub that asked reads that.
type Ask struct {
	Reader    func() fleet.Reader
	ConfigDir string
	Self      string // this companion's socket, so it does not hand work to itself
	Called    string // what this companion is called, for the label the receiver sees
	Team      string // the group it belongs to; a hub may hand work to this team and no other
	Hub       bool   // whether it answers for its team, which is what lets it hand work on at all
	Cache     *fleet.Cache
}

// DispatchedBy renders the label that opens a handed-over request. It lives in fleet because the
// view that reads handoffs back off the logs needs the same sentence, and two spellings of one
// marker is one of them silently not matching.
func DispatchedBy(who string) string { return fleet.DispatchedBy(who) }

func (Ask) Name() string { return "ask_companion" }

func (Ask) Description() string {
	return "Hand a piece of work to another magi on this machine — use `companions` first to see " +
		"who there is and what each does. The request is delivered exactly as you write it, so " +
		"write it as a complete instruction: they do not see your conversation, your files or your " +
		"reasoning, only these words and their own workspace. They answer in their own transcript; " +
		"read it back with `companions`, which shows what each one is doing and the last thing it " +
		"said. Refused if the address matches nobody or several, if they are mid-turn, or if this " +
		"turn was itself asked for by somebody else."
}

func (Ask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"to":{"type":"string","description":"who: the name a companion goes by, or words from what it does"},
		"request":{"type":"string","description":"the whole instruction, standing on its own"}
	},"required":["to","request"],"additionalProperties":false}`)
}

func (a Ask) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
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
	if a.Reader == nil || a.Reader() == nil {
		return errText("this magi cannot see the others, so it cannot hand work to one"), nil
	}
	relaying := wasDispatched(ctx, a.Reader(), env.SessionID)
	if relaying && !a.Hub {
		return errText("this turn was itself asked for by another companion, and a request is not " +
			"passed along. Do the part you can and say plainly in your answer what you could not " +
			"do and who you think should — the person who asked will read it."), nil
	}

	list, err := fleet.ListCached(ctx, a.Reader(), a.ConfigDir, a.Self, a.Cache)
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
	case relaying && (a.Team == "" || !strings.EqualFold(target.Team, a.Team)):
		// A hub splitting work across its own team is the reason to have a hub. Reaching outside it
		// while relaying is how two hubs would form a chain, and a chain is the thing nobody can
		// see the shape of.
		return errText(fmt.Sprintf("this was asked of you by somebody else, so you can only pass "+
			"parts of it to your own team (%s). %s is not in it — answer with what you could do and "+
			"say who should do the rest", orNone(a.Team), target.Name)), nil
	case target.Here:
		return errText("that is you. Do it yourself, or name somebody else"), nil
	case !target.Live:
		return errText(fmt.Sprintf("%s is not running, so there is nothing to hand the work to",
			target.Name)), nil
	case target.State == fleet.Working || target.State == fleet.Waiting:
		// Not a queue. A prompt sent to a running turn is re-read BY that turn — it would arrive
		// inside the work they are already doing rather than after it, which is a steer and not a
		// request. Saying so beats derailing somebody.
		return errText(fmt.Sprintf("%s is mid-turn (%s). A request sent now would land inside that "+
			"work rather than after it, so it is not sent. Check `companions` again when they are "+
			"idle, or ask somebody else", target.Name, fleet.Clip(firstLine(target.Task), 80))), nil
	}

	cl, derr := daemon.Dial(target.Socket)
	if derr != nil {
		return errText("cannot reach " + target.Name + ": " + derr.Error()), nil
	}
	defer cl.Close()
	if serr := cl.Submit(ctx, command.SubmitPrompt{
		SessionID: session.SessionID(target.Session),
		Parts:     []session.Part{{Kind: session.PartText, Text: DispatchedBy(a.who()) + "\n\n" + in.Request}},
	}); serr != nil {
		return errText("could not hand it to " + target.Name + ": " + serr.Error()), nil
	}
	return okText(fmt.Sprintf("handed to %s. They are working on it in %s; they do not report back, "+
		"so read their answer with `companions` when they go idle — the listing carries the last "+
		"thing each one said.", target.Name, target.Workdir)), nil
}

// who is what the label says. A companion that declared no name is identified by its workspace,
// because "asked by another companion" with no way to tell which one is worse than a long path.
func (a Ask) who() string {
	if n := strings.TrimSpace(a.Called); n != "" {
		return n
	}
	if a.Self != "" {
		return "the companion at " + a.Self
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
	return session.ToolResult{Content: json.RawMessage(mustJSON(msg))}
}
