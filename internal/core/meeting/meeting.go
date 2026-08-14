// Package meeting is companions talking to each other until each of them knows what to do.
//
// # Why this is not the council
//
// The council is one model arguing with itself inside one turn: three voices, one context, one
// workspace. This is several COMPANIONS — separate daemons, separate workspaces, separate
// knowledge — and what makes it worth the turns is exactly that: api knows what the billing code
// does, design knows what the screen promises, ops knows what deploying it costs. Asking one model
// to play all three gets three opinions with one set of facts behind them.
//
// # Why there is a floor, and why it is a token
//
// Let everybody speak at once and they speak past each other. A participant's turn is atomic: it
// reads what has landed, writes its piece, and stops — so three running at once compose three
// replies to the same moment and none of them answers the others. That is not a discussion; it is
// three separate answers, which anybody could already get by asking three companions separately.
//
// A token makes each speaker read everything said before it. That single property is the whole
// difference between a meeting and a survey.
//
// # What a meeting produces
//
// Work, split up: at the end each participant has something to do, or explicitly nothing. Not a
// verdict, not a vote — the question a meeting answers here is "who does what about this", and a
// participant with nothing to do is an ordinary outcome rather than a failure to contribute.
//
// # What a meeting must not do
//
// Change anything. Participants speak from read-only sessions: the point is to decide, and a
// meeting that also edited three workspaces would be three agents acting on a plan that was still
// being argued about. Deciding and doing are separated on purpose, and the doing goes out as
// ordinary handovers afterwards.
package meeting

import (
	"strings"
	"time"
)

// Speaker is one participant: a companion, or the person running the meeting.
type Speaker struct {
	// Name is what everybody calls them, and what an utterance is attributed to.
	Name string
	// Socket is the companion's door. Empty for the person, which is also how the two are told
	// apart everywhere below — a person has no socket and no turn to spend.
	Socket string
	// Passes counts CONSECUTIVE passes. Two in a row and this speaker is not asked again until
	// somebody names them: the largest waste in a meeting is a turn spent on a companion that has
	// already said twice that this is not its business.
	Passes int
	// Ready is whether this participant has finished getting ready — read its own workspace, its
	// history and the question — and Brief is what it said it brings. Nobody speaks until
	// everybody is ready: a meeting where people arrive cold spends its first two rounds looking
	// things up out loud, and the reader watching the screen cannot tell that from a slow model.
	//
	// A person is ready the moment they are in the room; there is nothing for them to prepare.
	Ready bool
	Brief string
	// Trouble is why this participant could not get ready, when that is what happened. It is not
	// a reason to hold the room: the meeting opens without them and says so.
	Trouble string
}

// Person reports whether this speaker is the human in the room.
func (s Speaker) Person() bool { return s.Socket == "" }

// Utterance is one thing said, or one deliberate silence.
type Utterance struct {
	Who string
	// Text is what they said. Empty when Pass is set and they gave no reason, which is allowed:
	// making a reason mandatory is how "I agree with what design said" gets written down.
	Text string
	// Pass marks a speaker that READ the discussion and had nothing to add. Different from not
	// being asked — see Meeting.Next — and worth recording, because silence from somebody who read
	// it is information.
	Pass bool
	// Round is which lap this was said in, stamped when it is recorded.
	//
	// Derived at first, by counting back over the roster, and that was wrong the moment a round
	// ended: the walk found the same utterances again and every speaker read as having already had
	// their turn, so the meeting advanced a round per question and ran out without asking anybody
	// twice. A round is a fact about when something was said; it belongs in the record.
	Round int
	At    time.Time
}

// Task is what one participant leaves the meeting with. A meeting where somebody leaves with
// nothing is an ordinary meeting.
type Task struct {
	Who  string
	What string
}

// Meeting is who is talking, whose turn it is, and what has been said.
//
// A value, and every rule below is a method on it that returns the next state rather than reaching
// for a clock or a network. What it decides — who speaks next, when a round ends, when the whole
// thing is over — is exactly the part worth being able to test at a table.
type Meeting struct {
	Topic    string
	Speakers []Speaker
	// Holder is who has the floor: a speaker's name, or "" between turns.
	Holder string
	// Round counts from 1. Everybody speaks in the first one; from the second, a speaker who has
	// passed twice in a row is skipped.
	Round int
	// MaxRounds is a BACKSTOP and not a plan.
	//
	// A meeting ends when nobody has anything left to add — the participants decide that, by
	// passing, and a pass is exactly a vote to stop. Asking the convener for a number instead
	// makes them guess before the discussion how long the discussion needs to be, which is the one
	// thing they cannot know; and a number chosen that way is either a cap that cuts an argument
	// in half or a floor that pays for two laps of "I agree with design".
	//
	// It exists because the other failure is real too: every lap is one model turn per participant
	// asked, and a room that will not stop spends until somebody notices. So the count catches
	// that and nothing else. When it is what ended a meeting, Spent says so — a discussion that
	// converged and one that ran out of room are different outcomes, and a reader who cannot tell
	// them apart cannot tell whether the conclusions are the whole answer.
	MaxRounds int
	// Spent marks a meeting the backstop ended rather than the room.
	Spent bool
	Said  []Utterance
	// Named is anybody called on by name since the last time they spoke: it puts a skipped speaker
	// back in the round, which is what happens when a person says "@ops, what about the rollout".
	Named map[string]bool
	// Opened is false while the participants are still getting ready. The floor does not move and
	// nothing is asked until it is true — see Ready on each speaker.
	Opened bool
	// Closed is set when the meeting has finished — everybody passed, the backstop stopped it, or
	// the convener ended it.
	Closed bool
}

