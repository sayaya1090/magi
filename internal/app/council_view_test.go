package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// councilMarks says it is "separated from the read so a test can drive it with a slice". No test
// did: measured from every caller in the tree — magi-web's handlers and the fleet's row build —
// the walk was 45.8% covered, and tallyWords and lastVoteOf were 0%. What that bought is that the
// two things this file's own comments say most loudly (a round nobody voted in is not a rejection;
// a member arriving twice is one mark unless it changed) were held by nothing at all. The console
// tests hand-build a CouncilMark with the tally already a string, so they exercise the renderer and
// leave the producer of that string unrun.

func decided(d event.CouncilDecidedData) CouncilMark {
	marks := councilMarks([]event.Event{mkEvent(event.TypeCouncilDecided, event.ActorSystem, d)})
	if len(marks) != 1 {
		panic("one decided event must produce one mark")
	}
	return marks[0]
}

// A rebuttal only runs when the independent vote SPLIT, and the tally recorded beside it is the one
// taken after — so a 3-0 that started 1-2 reads as agreement that never happened. The TUI verdict
// line, the headless transcript and the loop map all say which; the console could not, because the
// field stopped at the wire.
func TestTheConsoleIsToldWhetherTheRoundWasArgued(t *testing.T) {
	quiet := decided(event.CouncilDecidedData{Round: 1, Decision: "done",
		Tally: council.Breakdown{Done: 3, Voters: 3}})
	if quiet.Debate != "" {
		t.Errorf("no rebuttal ran, so there is nothing to say: %q", quiet.Debate)
	}

	turned := decided(event.CouncilDecidedData{Round: 1, Decision: "done",
		Tally:  council.Breakdown{Done: 3, Voters: 3},
		Debate: &council.DebateOutcome{Before: council.Continue, After: council.Done, Changed: 2}})
	if !strings.Contains(turned.Debate, "continue→done") || !strings.Contains(turned.Debate, "2 members moved") {
		t.Errorf("the argument turned the outcome over and the row must say so: %q", turned.Debate)
	}

	held := decided(event.CouncilDecidedData{Round: 1, Decision: "done",
		Tally:  council.Breakdown{Done: 3, Voters: 3},
		Debate: &council.DebateOutcome{Before: council.Done, After: council.Done, Changed: 1}})
	if !strings.Contains(held.Debate, "done held") || !strings.Contains(held.Debate, "1 member moved") {
		t.Errorf("one member moved but the outcome stood, singular: %q", held.Debate)
	}

	// The one worth naming most: they heard each other and nobody budged. Saying nothing here is
	// indistinguishable from no debate having run.
	stood := decided(event.CouncilDecidedData{Round: 1, Decision: "done",
		Tally:  council.Breakdown{Done: 3, Voters: 3},
		Debate: &council.DebateOutcome{Before: council.Done, After: council.Done, Changed: 0}})
	if !strings.Contains(stood.Debate, "no one moved") {
		t.Errorf("a rebuttal that moved nobody is still a rebuttal: %q", stood.Debate)
	}
}

// The console's Silent flag is what stops councilText spelling an unreachable council "reject".
// magi-web's test builds the flag by hand; this is the line that decides it.
func TestARoundNobodyVotedInIsMarkedSilentAndOneWithVotesIsNot(t *testing.T) {
	unreachable := decided(event.CouncilDecidedData{Round: 1, Decision: "continue",
		Tally: council.Breakdown{Abstain: 3, Silent: 3}})
	if !unreachable.Silent {
		t.Error("no votes and three members never reached: the round judged nothing, and a surface " +
			"reading this as a rejection sends the reader to fix the work instead of the backend")
	}

	// Voters == 0 alone is not enough. The short-circuit in council_advice.go emits a decided fact
	// with an empty tally when the agent re-declares an identical report — a real prior rejection,
	// with nobody unreachable. Calling that "the council did not answer" would be its own lie.
	empty := decided(event.CouncilDecidedData{Round: 1, Decision: "continue",
		Note: "the agent declared finished again without changing anything"})
	if empty.Silent {
		t.Error("nothing failed here; the earlier councils answered and this is their answer standing")
	}

	voted := decided(event.CouncilDecidedData{Round: 1, Decision: "continue",
		Tally: council.Breakdown{Continue: 2, Done: 1, Voters: 3, Abstain: 1, Silent: 1}})
	if voted.Silent {
		t.Error("three members voted; one silent member does not turn a real vote into no answer")
	}
}

