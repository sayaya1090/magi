package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/council"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What the council said, for a screen that reads the log rather than the bus.
//
// # Why this exists at all
//
// The terminal draws the council inline and gets the ordering for free: it consumes EVENTS, in
// order, and appends a block for each. A verdict lands between the two messages it happened
// between because that is literally where it arrived.
//
// The browser console cannot do that. It asks for SessionState, which reconstructs the log into
// messages — and a message is a lossy projection of the log. Council rounds are events and not
// message parts, so they were not merely unstyled or unhandled there: they could not arrive. The
// console showed a turn ending with no sign that three members had voted on whether it should.
//
// # Anchoring, rather than a second ordering
//
// The obvious repair — send the council separately and put it at the end — is right most of the
// time and wrong exactly when a session has more than one turn, which is every session anybody
// keeps. The other obvious repair, giving the browser the events and letting it assemble the
// transcript itself, means a second implementation of reconstruct: the two would agree until one
// of them learned something.
//
// So each mark carries the id of the message it followed. Both sides key on the same id out of the
// same log — reconstruct puts that id on the message (Message.ID is the log's MessageID) and this
// walk reads it from the same events — so a splice cannot land in the wrong place without one of
// them having misread the log.

// CouncilMark is one thing the council did, and where in the transcript it did it.
type CouncilMark struct {
	// After is the id of the message this landed after. Empty means it preceded every message,
	// which a council event cannot really do — a round is convened about work — but is what an
	// unanchored mark would look like, so it is representable rather than silently dropped.
	After string `json:"after,omitempty"`
	Round int    `json:"round"`
	// Member and Decision are a vote. Empty Member means this is the round's outcome instead.
	Member   string `json:"member,omitempty"`
	Lens     string `json:"lens,omitempty"`
	Decision string `json:"decision"`
	// Why is the member's reasoning, or the note explaining an outcome. It is the thing behind the
	// one-line summary on screen, which is why it is carried whole rather than clipped here: the
	// surface that shows it decides how much of it to show.
	Why string `json:"why,omitempty"`
	// Cite is the fragment of the record a member said its vote rests on. An empty one on a "done"
	// is itself worth seeing, so it travels even when blank.
	Cite string `json:"cite,omitempty"`
	// Silent says nobody spoke: on a vote, this member was never reached (backend down, deadline,
	// unreadable reply); on an outcome, the round closed with no votes in it at all. It is not an
	// abstention and it is not a rejection — the two things a surface would otherwise call it.
	Silent bool `json:"silent,omitempty"`
	// Tally is set on an outcome: how the vote came out, in words.
	Tally string `json:"tally,omitempty"`
	// Debate is set on an outcome that had a rebuttal round, in words. A rebuttal runs only when
	// the independent vote SPLIT, and the tally beside it is the one taken after — so a unanimous
	// line can be the product of an argument rather than of agreement, and a reader who cannot see
	// that reads three members agreeing where there were two and one who was talked round.
	Debate string `json:"debate,omitempty"`
	// Confidence, Feedback and Keep are the rest of a vote, carried for a surface that shows one
	// whole rather than one line. They were dropped here while the terminal read them straight off
	// the event, which is why the console could show a verdict and not what the member wanted done
	// about it — Keep in particular is the instruction protecting work already finished.
	Confidence float64 `json:"confidence,omitempty"`
	Feedback   string  `json:"feedback,omitempty"`
	Keep       string  `json:"keep,omitempty"`
}

// IsOutcome reports whether this mark is a round's decision rather than one member's vote.
func (m CouncilMark) IsOutcome() bool { return m.Member == "" }

// CouncilMarks returns every council vote and outcome in a session, in log order, each anchored to
// the message it followed.
func (a *App) CouncilMarks(ctx context.Context, sid session.SessionID) ([]CouncilMark, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, err
	}
	// The same events reconstruct is given, so the anchors refer to messages that exist. Without
	// this a mark could point at a resurfaced interjection's dropped original — an id the rendered
	// transcript does not contain — and the splice would silently do nothing.
	return councilMarks(dropResurfacedOrigins(evs)), nil
}