// New starts a meeting on a topic with the speakers given, in the order they will first speak.
//
// maxRounds is the backstop; zero takes the default. Five rather than three: the count is not
// meant to be what ends an ordinary meeting, and at three it was — a disagreement worth convening
// three companions for is rarely settled in one lap of statements, one of objections and nothing
// else.
func New(topic string, speakers []Speaker, maxRounds int) *Meeting {
	if maxRounds <= 0 {
		maxRounds = 5
	}
	return &Meeting{
		Topic: topic, Speakers: append([]Speaker(nil), speakers...),
		Round: 1, MaxRounds: maxRounds, Named: map[string]bool{},
	}
}

// Next says who should speak now, and reports false when the meeting is over.
//
// The person is never chosen: they take the floor when they want it (see Take) and are never made
// to answer. Everybody else is asked in order — all of them in the first round, and from the second
// only those who have something to say, judged by what they have done with their last two turns.
func (m *Meeting) Next() (Speaker, bool) {
	if m.Closed || !m.Opened {
		// A room that has not opened has nobody to ask. The participants are reading their own
		// workspaces; asking one of them now would be asking it to answer a question it has not
		// finished looking at, which is the thing the preparation turn exists to prevent.
		return Speaker{}, false
	}
	for {
		for _, s := range m.speakingOrder() {
			if m.spokeThisRound(s.Name) || s.Person() {
				continue
			}
			if m.Round > 1 && s.Passes >= 2 && !m.Named[s.Name] {
				continue // asked twice, said no twice: not asked again until somebody names them
			}
			return s, true
		}
		// The round is over, and there are two ways for that to be the end of it. Everybody having
		// passed is the ordinary one: the discussion has stopped moving, which is a better reason
		// to end than a count, and it ends here rather than after another lap of asking a silent
		// room. Running out of rounds is the backstop, and it is recorded as such — see Spent.
		if m.allPassedThisRound() {
			m.Closed = true
			return Speaker{}, false
		}
		if m.Round >= m.MaxRounds {
			m.Closed, m.Spent = true, true
			return Speaker{}, false
		}
		m.Round++
	}
}

// Open reports whether everybody has finished getting ready, and opens the room when they have.
//
// Everybody, not most: the reason to wait at all is that the first speaker should be answering a
// question the others have already read their own workspaces about. A participant that could not
// get ready — no daemon, a model that failed — does not hold the room; it carries its Trouble and
// the meeting opens without it, because the alternative is a room that never opens and a screen
// that cannot say why.
func (m *Meeting) Open() bool {
	if m.Opened {
		return true
	}
	for _, s := range m.Speakers {
		if s.Person() || s.Ready || s.Trouble != "" {
			continue
		}
		return false
	}
	m.Opened = true
	return true
}

// Prepared marks what a participant came back with, and whether it managed to.
func (m *Meeting) Prepared(who, brief, trouble string) {
	for i := range m.Speakers {
		if m.Speakers[i].Name != who {
			continue
		}
		m.Speakers[i].Ready = trouble == ""
		m.Speakers[i].Brief = brief
		m.Speakers[i].Trouble = trouble
		return
	}
}

// Take gives the floor to whoever is named, out of order.
//
// The person uses it by starting to type, and a convener uses it to call on somebody. Nothing is
// interrupted: what it changes is who goes NEXT, which is the whole of what a floor is.
func (m *Meeting) Take(who string) {
	m.Holder = who
	if !m.has(who) {
		return
	}
	m.Named[who] = true
}

// Yield gives the floor back, which is what a person does when they stop typing.
func (m *Meeting) Yield() { m.Holder = "" }

// Say records an utterance and moves the floor on.
//
// A pass is an utterance too. The consecutive count is what the skipping rule reads, and it is
// reset by speaking — a companion that passed twice and then had something to say is back in the
// conversation, without anybody having to un-skip it.
func (m *Meeting) Say(u Utterance) {
	if u.At.IsZero() {
		u.At = time.Now()
	}
	if u.Round == 0 {
		u.Round = m.Round
	}
	m.Said = append(m.Said, u)
	for i := range m.Speakers {
		if m.Speakers[i].Name != u.Who {
			continue
		}
		if u.Pass {
			m.Speakers[i].Passes++
		} else {
			m.Speakers[i].Passes = 0
		}
	}
	delete(m.Named, u.Who)
	m.Holder = ""
	// Being named is what puts a skipped speaker back, and it is spent by the naming being
	// answered — otherwise one "@ops?" would keep ops in every round for the rest of the meeting.
	for _, name := range mentions(u.Text) {
		if m.has(name) && !strings.EqualFold(name, u.Who) {
			m.Named[name] = true
		}
	}
}