// The two kinds of non-vote, named apart — a member that declined, and one that was never reached.
// Zeros stay off the line: a "(0 abstained)" on every clean round is noise, and noise is what a
// reader stops reading.
func TestTallyWordsSeparatesDecliningFromNotAnswering(t *testing.T) {
	if got := tallyWords(council.Breakdown{Done: 2, Continue: 1, Voters: 3}); got != "2 done, 1 continue of 3" {
		t.Errorf("a clean round carries no parentheses: %q", got)
	}
	if got := tallyWords(council.Breakdown{Done: 2, Voters: 2, Abstain: 1}); got != "2 done, 0 continue of 2 (1 abstained)" {
		t.Errorf("a member that chose to abstain: %q", got)
	}
	if got := tallyWords(council.Breakdown{Done: 2, Voters: 2, Abstain: 1, Silent: 1}); got != "2 done, 0 continue of 2 (1 no answer)" {
		t.Errorf("Silent <= Abstain always, so a purely unreachable member must not ALSO be counted "+
			"as having abstained: %q", got)
	}
	if got := tallyWords(council.Breakdown{Done: 1, Voters: 1, Abstain: 2, Silent: 1}); got != "1 done, 0 continue of 1 (1 abstained) (1 no answer)" {
		t.Errorf("one of each, and both said: %q", got)
	}
	// The literal cmd/magi-web/council_silent_test.go hand-writes. It was faithful; nothing kept it so.
	if got := tallyWords(council.Breakdown{Abstain: 3, Silent: 3}); got != "0 done, 0 continue of 0 (3 no answer)" {
		t.Errorf("the console test's literal no longer matches what produces it: %q", got)
	}
}

// A mark is spliced into the transcript by the id of the message it followed. Both sides key on the
// same id out of the same log, so a walk that anchors to the wrong message puts a verdict under
// somebody else's turn.
func TestAMarkCarriesTheIDOfTheMessageItFollowed(t *testing.T) {
	marks := councilMarks([]event.Event{
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "go"}}}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m2", Role: session.RoleAssistant,
				Part: session.Part{Kind: session.PartText, Text: "done"}}),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem,
			event.CouncilVerdictData{Round: 1, Member: "melchior", Decision: "done"}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m3", Role: session.RoleAssistant,
				Part: session.Part{Kind: session.PartText, Text: "more"}}),
		mkEvent(event.TypeCouncilDecided, event.ActorSystem,
			event.CouncilDecidedData{Round: 1, Decision: "done", Tally: council.Breakdown{Done: 1, Voters: 1}}),
	})
	if len(marks) != 2 {
		t.Fatalf("a vote and an outcome are two marks, got %d", len(marks))
	}
	if marks[0].After != "m2" || marks[1].After != "m3" {
		t.Errorf("each mark follows the message the log had most recently touched, got %q and %q",
			marks[0].After, marks[1].After)
	}
	if !marks[1].IsOutcome() || marks[0].IsOutcome() {
		t.Error("the outcome is the one with no member on it")
	}
}

// A member arrives twice: the live preview when it answers, then the recorded fact when the round
// closes. Identical ones are the same news. One that DIFFERS is a rebuttal having changed that
// member's mind, which is news of its own. The second repeat is not adjacent — another member voted
// in between — which is the case lastVoteOf exists for and the one nothing ran.
func TestARepeatedVoteCollapsesUnlessItChanged(t *testing.T) {
	melchior := event.CouncilVerdictData{Round: 1, Member: "melchior", Decision: "continue",
		Rationale: "the tests do not cover it"}
	balthasar := event.CouncilVerdictData{Round: 1, Member: "balthasar", Decision: "done"}
	moved := melchior
	moved.Decision = "done"
	moved.Rationale = "balthasar showed me the probe"

	marks := councilMarks([]event.Event{
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, melchior),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, balthasar),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, melchior), // the fact, after the preview
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, balthasar),
	})
	if len(marks) != 2 {
		t.Fatalf("two members answered once each; the console must not list either twice, got %d: %+v",
			len(marks), marks)
	}

	changed := councilMarks([]event.Event{
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, melchior),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, balthasar),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, moved),
	})
	if len(changed) != 3 || changed[2].Decision != "done" {
		t.Fatalf("melchior changed its mind, which is a second thing it said: %+v", changed)
	}

	// A round of its own is a different round, whatever it says.
	round2 := melchior
	round2.Round = 2
	again := councilMarks([]event.Event{
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, melchior),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, balthasar),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem, round2),
	})
	if len(again) != 3 {
		t.Fatalf("the same verdict in a later round is that round's verdict: %+v", again)
	}
}

