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
	// Machine is what this companion's host is called, for the label a receiver on another machine
	// reads. Empty on a machine that cannot say its own name, which reads as "somewhere else" —
	// vague, and still better than a label claiming they are neighbours.
	Machine string
	// Reach opens the daemon protocol to a companion on another machine. nil on a magi with no way
	// to, and hand_off then refuses a target elsewhere rather than pretending the cluster is one
	// filesystem.
	Reach Reach
	// Roster is who there is, one line each, asked for every time the description is built.
	//
	// A function and not a string, because a description is rebuilt on every step and this list
	// was taken once at startup. A daemon that came up before its cluster had converged advertised
	// an empty list and went on advertising it: asked to hand work to a companion that joined a
	// minute later, the model answered that no such companion exists. It had never been shown one
	// — and a set a model is expected to look up rather than be SHOWN is a set it guesses at,
	// which is the entire reason this description carries a list.
	//
	// Observed across five machines, not reasoned about.
	//
	// It must not block: whatever supplies it decides how fresh is fresh, and reading the fleet
	// means dialling every published socket. See cmd/magi/roster.go.
	//
	// It reports how many companions the lines name as well as the lines. The count is what says
	// whether there is a CHOICE here, and it cannot be recovered from the text.
	Roster func() (lines string, n int)
	// Record is how work handed over before turned out, as this workspace judged it.
	//
	// Shown, never applied. It is a block of its own below the roster rather than a column inside
	// it, because a line that carries a score reads as an ordering however it is worded — and
	// nothing here reorders or filters anybody. The model picks; this is one more fact in front
	// of it, which is the same relationship the roster has to the choice.
	Record func() string
}

func (Hand) Name() string { return "hand_off" }

func (h Hand) Description() string {
	who, n := "", 0
	if h.Roster != nil {
		lines, count := h.Roster()
		who, n = strings.TrimSpace(lines), count
	}
	if who == "" {
		who = "(nobody else is running here right now, so this will refuse)"
	}
	// Only where there is something to choose BETWEEN. With one companion the record informs
	// nothing and is a paragraph in every prompt of every step; the weight of what a model reads
	// while deciding is a cost this tree has measured.
	past := ""
	if n > 1 && h.Record != nil {
		if r := strings.TrimSpace(h.Record()); r != "" {
			past = "\n" + r + "\n"
		}
	}
	return "Hand a piece of THIS task to another companion and keep working. It returns at once; " +
		"their answer arrives in this conversation when they finish, and you fold it into what you " +
		"have. Do not wait for it and do not ask twice.\n\n" +
		"Who there is:\n" + who + "\n" + past + "\n" +
		"They do not see your conversation, your files or your reasoning — only the words you " +
		"write here and their own workspace — so write a complete instruction that stands on its " +
		"own. Refused if the name matches nobody or several, if they are mid-turn, or if this turn " +
		"was itself handed to you by somebody else.\n\n" +
		"When their answer arrives, QUOTE IT AS IT STANDS. You will not be able to see the " +
		"workspace it came from, so you cannot tell which detail is load-bearing — a version, a " +
		"path, a flag, an error string. Leave whole parts out if you must, and say you did; never " +
		"reword what you keep. The answer arrives with a digest of itself so a quote can be checked."
}

