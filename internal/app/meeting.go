package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/meeting"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// meetingLook is the allowlist a meeting participant is spawned with, and it is the whole of
// "a meeting decides; it does not do". Three companions editing three workspaces while the plan is
// still being argued about is the failure that separation exists to prevent, so a participant gets
// the four tools that look and nothing else — not by being asked nicely in the prompt, which this
// tree has watched evaporate under pressure, but by the list it is given.
//
// So the allowlist has to be ONE list. It was written out at all three spawn sites, where a fourth
// tool added to one of them would have given a participant the ability to write while every
// comment in this file still said it could not.
var meetingLook = []string{"read", "glob", "grep", "list"}

// meetingSteps bounds one contribution. A participant that needs more than a few looks at its own
// files to say what it thinks is a participant writing the work rather than discussing it.
const meetingSteps = 8

// MeetingPrepare gives a participant its own session for this meeting and a chance to get ready.
//
// # Why a session of its own
//
// The companion may be mid-turn on its own work — that is the ordinary case, and a meeting nobody
// can attend because everybody is busy is a meeting that never happens. A child session has its
// own context and its own log, so taking part costs the working session nothing and leaves its
// plan, its history and its unfinished turn exactly as they were.
//
// # One session per participant, not one per turn
//
// Every contribution used to be a fresh child: a new session, a new context, everything the
// companion had already read thrown away and read again. Three companions over five rounds was
// fifteen children on the strip and fifteen cold starts — most of why a meeting with three agents
// crawls, and why the same participant could contradict what it had said two turns earlier
// without noticing. The session made here is reused for every turn of this meeting (see
// MeetingSayIn), so a participant remembers its own reading and its own words.
//
// # Why there is a preparation turn at all
//
// A meeting where everybody arrives cold spends its first two rounds looking things up out loud.
// The companion is asked to look BEFORE the room opens: what its working tree says, what its
// recent history says, what its workspace makes true about the question. Nobody hears this turn —
// what comes back is a readiness note for the screen.
//
// # Why git arrives as evidence rather than as a tool
//
// The participant keeps the four tools that look and nothing else. Git is read FOR it and handed
// over in the prompt: a companion that could run git could run anything, and the meeting's whole
// separation is that it decides and does not do. What it cannot get this way it can still read
// out of its own files.
func (a *App) MeetingPrepare(ctx context.Context, sid session.SessionID, who, topic string,
	room []meeting.Seat) (
	session.SessionID, string, error) {
	s := a.sessionInfo(ctx, sid)
	res, err := a.spawnChild(ctx, s, event.Actor{Kind: event.ActorUser, ID: meeting.Origin}, port.SpawnSpec{
		ToolName: "meeting",
		System:   meetingSystem(who),
		Prompt:   preparePrompt(who, topic, a.workNow(ctx, s.Workdir), room),
		Tools:    meetingLook,
		// More than a turn gets, because this is the turn that does the reading.
		MaxSteps: meetingPrepSteps,
	}, nil)
	if err != nil {
		return session.SessionID(res.SessionID), "", err
	}
	// A model failure during prepare (a 429, a stream drop) is stashed in res.Err, not returned as
	// err — spawnChild always returns err=nil. Ignored, a participant whose prepare turn never ran
	// was reported ready with an empty brief, and the room opened as if it had read its workspace.
	// Meeting.Open's own doc names "a model that failed" as a participant that must carry Trouble;
	// this is what makes that true.
	if res.Err != "" {
		return session.SessionID(res.SessionID), "", fmt.Errorf("%s", res.Err)
	}
	return session.SessionID(res.SessionID), readyNote(res.Text), nil
}

// readyNote is what the screen shows about a participant that has finished getting ready.
//
// Prose, or nothing. A live run came back with `{"path": ".", "pattern": "fleet"}` as one
// companion's note — the model's last tool call echoed as its answer — and a roster showing a
// reader JSON they did not ask for is worse than a roster showing "ready" and no more. Silence
// here is not an error: the note is a courtesy, the readiness is the fact.
func readyNote(said string) string {
	out := strings.TrimSpace(said)
	if out == "" {
		return ""
	}
	if strings.HasPrefix(out, "{") || strings.HasPrefix(out, "[") {
		return ""
	}
	return out
}