// A payload that will not parse is skipped, not carried half-built. A mark with a zero round and an
// empty decision would render as a councillor who voted for nothing.
func TestAnUnreadablePayloadIsSkipped(t *testing.T) {
	bad := event.Event{Type: event.TypeCouncilVerdict, Actor: event.Actor{Kind: event.ActorSystem}, Data: []byte(`{"round":`)}
	worse := event.Event{Type: event.TypeCouncilDecided, Actor: event.Actor{Kind: event.ActorSystem}, Data: []byte(`not json`)}
	if marks := councilMarks([]event.Event{bad, worse}); len(marks) != 0 {
		t.Errorf("neither event says anything readable: %+v", marks)
	}
}

// TestAMarkNeverPointsAtAMessageTheTranscriptDropped is the one thing CouncilMarks adds over the
// walk it delegates to, and it was held by nothing.
//
// An interjection that gets resurfaced leaves two prompts in the log — the original and the copy —
// and reconstruct drops the original, so the rendered transcript does not contain it. A verdict
// that landed right after the original anchors to it, and the console splices each mark in after
// the message named on it: an anchor naming a message that is not there matches nothing and the
// mark is silently never drawn. The vote happened; the reader is simply never shown it.
func TestAMarkNeverPointsAtAMessageTheTranscriptDropped(t *testing.T) {
	a, sid := sessionLog(t,
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{MessageID: "m_ask", Parts: []session.Part{{Kind: session.PartText, Text: "go"}}}),
		mkEvent(event.TypePartAppended, event.ActorAgent,
			event.PartAppendedData{MessageID: "m_reply", Role: session.RoleAssistant,
				Part: session.Part{Kind: session.PartText, Text: "working"}}),
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{MessageID: "m_orig", Parts: []session.Part{{Kind: session.PartText, Text: "wait — also check the parser"}}}),
		mkEvent(event.TypeCouncilVerdict, event.ActorSystem,
			event.CouncilVerdictData{Round: 1, Member: "melchior", Decision: "continue"}),
		mkEvent(event.TypePromptSubmitted, event.ActorUser,
			event.PromptSubmittedData{MessageID: "m_again", ResurfacedFrom: "m_orig",
				Parts: []session.Part{{Kind: session.PartText, Text: "wait — also check the parser"}}}),
	)
	marks, err := a.CouncilMarks(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 {
		t.Fatalf("one verdict is one mark, got %d", len(marks))
	}
	if marks[0].After == "m_orig" {
		t.Error("the mark follows a prompt the transcript does not contain, so it will never be drawn")
	}
	if marks[0].After != "m_reply" {
		t.Errorf("with the original gone the mark follows the last message that survived, got %q", marks[0].After)
	}
}

// TestCouncilMarksOfAnUnreadableLogIsNotAnEmptyRound: no marks is how a session that never convened
// a council reads. A log that could not be opened is not that, and answering with it would put
// "the council said nothing" on a session whose council may well have vetoed the work.
func TestCouncilMarksOfAnUnreadableLogIsNotAnEmptyRound(t *testing.T) {
	boom := errors.New("log is on a disk that went away")
	a := &App{store: unreadableStore{err: boom}}
	marks, err := a.CouncilMarks(context.Background(), session.SessionID("s_log"))
	if !errors.Is(err, boom) {
		t.Errorf("read failure did not reach the caller; err = %v", err)
	}
	if marks != nil {
		t.Errorf("a failed read still produced %d mark(s)", len(marks))
	}
}