// councilMarks is the walk, separated from the read so a test can drive it with a slice.
func councilMarks(evs []event.Event) []CouncilMark {
	var out []CouncilMark
	last := "" // the id of the most recent message the log has touched

	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID != "" {
				last = d.MessageID
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID != "" {
				last = d.MessageID
			}
		case event.TypeCouncilVerdict:
			var d event.CouncilVerdictData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			m := CouncilMark{After: last, Round: d.Round, Member: d.Member, Lens: d.Lens,
				Decision: d.Decision, Why: d.Rationale, Cite: d.Cite, Silent: d.Silent,
				Confidence: d.Confidence, Feedback: d.Feedback, Keep: d.Keep}
			// A member arrives twice: the live preview when it answers, then the recorded fact when
			// the round closes. Identical ones are the same news. One that DIFFERS is a rebuttal
			// round having changed that member's mind, which is news of its own — the same rule the
			// terminal follows, and for the same reason.
			if n := len(out); n > 0 && out[n-1] == m {
				continue
			}
			if seen := lastVoteOf(out, d.Round, d.Member); seen != nil && *seen == m {
				continue
			}
			out = append(out, m)
		case event.TypeCouncilDecided:
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			out = append(out, CouncilMark{After: last, Round: d.Round, Decision: d.Decision,
				Why: d.Note, Tally: tallyWords(d.Tally), Debate: debateWords(d.Debate),
				// No votes at all, and some member never answered: the round did not judge the
				// work, so the surface must not spell its outcome "reject".
				Silent: d.Tally.Voters == 0 && d.Tally.Silent > 0})
		}
	}
	return out
}

// lastVoteOf finds this member's most recent vote in a round, or nil.
func lastVoteOf(marks []CouncilMark, round int, member string) *CouncilMark {
	for i := len(marks) - 1; i >= 0; i-- {
		if marks[i].Round == round && marks[i].Member == member {
			return &marks[i]
		}
	}
	return nil
}

// tallyWords says how a round came out, in the shape a reader can check against the votes above it.
//
// Words rather than the struct: a screen showing "2/3 done" beside three verdicts it just listed is
// telling somebody something they can verify at a glance, and a JSON breakdown with six numbers in
// it is not. Abstentions are named only when there were any, because a zero there is noise on every
// round that did not have one.
func tallyWords(b council.Breakdown) string {
	out := fmt.Sprintf("%d done, %d continue of %d", b.Done, b.Continue, b.Voters)
	// Two kinds of non-vote, named apart: a member that declined, and one that was never reached
	// (backend down, deadline, unreadable reply — Verdict.Silent). Reading a council that could not
	// be called as a council that shrugged sends the reader to the wrong repair.
	if said := b.Abstain - b.Silent; said > 0 {
		out += fmt.Sprintf(" (%d abstained)", said)
	}
	if b.Silent > 0 {
		out += fmt.Sprintf(" (%d no answer)", b.Silent)
	}
	return out
}

// debateWords says whether a rebuttal round ran and what it did, or "" when none ran.
//
// CouncilDecidedData.Debate was added because the round left no trace and "did arguing change the
// outcome" could not be asked of a run. Three surfaces then learned to read it — the TUI verdict
// line, the headless transcript, the loop map — and the console, the one with the most room to
// show it, was not among them: the field never reached the wire, so neither browser could have
// rendered it if it had tried. The comment above councilText in magi-web says two surfaces
// reading one record must not each drop the half the other kept. This is the half that was still
// being dropped.
//
// A rebuttal that moved NOBODY is named too, and deliberately: that the members heard each other
// out and did not budge is the more interesting of the two outcomes, and silence about it is
// indistinguishable from no debate having run.
func debateWords(d *council.DebateOutcome) string {
	if d == nil {
		return ""
	}
	moved := fmt.Sprintf("%d members moved", d.Changed)
	if d.Changed == 1 {
		moved = "1 member moved"
	}
	switch {
	case d.Changed == 0:
		return "debated, no one moved"
	case d.Before != d.After:
		return fmt.Sprintf("debated: %s→%s, %s", d.Before, d.After, moved)
	default:
		return fmt.Sprintf("debated: %s held, %s", d.After, moved)
	}
}
