package app

import (
	"context"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/meeting"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// One companion's turn in a meeting: read what has been said, and add to it or pass.
//
// # Why a session of its own
//
// The companion may be mid-turn on its own work — that is the ordinary case, and a meeting nobody
// can attend because everybody is busy is a meeting that never happens. A child session has its
// own context and its own log, so taking part costs the working session nothing and leaves its
// plan, its history and its unfinished turn exactly as they were.
//
// # Why read-only, enforced here
//
// A meeting decides; it does not do. Three companions editing three workspaces while the plan is
// still being argued about is the failure this separation exists to prevent, so the child gets the
// four tools that look and nothing else — not by asking it nicely in the prompt, which this tree
// has watched evaporate under pressure, but by the allowlist it is spawned with.
//
// # What comes back
//
// What it said, or a pass. A pass is a first-class answer: a companion whose workspace has nothing
// to do with the question should say so in one line and cost one turn, and a prompt that demanded
// a contribution would get "I agree with what design said" instead — which is the sentence that
// makes people stop reading meeting records.
func (a *App) MeetingTurn(ctx context.Context, sid session.SessionID, who, topic, transcript string,
	closing bool) (meeting.Utterance, error) {
	s := a.sessionInfo(ctx, sid)
	ask := meetingPrompt(who, topic, transcript, closing)
	res, err := a.spawnChild(ctx, s, event.Actor{Kind: event.ActorUser, ID: "meeting"}, port.SpawnSpec{
		ToolName: "meeting",
		System: "You are " + who + ", taking part in a meeting between companions. You each work in " +
			"a different workspace and know different things; that is why you are all here. Read " +
			"your own files when you need to check something. You cannot change anything: this is " +
			"a discussion, and the work is handed out afterwards.",
		Prompt: ask,
		// The four that look. Not advice in the prompt — the allowlist is what makes it true.
		Tools:    []string{"read", "glob", "grep", "list"},
		MaxSteps: meetingSteps,
	}, nil)
	if err != nil {
		return meeting.Utterance{}, err
	}
	return readUtterance(who, res.Text), nil
}

// meetingSteps bounds one contribution. A participant that needs more than a few looks at its own
// files to say what it thinks is a participant writing the work rather than discussing it.
const meetingSteps = 8

// meetingPrompt is what a participant is given: the question, everything said so far, and the two
// shapes an answer may take.
//
// The transcript is whole rather than summarised. A meeting where the fourth speaker reads a
// précis of the first three is a meeting whose one advantage — that each speaker read what came
// before — has been thrown away.
func meetingPrompt(who, topic, transcript string, closing bool) string {
	var b strings.Builder
	b.WriteString("THE QUESTION\n" + strings.TrimSpace(topic) + "\n\n")
	if strings.TrimSpace(transcript) == "" {
		b.WriteString("Nobody has spoken yet. You are first.\n\n")
	} else {
		b.WriteString("WHAT HAS BEEN SAID\n" + strings.TrimSpace(transcript) + "\n\n")
	}
	if closing {
		b.WriteString("HOW TO ANSWER\n" +
			"The discussion is over. Say what YOU will do about this, in one or two lines, as work " +
			"you would start in your own workspace. If there is nothing for you to do, answer with " +
			"exactly PASS — that is a normal outcome and not a failure to contribute.\n")
		return b.String()
	}
	b.WriteString("HOW TO ANSWER\n" +
		"Add what only you can add: what your workspace makes true, what breaks, what it costs. " +
		"Answer the others by name where you disagree with them. Two or three sentences.\n" +
		"If you have nothing to add — the question does not touch what you work on, or somebody " +
		"has already said what you would say — answer with PASS, and one short line saying why if " +
		"there is one. Passing is a normal answer here.\n" +
		// The one thing the participants could not know, and the reason a meeting needs no length
		// set in advance: they are the ones who end it. Without this a model treats a pass as
		// abstaining from a discussion that will go on regardless, so it keeps finding something
		// to say and the round cap becomes what stops the room — a number nobody chose on purpose
		// deciding when a question has been answered.
		"THE MEETING ENDS WHEN NOBODY HAS ANYTHING LEFT TO ADD. A pass is how you say that, so " +
		"pass as soon as it is true rather than filling a turn.\n" +
		"Do not summarise what others said. Do not agree in order to have spoken.\n")
	return b.String()
}

// readUtterance turns what came back into a contribution or a pass.
//
// One keyword, at the start, because that is what a model can be relied on to do — and a reply
// that merely CONTAINS the word (\"I would not pass on this\") is a contribution, which is why the
// check is on the first word rather than anywhere in the text.
func readUtterance(who, text string) meeting.Utterance {
	said := strings.TrimSpace(text)
	first := said
	if i := strings.IndexAny(first, " \n\t:.—-"); i > 0 {
		first = first[:i]
	}
	if strings.EqualFold(strings.TrimSpace(first), "PASS") {
		reason := strings.TrimSpace(strings.TrimLeft(said[len(first):], " \n\t:.—-"))
		return meeting.Utterance{Who: who, Pass: true, Text: reason}
	}
	if said == "" {
		// Nothing at all is a pass with nothing to say, not an error: the turn happened, the
		// participant produced no words, and inventing a contribution for it would be worse.
		return meeting.Utterance{Who: who, Pass: true}
	}
	return meeting.Utterance{Who: who, Text: said}
}

// MeetingTask asks a participant what it will do, once the discussion has closed.
func (a *App) MeetingTask(ctx context.Context, sid session.SessionID, who, topic, transcript string) (
	meeting.Task, error) {
	u, err := a.MeetingTurn(ctx, sid, who, topic, transcript, true)
	if err != nil {
		return meeting.Task{}, err
	}
	if u.Pass {
		return meeting.Task{Who: who}, nil
	}
	return meeting.Task{Who: who, What: u.Text}, nil
}