// MeetingSayIn is a turn taken in the session the participant already has.
//
// The same session as its preparation and its earlier turns, so what it read to get ready is still
// there. Nothing is spawned: this is one more prompt in a conversation already going.
//
// # What comes back
//
// What it said, or a pass. A pass is a first-class answer: a companion whose workspace has nothing
// to do with the question should say so in one line and cost one turn, and a prompt that demanded
// a contribution would get "I agree with what design said" instead — which is the sentence that
// makes people stop reading meeting records.
func (a *App) MeetingSayIn(ctx context.Context, child session.SessionID, who, topic, transcript,
	minutes string, closing bool) (meeting.Utterance, error) {
	if strings.TrimSpace(string(child)) == "" {
		return meeting.Utterance{}, fmt.Errorf("this participant has no session in the meeting")
	}
	// Counted as activity for MeetingActive: these turns stay outside the run states on purpose,
	// which made a mid-round daemon invisible to Running() — and restartable under it (see App).
	a.meetingRounds.Add(1)
	defer a.meetingRounds.Add(-1)
	s := a.sessionInfo(ctx, child)
	if err := a.appendPromptText(ctx, child, event.Actor{Kind: event.ActorUser, ID: meeting.Origin},
		meetingPrompt(who, topic, transcript, minutes, closing)); err != nil {
		return meeting.Utterance{}, err
	}
	agent := AgentSpec{Name: spawnAgentName, System: meetingSystem(who),
		Tools: meetingLook, Model: s.Model}
	text, err := a.runLoop(ctx, s, agent, 1, meetingSteps, true)
	if err != nil {
		return meeting.Utterance{}, err
	}
	return readUtterance(who, text), nil
}

