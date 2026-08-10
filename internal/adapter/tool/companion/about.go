package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Asking what another companion can actually do.
//
// # Why there is a tool for this at all
//
// The roster names capabilities and stops there: `builder [core] — builds and tests · can:
// run-tests, bisect, profile (+2)`. That is enough to pick somebody out of a list and not enough to
// write them a request, because a name is not a contract — "bisect" could be git history or a
// failing test case, and handing over the wrong one costs a turn on both machines.
//
// The names travel with every sighting because a roster has to be readable without asking anybody.
// The descriptions do not, because they would be a hundred bytes per skill per member on every
// exchange every minute. So they are fetched, once, by whoever wants to know — which is this.
//
// # One question wherever the answer lives
//
// A companion on this machine already has an `about` behind its MCP door. A companion on another
// machine has no door here at all, and attaching one over ssh for the life of a daemon would be a
// held-open connection per member that dies with the first dropped link.
//
// So this asks the same question either way and lets the wiring decide how: in-process for a
// neighbour, one short ssh for a companion elsewhere. What comes back is rendered by one function
// (mcpserve.Describe) on whichever machine holds the workspace, so the answer does not read
// differently depending on which side of a network the reader is on.
type About struct {
	Reader    func() fleet.Reader
	ConfigDir string
	Self      string // this companion's socket, so it does not describe itself back
	Cache     *fleet.Cache
	// Ask renders a companion's description, wherever it is. Injected because reaching another
	// machine means running a process, which this package does not do.
	//
	// It takes no name. The host and socket are an address, and whatever answers at that address is
	// the companion — which is the property the relay bought: nothing on the far side has to
	// resolve a name against a config directory it may not be able to read.
	Ask func(ctx context.Context, host, socket string) (string, error)
}

func (About) Name() string { return "companion_can" }

func (a About) Description() string {
	return "Ask what another companion can actually do, before handing it work. The roster in " +
		"hand_off names their capabilities; this says what each name means, in the words of " +
		"whoever wrote it, and works the same whether they are on this machine or another one.\n\n" +
		"Worth one call before a request you are unsure about. Not worth a call to confirm " +
		"something the roster line already told you."
}

func (About) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"who":{"type":"string","description":"which companion, by the name listed in hand_off's description"}
	},"required":["who"],"additionalProperties":false}`)
}

func (a About) Execute(ctx context.Context, args json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Who string `json:"who"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errText("invalid arguments: " + err.Error()), nil
	}
	in.Who = strings.TrimSpace(in.Who)
	if in.Who == "" {
		return errText("name a companion"), nil
	}
	if a.Reader == nil || a.Reader() == nil || a.Ask == nil {
		return errText("this magi cannot see the other companions, so it cannot describe one"), nil
	}
	// Resolved the same way work is addressed, so a name that describes here is a name that can be
	// handed to. Two resolvers would let a model read about one companion and send to another.
	list, err := fleet.ListCached(ctx, a.Reader(), a.ConfigDir, a.Self, a.Cache)
	if err != nil {
		return errText("cannot read the published companions: " + err.Error()), nil
	}
	found := fleet.Resolve(list, in.Who)
	switch len(found) {
	case 0:
		return errText(fmt.Sprintf("nobody is called %q or does that. There is: %s",
			in.Who, fleet.Roster(list))), nil
	case 1:
	default:
		return errText(fmt.Sprintf("%q matches %s — name one of them",
			in.Who, fleet.Names(found))), nil
	}
	target := found[0]
	said, aerr := a.Ask(ctx, target.Host, target.Socket)
	if aerr != nil {
		// Named rather than swallowed. A companion that cannot be described is one that probably
		// cannot be handed work either, and saying which machine did not answer is what lets the
		// model decide to ask somebody else instead of trying anyway.
		where := target.Host
		if where == "" {
			where = "this machine"
		}
		return errText(fmt.Sprintf("could not get a description of %s from %s: %v",
			target.Name, where, aerr)), nil
	}
	if strings.TrimSpace(said) == "" {
		return errText(target.Name + " answered with nothing to say about itself"), nil
	}
	return okText(said), nil
}