// Close ends the meeting, which is the convener saying it is time to write down who does what.
func (m *Meeting) Close() { m.Closed = true; m.Holder = "" }

// Reopen puts a finished meeting back into session, with something new to answer.
//
// A room ends when nobody has anything left to add, and the participants decide that by passing —
// which is right, and is also how a person who stepped away for two minutes comes back to a
// meeting that talked among itself and stopped. There has to be a way to say "no, keep going, and
// here is why", and it has to be a person who says it: nothing here reopens itself.
//
// What it takes to actually restart:
//   - Closed goes back to false, and Spent with it: whatever ended it, this is not that any more.
//   - The pass counts are cleared. Two passes in a row is what stops a participant being asked,
//     and everybody who is here has just passed — leaving those in place would reopen a room in
//     which nobody may speak.
//   - The ceiling moves up by as much as it was, so the backstop is not standing on the door.
//   - The reason travels as a contribution from the person, because that is what it is: the
//     transcript has to say why the room came back or the next round is answering nothing.
func (m *Meeting) Reopen(who, why string) {
	m.Closed, m.Spent = false, false
	m.Holder = ""
	for i := range m.Speakers {
		m.Speakers[i].Passes = 0
	}
	m.MaxRounds += m.MaxRounds
	if strings.TrimSpace(why) != "" {
		m.Say(Utterance{Who: who, Text: strings.TrimSpace(why)})
		m.Named[who] = true
	}
}

// Transcript is what a speaker about to take its turn has to read: everything said so far, in
// order, attributed. The whole of it and not a summary — a meeting where the fourth speaker reads
// a précis of the first three is a meeting whose point has been thrown away.
func (m *Meeting) Transcript() string {
	var b strings.Builder
	for _, u := range m.Said {
		b.WriteString(u.Who)
		if u.Pass {
			b.WriteString(" passed")
			if strings.TrimSpace(u.Text) != "" {
				b.WriteString(": " + strings.TrimSpace(u.Text))
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(": " + strings.TrimSpace(u.Text) + "\n")
	}
	return b.String()
}

// speakingOrder is the roster, with anyone named moved to the front: being called on means being
// answered next, not eventually.
func (m *Meeting) speakingOrder() []Speaker {
	out := make([]Speaker, 0, len(m.Speakers))
	for _, s := range m.Speakers {
		if m.Named[s.Name] {
			out = append(out, s)
		}
	}
	for _, s := range m.Speakers {
		if !m.Named[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// spokeThisRound reports whether this speaker has already had the floor in the current round.
//
// Counted from the utterances rather than kept as a flag: a meeting is reconstructed from its own
// record when a page reloads or a process restarts, and a flag would be the one part that could
// not be.
func (m *Meeting) spokeThisRound(who string) bool {
	for _, u := range m.roundSaid() {
		if u.Who == who {
			return true
		}
	}
	return false
}

func (m *Meeting) allPassedThisRound() bool {
	said := m.roundSaid()
	if len(said) == 0 {
		return false
	}
	for _, u := range said {
		if !u.Pass && !m.isPerson(u.Who) {
			return false
		}
	}
	return true
}

// roundSaid is what has been said in the round now running.
//
// Read off the stamp each utterance carries rather than counted back over the roster: the walk
// could not see where one round ended and the next began, so once everybody had spoken it kept
// finding the same lap and reported every speaker as done — for every round after the first.
func (m *Meeting) roundSaid() []Utterance {
	out := []Utterance{}
	for _, u := range m.Said {
		if u.Round == m.Round && !m.isPerson(u.Who) {
			out = append(out, u)
		}
	}
	return out
}

func (m *Meeting) isPerson(who string) bool {
	for _, s := range m.Speakers {
		if s.Name == who {
			return s.Person()
		}
	}
	return false
}

func (m *Meeting) has(who string) bool {
	for _, s := range m.Speakers {
		if s.Name == who {
			return true
		}
	}
	return false
}

// mentions finds "@name" in what somebody said. The one piece of syntax this package reads, and it
// is the one people already use — a meeting where calling on somebody needs a control rather than
// a sentence is a meeting with a form in it.
func mentions(text string) []string {
	var out []string
	for _, field := range strings.Fields(text) {
		at := strings.IndexByte(field, '@')
		if at < 0 {
			continue
		}
		name := strings.TrimFunc(field[at+1:], func(r rune) bool {
			return !(r == '-' || r == '_' || r == '.' ||
				(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
		})
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