// MeetingWriteUp is the second half of a turn: the speaker rewrites the minutes with what it just
// said in them.
//
// # Why a session of its own
//
// The drafts have to accumulate somewhere — a document edited five times is five versions — and the
// one place they must not is the session where the participant DECIDES. A companion that reads the
// sentence it deleted two rounds ago is arguing with its own edit history instead of with the room.
//
// The alternative was cutting the old minutes out of each speaking request, which would move the
// prefix and cost the KV cache everything after it. This repository has already paid that bill:
// re-issuing a session for each round of deliberation was the single largest cost in a benchmark,
// and removing it cut writes per call by 62% with no loss of accuracy. So both sessions stay
// append-only, and the accumulation goes where it does no harm.
//
// This session gets NO tools. It reads nothing and runs nothing: everything it needs is in the
// prompt, and a secretary that could go and look would start reporting things nobody said.
func (a *App) MeetingWriteUp(ctx context.Context, child session.SessionID, who, topic, minutes,
	said string, room []meeting.Seat) (string, error) {
	if strings.TrimSpace(string(child)) == "" {
		return "", fmt.Errorf("this participant has no minutes session")
	}
	a.meetingRounds.Add(1)
	defer a.meetingRounds.Add(-1)
	s := a.sessionInfo(ctx, child)
	if err := a.appendPromptText(ctx, child,
		event.Actor{Kind: event.ActorUser, ID: meeting.MinutesOrigin},
		minutesPrompt(who, topic, minutes, said, room)); err != nil {
		return "", err
	}
	agent := AgentSpec{Name: spawnAgentName, System: minutesSystem(who), Model: s.Model}
	out, err := a.runLoop(ctx, s, agent, 1, 1, true)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MeetingWriteRoom opens the session the minutes are kept in, once per meeting per participant.
//
// It creates the session and runs NOTHING. That was not the first shape: it seeded a prompt saying
// "nothing to do yet, the first revision arrives next", which cost two model calls per participant
// per meeting and put two documents nobody asked for into the very context this session exists to
// keep clean. Measured on a live meeting — the seed turn answered the "nothing to do" prompt with a
// full minutes table of its own invention, then magi's own step-budget nudge drew a second one.
//
// A session with no turns in it is exactly right here. The first write-up is its first turn.
func (a *App) MeetingWriteRoom(ctx context.Context, sid session.SessionID, who, topic string) (
	session.SessionID, error) {
	s := a.sessionInfo(ctx, sid)
	return a.CreateSession(ctx, command.CreateSession{
		Workdir: s.Workdir,
		Parent:  string(sid),
		Agent:   spawnAgentName,
		Model:   s.Model,
		Actor:   event.Actor{Kind: event.ActorUser, ID: meeting.MinutesOrigin},
	})
}

func minutesSystem(who string) string {
	return "You are " + who + ", keeping the minutes of a meeting between companions. You write " +
		"down what the room has settled, what it has not, and who is doing what. You are not in " +
		"the discussion: you do not add opinions, and you do not decide anything that was not said."
}

// minutesTemplate is what the first speaker is handed: the skeleton of an ordinary work meeting's
// record, with every section present and empty.
//
// Given as a form rather than described in prose because a description produces a different shape
// from each model, and the second speaker then has to guess where its own edit belongs. A form
// filled in is a form everybody after can find their way around.
const minutesTemplate = `# <the question>

## In the room
%s

## Decided
- (nothing yet)

## Still open
- (nothing yet)

## Action items
- <owner>: <what> — <by when, if said> [not started]

## Questions for the room
- (nothing yet)`

// roomLines is the attendance block, written by magi rather than by the model.
//
// The convener knows who is in the room; the writer would be remembering it from a preparation turn
// several rounds ago. A record that names the wrong people is worse than one that names none, and
// this is the ordinary rule in this tree — the model is not asked for what magi already holds.
func roomLines(room []meeting.Seat) string {
	if len(room) == 0 {
		return "- (not recorded)"
	}
	var b strings.Builder
	for _, s := range room {
		b.WriteString("- " + s.Name)
		switch {
		case s.Person:
			b.WriteString(" — called this meeting")
		case s.Role != "":
			b.WriteString(" (" + s.Role + ")")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// minutesPrompt is the secretary's turn: here is the document, here is what was just said, give it
// back revised.
func minutesPrompt(who, topic, minutes, said string, room []meeting.Seat) string {
	var b strings.Builder
	b.WriteString("THE QUESTION\n" + strings.TrimSpace(topic) + "\n\n")
	if m := strings.TrimSpace(minutes); m != "" {
		b.WriteString("THE MINUTES AS THEY STAND\n" + m + "\n\n")
	} else {
		// Nobody has written anything yet: this speaker is the first, and it gets the form.
		b.WriteString("THE MINUTES ARE EMPTY. Start them from this form, keeping every heading:\n" +
			fmt.Sprintf(minutesTemplate, roomLines(room)) + "\n\n")
	}
	b.WriteString("WHAT " + who + " JUST SAID\n" + strings.TrimSpace(said) + "\n\n")
	b.WriteString("WHAT TO DO NOW\n" +
		"Give back the whole minutes, revised. Rules:\n" +
		"- Bullets only. One item per line, no paragraphs.\n" +
		"- Carry every section and every line you are not changing through UNCHANGED. Do not " +
		"summarise them: minutes that shrink each round end the meeting with nothing in them.\n" +
		"- Move a line from \"Still open\" to \"Decided\" only when the room actually settled it, and " +
		"say WHY in the same line — the reason is what a reader six weeks from now needs and the " +
		"only person who has it is the one who just said it.\n" +
		"- Attribute. Every line under \"Decided\" and \"Still open\" ends with the name of whoever " +
		"it came from, in brackets: `(" + who + ")`. Minutes nobody can trace back are a summary, " +
		"and a reader who disagrees with a line has nowhere to take it.\n" +
		"- \"In the room\" is given to you filled in. Carry it through exactly; do not add or " +
		"remove a name.\n" +
		"- Under \"Action items\", write only what somebody took on in their own words, and your " +
		"own. Never assign work to a name that did not accept it — that is a promise they do not " +
		"know they made.\n" +
		"- Answer with the document and nothing else. No preamble, no explanation of what you " +
		"changed.\n")
	return b.String()
}

// readUtterance turns what came back into a contribution or a pass.
//
// One keyword, at the start, because that is what a model can be relied on to do — and a reply
// that merely CONTAINS the word ("I would not pass on this") is a contribution, which is why the
// check is on the first word rather than anywhere in the text.
func readUtterance(who, text string) meeting.Utterance {
	said := strings.TrimSpace(text)
	first := said
	if i := strings.IndexAny(first, " \n\t:.—-"); i > 0 {
		first = first[:i]
	}
	// Undressed before it is compared. A model answering a chat writes markdown, and the first
	// live meeting this ran came back "**PASS** – my workspace only contains…" ten times: the word
	// was there, the asterisks were not in the comparison, and every pass was recorded as a
	// contribution. Nobody's pass count moved, the round never came back all-passes, and a
	// discussion in which nothing was said ran to the ceiling — ten model turns to say nothing,
	// which is the exact failure the convergence rule exists to prevent.
	if strings.EqualFold(strings.Trim(first, "*_`~ \t"), "PASS") {
		reason := strings.TrimSpace(strings.TrimLeft(said[len(first):], " \n\t:.—–·-*_`~"))
		return meeting.Utterance{Who: who, Pass: true, Text: reason}
	}
	if said == "" {
		// Nothing at all is a pass with nothing to say, not an error: the turn happened, the
		// participant produced no words, and inventing a contribution for it would be worse.
		return meeting.Utterance{Who: who, Pass: true}
	}
	return meeting.Utterance{Who: who, Text: said}
}

// meetingSystem is who the participant is, in every turn of the meeting including the first.
func meetingSystem(who string) string {
	return "You are " + who + ", taking part in a meeting between companions. You each work in " +
		"a different workspace and know different things; that is why you are all here. Read " +
		"your own files when you need to check something. You cannot change anything: this is " +
		"a discussion, and the work is handed out afterwards."
}

// meetingPrepSteps bounds the reading a participant does before the room opens.
const meetingPrepSteps = 14

// workNow is what this workspace looks like right now, read for the participant rather than by it.
//
// Cheap and bounded: the branch, what is uncommitted, and the last few commits. It answers "what
// have you been doing", which a companion arriving at a meeting has to have answered before it can
// say anything worth hearing.
func (a *App) workNow(ctx context.Context, workdir string) string {
	if a.plat == nil || strings.TrimSpace(workdir) == "" {
		return ""
	}
	var b strings.Builder
	if g, err := a.GitFacts(ctx, workdir); err == nil && g.Repo {
		b.WriteString("branch: " + g.Branch)
		if g.Upstream != "" {
			b.WriteString(" (tracking " + g.Upstream)
			if g.Ahead > 0 || g.Behind > 0 {
				b.WriteString(fmt.Sprintf(", %d ahead, %d behind", g.Ahead, g.Behind))
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
		if len(g.Changes) == 0 {
			b.WriteString("working tree: clean\n")
		} else {
			b.WriteString(fmt.Sprintf("working tree: %d changed\n", len(g.Changes)))
			for i, c := range g.Changes {
				if i == 12 {
					b.WriteString(fmt.Sprintf("  … and %d more\n", len(g.Changes)-i))
					break
				}
				b.WriteString("  " + c.Kind + "  " + c.Path + "\n")
			}
		}
	}
	if res, err := a.plat.Exec(ctx, port.Cmd{Path: "git",
		Args: []string{"log", "-n", "10", "--pretty=%h %ad %s", "--date=short"},
		Dir:  workdir, MaxOutput: 8 << 10}); err == nil && res.ExitCode == 0 {
		if out := strings.TrimSpace(string(res.Stdout)); out != "" {
			b.WriteString("\nrecent commits:\n" + out + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// roomNote is the roster a participant reads while getting ready: everybody else, and what each of
// them is for.
//
// Without it the last line of the prompt asks for "what you know that the others do not" while
// declining to say who the others are, and every participant writes about its own workspace and
// nothing else. What it should let somebody write is "the api one will have the wire; I will take
// the storage" — the meeting's shape decided before the first round instead of during it.
//
// Nothing is invented for a seat that published neither a role nor a list of abilities. A name on
// its own is still worth knowing, and a made-up description of a companion is worse than no
// description: the reader would plan around it.
func roomNote(me string, room []meeting.Seat) string {
	var b strings.Builder
	for _, s := range room {
		if s.Name == me {
			continue // it already knows what it brings; that is what this turn is for
		}
		switch {
		case s.Person:
			b.WriteString("  " + s.Name + " — the person who called this meeting\n")
			continue
		case s.Role != "" && s.Does != "":
			b.WriteString("  " + s.Name + " (" + s.Role + ") — can: " + s.Does + "\n")
		case s.Role != "":
			b.WriteString("  " + s.Name + " (" + s.Role + ")\n")
		case s.Does != "":
			b.WriteString("  " + s.Name + " — can: " + s.Does + "\n")
		default:
			b.WriteString("  " + s.Name + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// preparePrompt is the homework: read your own state, then say what you bring.
func preparePrompt(who, topic, work string, room []meeting.Seat) string {
	var b strings.Builder
	// **Do not tell it nobody will hear this.** That sentence used to be here, two paragraphs
	// above "for the person who called the meeting" — the prompt contradicted itself, and a
	// participant that believed the first half thought out loud before answering. Measured
	// (2026-09-03, live meeting): the note on the screen opened with the model's own plan for
	// its answer and ended with the instruction echoed back at itself ("Two or three lines in
	// Korean.") before the Korean began.
	//
	// What is true is narrower and worth saying exactly: the ROOM does not hear this turn — it
	// is not a round, nobody answers it — and the person who called the meeting reads what comes
	// back. A model told the truth about its audience writes for that audience.
	b.WriteString("A meeting is being called and you are in it. It has not started yet — this is " +
		"your time to get ready. The room will not hear this turn: it is not a round, and nobody " +
		"will answer it. What you write at the end is read by the person who called the meeting.\n\n")
	if n := strings.TrimSpace(who); n != "" {
		b.WriteString("You are " + n + ".\n\n")
	}
	b.WriteString("THE QUESTION\n" + strings.TrimSpace(topic) + "\n\n")
	if seats := roomNote(who, room); seats != "" {
		b.WriteString("WHO ELSE IS IN THE ROOM\n" + seats + "\n\n")
	}
	if work != "" {
		b.WriteString("YOUR WORKSPACE RIGHT NOW, read for you\n" + work + "\n\n")
	}
	b.WriteString("WHAT TO DO NOW\n" +
		"Read what you need to read in your own workspace to answer this well: the files the " +
		"question touches, what you changed recently, what is half-done. Check rather than " +
		"remember.\n\n" +
		"THEN ANSWER WITH\n" +
		"Two or three lines, for the person who called the meeting: what you bring to this " +
		"question, and anything you already know is a problem. Not a plan, not an offer to help — " +
		"what you know that the others do not.\n")
	return b.String()
}

// MeetingActive reports whether any meeting round is being composed right now. The auto-update idle
// gate reads it alongside Running(): meeting turns deliberately never enter the run states, so
// without this a daemon restarted itself mid-contribution and the console recorded the participant
// as failing the round.
func (a *App) MeetingActive() bool { return a.meetingRounds.Load() > 0 }

// meetingPrompt is what a participant is given: the question, everything said so far, and the two
// shapes an answer may take.
//
// The transcript is whole rather than summarised. A meeting where the fourth speaker reads a
// précis of the first three is a meeting whose one advantage — that each speaker read what came
// before — has been thrown away.
func meetingPrompt(who, topic, transcript, minutes string, closing bool) string {
	var b strings.Builder
	b.WriteString("THE QUESTION\n" + strings.TrimSpace(topic) + "\n\n")
	// The document BEFORE what has been said, because it is the shorter and the more settled of
	// the two: a speaker that reads what is already agreed does not spend its turn agreeing again.
	// Only the current version travels — the drafts it went through live in the other session.
	if m := strings.TrimSpace(minutes); m != "" {
		b.WriteString("THE MINUTES SO FAR — what the room has already written down\n" + m + "\n\n")
	}
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
