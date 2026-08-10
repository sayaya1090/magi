package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Handing work to a companion on another machine.
//
// # It is the same handoff, reached through a door
//
// Nothing about the delivery is different over there: `magi --hand <name>` on the other machine
// runs Target and Send, the two the local tool runs. What crosses is the request and a name, and
// what comes back is the session it landed in. So the mid-turn refusal, the label above the
// request and the definition of "their answer" are one implementation — the thing that would
// otherwise have drifted first.
//
// # The way back is asked for, because it cannot be read
//
// A local wait reads the receiver's log: the answer is written where every answer is written and
// nothing has to be sent. That is the property the whole design rests on, and it does not survive
// the machine boundary — their log is a directory on their disk.
//
// So the same question is ASKED instead, over the same ssh: has a turn finished past the point the
// work landed, and what did they say. One question, two ways of putting it, chosen by where the
// log is. Not a second mechanism with its own idea of what an answer is: the far side computes it
// with the same code the local watch does.
//
// # Nothing is pushed back, deliberately
//
// The receiver does not send anything and does not know it is being waited on. A reply channel
// would need the far machine to reach THIS one, and a laptop that can ssh to a build box is
// routinely not reachable from it. Polling from the side that already proved it can cross is the
// only direction that is always available.

// Cross runs a magi subcommand on another machine and returns what it printed.
//
// A function rather than an exec in here, because this package must not learn how to run processes
// — and because a test can then hand work across a machine boundary that does not exist.
type Cross func(ctx context.Context, host string, args []string, stdin []byte) ([]byte, error)

// crossTimeout bounds one ssh. Long enough for a slow link and a cold connection, short enough that
// a machine that has gone away does not hold a tool call or a probe tick open.
const crossTimeout = 30 * time.Second

// handedThere is what the far side says it did.
type handedThere struct {
	Refused string `json:"refused,omitempty"`
	Name    string `json:"name,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	Session string `json:"session,omitempty"`
	// Since is where their log stood when the work landed. Their number, from their store, because
	// it is their log it indexes — deriving it here would be inventing a position in a file this
	// machine has never read.
	Since int64 `json:"since,omitempty"`
}

// answerThere is what the far side says about the work now.
type answerThere struct {
	Done   bool   `json:"done,omitempty"`
	Answer string `json:"answer,omitempty"`
	News   string `json:"news,omitempty"`
	Over   bool   `json:"over,omitempty"`
}

// handAcross sends the request to another machine and arranges for the answer to be fetched.
func (h Hand) handAcross(ctx context.Context, target fleet.Agent, request string, env port.ToolEnv) session.ToolResult {
	if h.Cross == nil {
		return errText(fmt.Sprintf("%s is on %s and this magi has no way to reach another "+
			"machine. Name somebody here, or do it yourself", target.Name, target.Host))
	}
	if target.Host == "" {
		// A member with no hostname cannot be reached and cannot have been sighted properly. Said
		// rather than attempted, because `ssh "" magi` is a confusing failure.
		return errText(target.Name + " is on a machine that never said its name, so there is no " +
			"way to reach it")
	}
	body, err := json.Marshal(struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Request string `json:"request"`
		Label   string `json:"label"`
	}{From: h.who(), To: target.Name, Request: request, Label: fleet.DispatchedFrom(h.who(), h.Machine)})
	if err != nil {
		return errText("could not put the request on the wire: " + err.Error())
	}

	cctx, cancel := context.WithTimeout(ctx, crossTimeout)
	said, cerr := h.Cross(cctx, target.Host, []string{"--hand"}, body)
	cancel()
	if cerr != nil {
		return errText(fmt.Sprintf("%s did not take it: %v. It needs magi on its PATH and this "+
			"machine needs to be able to `ssh %s`", target.Host, cerr, target.Host))
	}
	var got handedThere
	if jerr := json.Unmarshal(said, &got); jerr != nil {
		return errText(fmt.Sprintf("%s answered with something unreadable: %v", target.Host, jerr))
	}
	if got.Refused != "" {
		// Their refusal, in their words. Not reworded here: the reasons a companion cannot take
		// work are the same sentences the local path produces, and a paraphrase would be a second
		// vocabulary for one set of facts.
		return errText(got.Refused)
	}
	if got.Session == "" {
		return errText(target.Host + " took the work but did not say into which session, so there " +
			"would be no way to bring an answer back. Treat it as not sent")
	}

	// Registered AFTER the crossing, unlike the local path, and for the reason the local path
	// registers before: the wait needs the session and the position their store gave us, and
	// neither exists until the far side has answered. The gap this opens is a receiver that
	// finishes between their Send and this line — which cannot lose the answer, because the answer
	// is fetched by position in their log rather than by watching for an event to go past.
	if xerr := env.Expect(port.Elsewhere{
		Who: target.Name + " on " + target.Host, Session: got.Session, Request: request,
		Answer: h.answerFrom(target, got),
		Probe:  h.probeAcross(target, got),
	}); xerr != nil {
		return errText(fmt.Sprintf("%s has the work, but the answer cannot be waited for here "+
			"(%v) — read their transcript on %s", target.Name, xerr, target.Host))
	}
	where := got.Workdir
	if where == "" {
		where = "their workspace"
	}
	return okText(fmt.Sprintf("Handed to %s on %s, working in %s. Carry on with the rest of your "+
		"task — their answer will arrive here when they finish, quoting what you asked. Do not "+
		"wait for it and do not send it again.", target.Name, target.Host, where))
}

// answerFrom asks the far machine whether the work is finished, and for what was said.
func (h Hand) answerFrom(target fleet.Agent, got handedThere) func() (string, bool) {
	cross := h.Cross
	return func() (string, bool) {
		a, ok := askAcross(cross, target.Host, got)
		if !ok || !a.Done {
			return "", false
		}
		return a.Answer, true
	}
}

// probeAcross asks the far machine whether anybody is still doing the work.
func (h Hand) probeAcross(target fleet.Agent, got handedThere) func() (string, bool) {
	cross := h.Cross
	name := target.Name + " on " + target.Host
	return func() (string, bool) {
		a, ok := askAcross(cross, target.Host, got)
		if !ok {
			// A machine that did not answer is not a machine that lost the work. Saying nothing
			// leaves the wait running, which is right: an ssh fails for a dropped wifi connection
			// far more often than for a companion that has died.
			return "", false
		}
		if a.News == "" {
			return "", false
		}
		return strings.Replace(a.News, target.Name, name, 1), a.Over
	}
}

func askAcross(cross Cross, host string, got handedThere) (answerThere, bool) {
	if cross == nil {
		return answerThere{}, false
	}
	body, err := json.Marshal(struct {
		Session string `json:"session"`
		Since   int64  `json:"since"`
	}{Session: got.Session, Since: got.Since})
	if err != nil {
		return answerThere{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), crossTimeout)
	defer cancel()
	said, err := cross(ctx, host, []string{"--handoff-state"}, body)
	if err != nil {
		return answerThere{}, false
	}
	var a answerThere
	if json.Unmarshal(said, &a) != nil {
		return answerThere{}, false
	}
	return a, true
}