func (Hand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"to":{"type":"string","description":"which companion, by the name listed in this tool's description"},
		"request":{"type":"string","description":"the whole instruction, standing on its own"},
		"so_that":{"type":"string","description":"what this is for — the thing you will do with their answer. One clause. It is what lets them adapt when they hit something you did not foresee, which they cannot ask you about."},
		"answer_as":{"type":"string","description":"the form the answer must come back in: write out the headings or fields you want filled, in the order you will read them. They fill it in. Not how to do the work — what the finished answer looks like. For example: \"- token name:\\n- hex value:\\n- contrast ratio, or why you could not measure it:\""},
		"looking":{"type":"boolean","description":"true when this is a QUESTION about their workspace and nothing in it should change. They answer it with the four tools that only read, and — because such a turn cannot collide with anything — they can answer it while they are busy with something else. Set it whenever you are asking rather than asking for work: it is the difference between an answer now and an answer after their current turn."}
	},"required":["to","request","so_that","answer_as"],"additionalProperties":false}`)
}

// fillable reports whether a form has anywhere to put an answer.
//
// It catches a TOKEN, not a bad form. Live, twice out of two, a model satisfied this field with
// the word "text" — the requirement was met and nothing was said. A single word cannot be filled
// in, and it cannot be checked against either: the note that delivers the answer quotes the form
// and tells the reader to compare, which pointed at "text".
//
// Deliberately no further than that. Whether a form is a GOOD one — the right headings, in the
// right order, for this piece of work — is a judgement about the task, and magi making it would be
// one more thing to be wrong about while looking authoritative. What it can say is that one word
// is not a form.
func fillable(form string) bool {
	if strings.ContainsAny(form, "\n\r") {
		return true
	}
	return len(strings.Fields(form)) > 1
}

// withForm puts the form after the request, in the asker's words, and tells the receiver what to
// do with it.
//
// # A form and not a sentence about being finished
//
// "Done when the tokens are named" is something the asker checks afterwards, against prose, by
// reading carefully. A form is checked by looking: the headings are either filled or they are not.
// And it changes what a gap looks like — a part that could not be done comes back AS that part,
// said so, instead of as a paragraph explaining why the whole thing is hard. Observed: a companion
// asked for tokens spent a round trip answering "there are no files here, point me at them", where
// the same gap under a heading would have named itself and left the rest of the answer standing.
//
// # Carried in the text, deliberately
//
// It has to reach a companion on another machine, where the only things that cross are a label and
// a request. Folding it into the request means both paths carry it by construction — there is no
// version of magi that takes the work and drops the form.
// asked is one request as it goes out: what they were asked, the form the answer must take, and
// the two composed into the words that actually cross. Kept apart because the quote-back beside
// their answer shows them separately — the check is "does this fill that", and a single blob makes
// the reader do the splitting.
type asked struct {
	Request string
	Purpose string
	Form    string
	// Looking is the asker saying this is a question: nothing in the receiver's workspace should
	// change to answer it. What it buys is when rather than what — a question can be answered
	// while its receiver is busy, because a turn that cannot write has nothing to wait for.
	Looking bool
}

// text is the words that cross: the task, then what it is for, then the form.
//
// That order is the mission-statement convention — the task and its purpose together, end state
// after — and it is not decoration. A doer who knows what the answer is FOR can substitute
// something sensible when the ground turns out not to match the request; one who does not can only
// stop. It matters here more than in an office for the reason everything else here does: there is
// no reply channel, so "I would have asked" is not available to them.
func (a asked) text() string {
	out := a.Request
	if p := strings.TrimSpace(a.Purpose); p != "" {
		out += "\n\nIn order to: " + p
	}
	return withForm(out, a.Form)
}

func withForm(request, form string) string {
	if strings.TrimSpace(form) == "" {
		return request
	}
	return request + "\n\nAnswer in this form, filling it in. If a part cannot be done, keep the " +
		"part and say so under it rather than leaving it out — a gap with a name is something the " +
		"asker can act on, and a missing section is not:\n\n" + form
}

func (h Hand) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		To       string `json:"to"`
		Request  string `json:"request"`
		SoThat   string `json:"so_that"`
		AnswerAs string `json:"answer_as"`
		Looking  bool   `json:"looking"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errText("invalid arguments: " + err.Error()), nil
	}
	in.To, in.Request = strings.TrimSpace(in.To), strings.TrimSpace(in.Request)
	in.SoThat, in.AnswerAs = strings.TrimSpace(in.SoThat), strings.TrimSpace(in.AnswerAs)
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

	target, refused := Target(ctx, h.Reader(), h.Cache, h.ConfigDir, h.Self, in.To)
	if refused != "" {
		return errText(refused), nil
	}
	if relaying && (h.Team == "" || !strings.EqualFold(target.Team, h.Team)) {
		return errText(fmt.Sprintf("this was asked of you by somebody else, so you can only pass "+
			"parts of it to your own team (%s). %s is not in it — answer with what you could do and "+
			"say who should do the rest", orNone(h.Team), target.Name)), nil
	}
	// Last of the refusals, on purpose. The ones above are about whether this can happen at all —
	// you may not pass work on, nobody is called that, they are mid-turn — and a model told to
	// write a form when the real answer is "not to them" spends a turn on the wrong repair.
	//
	// Required, and it is the item this tree kept losing. Whether an answer ARRIVED is already
	// known: the wait says so. Whether it was the THING is a comparison, and without a form there
	// is nothing to compare it against — an answer comes back, every gate is satisfied, and nobody
	// has checked. It matters more here than in an office, because a companion on another machine
	// cannot knock on the door and ask what you meant.
	// Both at once when both are missing. Two refusals in a row for one badly-formed request is
	// two turns spent learning what one sentence could have said.
	var missing []string
	if in.SoThat == "" {
		missing = append(missing, "what this is FOR — the thing you will do with their answer. It "+
			"is what lets them adapt if the ground turns out not to match your request, and they "+
			"cannot ask you")
	}
	switch {
	case in.AnswerAs == "":
		missing = append(missing, "the FORM the answer should come back in — the headings or "+
			"fields you will read. An unstated shape is one they have to guess at, and a guess "+
			"that comes back looking like an answer is the expensive kind of wrong")
	case !fillable(in.AnswerAs):
		missing = append(missing, fmt.Sprintf("a FORM rather than the word %q — something with "+
			"places in it for the answer to go, like \"- token name:\\n- hex value:\\n- contrast "+
			"ratio, or why you could not measure it:\". A single word is not something anybody can "+
			"fill in, and it is not something you can check an answer against either", in.AnswerAs))
	}
	if len(missing) > 0 {
		return errText("nothing was sent. They cannot ask you what you meant — there is no reply " +
			"channel — so this needs " + strings.Join(missing, "; and ")), nil
	}
	// One door, whoever is on the other side of it.
	//
	// There used to be two: a neighbour was dialled and its session submitted to directly, while a
	// companion on another machine went through the daemon protocol. The second one is the one
	// that was right — the daemon decides what happens to work handed to it, because it is the
	// only thing that knows what it is already doing — and dial-or-relay is settled beneath this
	// by what is published at the socket, not here by a state on a row. See cmd/magi/hand.go.
	//
	// What went with the local path: a Send that put a prompt straight into somebody else's
	// session, a probe that inferred their liveness from a fleet listing, and a watch registered
	// before the work was sent because a fast peer could finish in the gap. None of them have a
	// job any more. The receipt is minted inside the receiving daemon, from a position it takes
	// before it starts, so there is no gap to lose an answer in.
	return h.handAcross(ctx, target, asked{
		Request: in.Request, Purpose: in.SoThat, Form: in.AnswerAs, Looking: in.Looking}, env), nil
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

func okText(msg string) session.ToolResult {
	return session.ToolResult{Content: mustJSON(msg)}
}
