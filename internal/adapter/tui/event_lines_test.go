package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// The council's advisory "keep" — what a revision must NOT lose — was produced, persisted per
// member and prepended to the feedback injected into the model, while having NO rendering path
// anywhere: the one instruction that protects work already done was the one part of a verdict the
// user could never read. It must reach both the transcript row and the detail modal, and it must
// do so for an APPROVING member too, since that member's keep is precisely what a rewrite forced
// by someone else's objection would drop.
func TestCouncilKeepReachesTheScreen(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 100

	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Casper"}, Rule: "majority",
		Task: "add a health endpoint", Plan: "[write] serve /healthz", Keep: true,
	}))
	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 1, Member: "Casper", Lens: "correctness", Decision: "done",
		Rationale: "the procedure is sound", Keep: "step 2's fixture setup — the later steps read it",
	}))

	blk := m.blocks[len(m.blocks)-1]
	row := ansi.Strip(m.renderBlock(blk))
	if !strings.Contains(row, "step 2's fixture setup") {
		t.Errorf("an approving member's keep must render under the verdict row:\n%s", row)
	}
	if !strings.Contains(row, "keep") {
		t.Errorf("the keep line must be labelled as such:\n%s", row)
	}

	m.councilDetail = &blk.councilVerdicts[0]
	m.councilDetailEvidence = blk.evidence
	detail := ansi.Strip(m.renderCouncilDetail(100))
	if !strings.Contains(detail, "step 2's fixture setup") {
		t.Errorf("the detail modal must show the keep:\n%s", detail)
	}
	// An empty keep section is ambiguous — nobody was asked, or everyone was asked and none
	// answered — so the round records that it ASKED, and the evidence says so.
	if !strings.Contains(detail, "must preserve") {
		t.Errorf("the evidence must state that this round asked for a keep:\n%s", detail)
	}
	m.councilDetail = nil

	// A round that did NOT ask must not claim it did, and a member with no keep adds no line.
	if got := formatCouncilEvidence(event.CouncilConvenedData{Task: "t"}); strings.Contains(got, "must preserve") {
		t.Errorf("a round that never asked must not advertise a keep: %q", got)
	}
	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 2, Member: "Casper", Decision: "done", Rationale: "fine",
	}))
	if row := ansi.Strip(m.renderBlock(m.blocks[len(m.blocks)-1])); strings.Contains(row, "keep") {
		t.Errorf("a verdict with no keep must add no keep line:\n%s", row)
	}
}

// Facts that were produced, persisted, and consumed by prompts while having no on-screen path:
// a contract fixed mid-run left no mark, and a structural concern was reachable only by clicking
// a council member — so a concern raised on a turn that never convened a council was invisible
// outright. (A discarded side-pass was the third; its event type had no producer left and came
// out with this line on 2026-08-02.)
func TestPreviouslyUnrenderedFactsGetALine(t *testing.T) {
	if got := artifactLine(event.ArtifactEmittedData{Artifact: artifactOf("Acceptance criteria (plan audit)", "acceptance-criteria")}); !strings.Contains(got, "Acceptance criteria") {
		t.Errorf("an artifact must record WHEN it was fixed: %q", got)
	}
	// Falls back to the kind so an untitled artifact is still a visible milestone.
	if got := artifactLine(event.ArtifactEmittedData{Artifact: artifactOf("", "check-audit")}); !strings.Contains(got, "check-audit") {
		t.Errorf("an untitled artifact must fall back to its kind: %q", got)
	}
	if got := artifactLine(event.ArtifactEmittedData{}); got != "" {
		t.Errorf("an empty artifact renders nothing, got %q", got)
	}
}

// The aggregate feedback is a merge of the members' own feedback, and each rejecting member's
// reason already renders under its verdict row — so printing it under the decision too would say
// the same thing twice. The one round that emits NO verdicts (a standing rejection reused because
// nothing changed since the last one) is exactly where the demand would otherwise appear nowhere.
func TestDecidedShowsTheDemandOnlyWhenNoVerdictsWereRendered(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 1, Member: "Melchior", Decision: "continue", Feedback: "the output is never compared",
	}))
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Continue: 1},
		Feedback: "the output is never compared",
	}))
	decided := ansi.Strip(m.blocks[len(m.blocks)-1].text)
	if strings.Contains(decided, "the output is never compared") {
		t.Errorf("a deliberated round already shows each member's reason — the decision must not repeat it:\n%s", decided)
	}

	// Round 2 reuses the standing rejection without deliberating: no verdicts, so this line is
	// the only place the demand can appear.
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Decision: string(council.Continue), Tally: council.Breakdown{Continue: 1},
		Feedback: "the output is never compared",
		Note:     "no new tool actions since the last rejection — verdict reused without deliberation",
	}))
	reused := ansi.Strip(m.blocks[len(m.blocks)-1].text)
	if !strings.Contains(reused, "the output is never compared") {
		t.Errorf("a round with no verdicts must state what it is still refusing over:\n%s", reused)
	}
}

// artifactOf builds the minimal artifact the line builder reads.
func artifactOf(title, kind string) artifact.Artifact {
	return artifact.Artifact{Title: title, Kind: artifact.Kind(kind)}
}

// A cancelled prompt, and each interjection coalesced into a later one, is recorded as
// abandoned and read back by seedPromptIdx so it never seeds a turn — but nothing showed it.
// onTurnFinished clears the queued glyph off every waiting bubble at turn end, so the dropped
// request came to rest looking exactly like one that had been answered, on screen and again on
// every resume.
func TestAbandonedPromptSaysSoOnItsBubble(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 100
	m.blocks = []block{
		{kind: blockUser, reqID: "m_1", text: "first ask", queued: true},
		{kind: blockAssistant, text: "working"},
		{kind: blockUser, reqID: "m_2", text: "second ask"},
	}
	m.cache = []string{"c0", "c1", "c2"}

	m.applyEvent(ev(t, event.TypePromptAbandoned, event.PromptAbandonedData{MsgID: "m_1"}))

	if !m.blocks[0].abandoned || m.blocks[0].queued {
		t.Fatalf("the abandoned bubble must be marked and stop claiming it is still waiting: %+v", m.blocks[0])
	}
	if m.blocks[2].abandoned {
		t.Errorf("only the named request was abandoned, not the whole transcript")
	}
	if len(m.cache) != 0 {
		t.Errorf("the cache must be dropped from that bubble's index so it re-renders, got %v", m.cache)
	}

	row := ansi.Strip(m.renderBlock(m.blocks[0]))
	if !strings.Contains(row, "not answered") {
		t.Errorf("the bubble must say it will never be answered:\n%s", row)
	}
	// The copy chip's click column is derived from label+timestamp width alone, so the note
	// must trail the chip — otherwise the copy button slides out from under its hit-test.
	if i, j := strings.Index(row, "⧉"), strings.Index(row, "not answered"); i < 0 || i > j {
		t.Errorf("the note must come after the copy chip, not shift it right:\n%s", row)
	}
	// An answered request gets no note.
	if got := ansi.Strip(m.renderBlock(m.blocks[2])); strings.Contains(got, "not answered") {
		t.Errorf("a live request must not be labelled abandoned:\n%s", got)
	}
	// An unknown id is a no-op, not a panic or a mislabelled neighbour.
	m.applyEvent(ev(t, event.TypePromptAbandoned, event.PromptAbandonedData{MsgID: "m_absent"}))
	m.applyEvent(ev(t, event.TypePromptAbandoned, event.PromptAbandonedData{}))
	if m.blocks[2].abandoned {
		t.Errorf("an unmatched abandon marker must touch nothing")
	}
}
