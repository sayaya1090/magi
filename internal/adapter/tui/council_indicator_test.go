package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func ev(t *testing.T, ty event.Type, data any) event.Event {
	t.Helper()
	d, _ := json.Marshal(data)
	return event.Event{Type: ty, Data: d}
}

// Council events surface as transcript milestones plus a live header chip; the
// chip clears when the turn finishes.
func TestCouncilIndicator(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	// A routine convened (no signals) arms the chip but adds NO transcript line —
	// it would repeat the same members/rule every round (noise); the verdict and
	// decided lines carry the round's actual information.
	before := len(m.blocks)
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
		Task: "fix the parser bug", Report: "fixed the EOF handling",
	}))
	if m.councilRound != 1 {
		t.Fatalf("councilRound = %d, want 1", m.councilRound)
	}
	if len(m.blocks) != before {
		t.Fatalf("routine convened should not add a transcript line: %+v", m.blocks[len(m.blocks)-1])
	}
	// With deterministic signals the convened line IS the evidence display — shown.
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior"}, Rule: "majority", Signals: []string{"self-check: fabrication"},
		Task: "fix the parser bug", Report: "fixed the EOF handling",
	}))
	if last := m.blocks[len(m.blocks)-1]; last.kind != blockInfo || !strings.Contains(last.text, "self-check: fabrication") {
		t.Fatalf("convened with signals should be a transcript line: %+v", last)
	}

	// Live polling updates the chip member.
	m.applyEvent(ev(t, event.TypeCouncilDeliberating, event.CouncilDeliberatingData{Round: 1, Member: "Balthasar", State: "asking"}))
	if m.councilMember != "Balthasar" {
		t.Fatalf("councilMember = %q, want Balthasar", m.councilMember)
	}

	// A verdict rides the LIVE area while the round is open (the transcript is inline, so a row
	// that grew as members landed left a copy of every earlier version in the scrollback) and
	// becomes a COMPACT block carrying the full vote data when the round closes. The detail
	// (lens/rationale/feedback) shows only in the modal opened by clicking it.
	m.applyEvent(ev(t, event.TypeCouncilVerdict, event.CouncilVerdictData{
		Round: 1, Member: "Melchior", Lens: "correctness", Decision: "continue",
		Rationale: "the parser drops the trailing newline", Feedback: "handle EOF without a newline",
	}))
	if len(m.liveVerdicts) != 1 {
		t.Fatalf("an open round's vote should be live, got %d", len(m.liveVerdicts))
	}
	for _, b := range m.blocks {
		if b.kind == blockCouncilVerdict {
			t.Fatal("an open round must not commit a row to the transcript")
		}
	}
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Continue: 1},
	}))
	var v block
	for _, b := range m.blocks {
		if b.kind == blockCouncilVerdict {
			v = b
		}
	}
	if v.kind != blockCouncilVerdict || len(v.councilVerdicts) != 1 {
		t.Fatalf("verdict should be a compact council row carrying its data, got kind=%d", v.kind)
	}
	if v.councilVerdicts[0].Member != "Melchior" || v.councilVerdicts[0].Rationale == "" {
		t.Fatalf("verdict block should carry the full vote data: %+v", v.councilVerdicts)
	}
	// The compact render shows member + decision but NOT the rationale. A termination
	// "continue" is shown as "reject" (it's a rejection, not progress).
	m.width = 80
	compact := m.renderBlock(v)
	if !strings.Contains(compact, "Melchior") || !strings.Contains(compact, "reject") {
		t.Fatalf("compact line should show member + reject (termination continue): %q", compact)
	}
	if strings.Contains(compact, "continue") {
		t.Fatalf("termination verdict should read 'reject', not 'continue': %q", compact)
	}
	if strings.Contains(compact, "trailing newline") {
		t.Fatalf("compact line must NOT include the rationale: %q", compact)
	}
	// Clicking opens the full-screen detail with the full reasoning AND the round's
	// evidence (what the members saw): task + report flow through convened→verdict.
	if v.evidence == "" || !strings.Contains(v.evidence, "fix the parser bug") {
		t.Fatalf("verdict block should carry the round's evidence, got %q", v.evidence)
	}
	m.councilDetail = &v.councilVerdicts[0]
	m.councilDetailEvidence = v.evidence
	detail := m.renderCouncilDetail(80)
	if !strings.Contains(detail, "correctness") ||
		!strings.Contains(detail, "the parser drops the trailing newline") ||
		!strings.Contains(detail, "handle EOF without a newline") {
		t.Fatalf("detail missing lens/rationale/feedback: %q", detail)
	}
	if !strings.Contains(detail, "fix the parser bug") || !strings.Contains(detail, "fixed the EOF handling") {
		t.Fatalf("detail should show the evidence the council saw (task/report): %q", detail)
	}
	m.councilDetail = nil

	// The decision line shows the tally and that feedback was injected; polling clears.
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue),
		Tally:    council.Breakdown{Done: 1, Continue: 2},
		Feedback: "add the missing test",
	}))
	if m.councilMember != "" {
		t.Fatalf("councilMember should clear after a decision, got %q", m.councilMember)
	}
	last := m.blocks[len(m.blocks)-1]
	// Outcome word is "reject" (termination), tally counts and feedback still shown.
	if !strings.Contains(last.text, "reject") || !strings.Contains(last.text, "1 done / 2 continue") || !strings.Contains(last.text, "feedback injected") {
		t.Fatalf("decided line missing reject/tally/feedback: %q", last.text)
	}

	// Turn end clears the council indicator (chip disappears).
	m.applyEvent(event.Event{Type: event.TypeTurnFinished})
	if m.councilRound != 0 || m.councilMember != "" {
		t.Fatalf("council indicator should clear on turn finish: round=%d member=%q", m.councilRound, m.councilMember)
	}
}

// A round's three member verdicts collapse into ONE block rendered on a single line,
// and clicking a member's column opens that member's detail.
func TestCouncilVerdictsOnOneLine(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width = 120
	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
		Task: "review the docs", Plan: "1. [solo] x",
	}))
	for _, v := range []event.CouncilVerdictData{
		{Round: 1, Member: "Melchior", Lens: "correctness", Decision: "done", Confidence: 0.97, Rationale: "sound approach"},
		{Round: 1, Member: "Balthasar", Lens: "verification", Decision: "abstain", Confidence: 0.99, Rationale: "nothing to verify yet"},
		{Round: 1, Member: "Casper", Lens: "completeness", Decision: "done", Confidence: 0.99, Rationale: "covers the task"},
	} {
		m.applyEvent(ev(t, event.TypeCouncilVerdict, v))
	}

	// Nothing is committed while the round is open.
	if len(m.liveVerdicts) != 3 {
		t.Fatalf("the open round should hold three live votes, got %d", len(m.liveVerdicts))
	}
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Done), Tally: council.Breakdown{Done: 2, Abstain: 1},
	}))
	// All three share one block (one rendered line, no embedded newline).
	var blk block
	for _, b := range m.blocks {
		if b.kind == blockCouncilVerdict {
			blk = b
		}
	}
	if blk.kind != blockCouncilVerdict || len(blk.councilVerdicts) != 3 {
		t.Fatalf("a round's votes should collapse into one 3-member block, got %d blocks-worth", len(blk.councilVerdicts))
	}
	line := m.renderBlock(blk)
	if strings.Contains(line, "\n") {
		t.Fatalf("the verdict row must be a single line:\n%q", line)
	}
	for _, name := range []string{"Melchior", "Balthasar", "Casper"} {
		if !strings.Contains(line, name) {
			t.Fatalf("one-line row should include %s: %q", name, line)
		}
	}

	// A continue (reject/revise) vote prints its reason on a line below the row; the
	// member row itself stays a single line.
	mm2 := newTestModel(t)
	m2 := &mm2
	m2.width = 120
	for _, v := range []event.CouncilVerdictData{
		{Round: 1, Member: "Melchior", Lens: "correctness", Decision: "done", Confidence: 0.9},
		{Round: 1, Member: "Balthasar", Lens: "verification", Decision: "continue", Confidence: 0.91, Feedback: "no test covers --dry-run"},
	} {
		m2.applyEvent(ev(t, event.TypeCouncilVerdict, v))
	}
	m2.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: string(council.Continue), Tally: council.Breakdown{Done: 1, Continue: 1},
	}))
	var blk2 block
	for _, b := range m2.blocks {
		if b.kind == blockCouncilVerdict {
			blk2 = b
		}
	}
	out := m2.renderBlock(blk2)
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("a reject reason should render below the row:\n%q", out)
	}
	if strings.Contains(rows[0], "\n") || !strings.Contains(rows[0], "Balthasar") {
		t.Fatalf("first line should be the one-line member row: %q", rows[0])
	}
	if !strings.Contains(out, "no test covers --dry-run") {
		t.Fatalf("reject reason missing from render:\n%q", out)
	}

	// Clicking in the third member's column range opens Casper's detail. The row is committed
	// when the round closes, so it is no longer the last block — the outcome line follows it.
	i := -1
	for j, b := range m.blocks {
		if b.kind == blockCouncilVerdict {
			i = j
		}
	}
	if i < 0 {
		t.Fatal("no committed verdict row to click")
	}
	m.blockLineStart = make([]int, len(m.blocks))
	for j := range m.blockLineStart {
		m.blockLineStart[j] = j
	}
	x := 2
	x += ansi.StringWidth(councilMemberPlain(blk.councilVerdicts[0])) + ansi.StringWidth(councilRowSep)
	x += ansi.StringWidth(councilMemberPlain(blk.councilVerdicts[1])) + ansi.StringWidth(councilRowSep)
	m.selAL, m.selAC = i, x+1 // a column inside Casper's segment
	if !m.openCouncilDetailAt(i) || m.councilDetail == nil || m.councilDetail.Member != "Casper" {
		t.Fatalf("click in the third column should open Casper's detail, got %+v", m.councilDetail)
	}
}

func TestCouncilWaitingIndicator(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	m.applyEvent(ev(t, event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
	}))
	if m.councilRound != 1 {
		t.Fatalf("convened must arm the round, got %d", m.councilRound)
	}
	if got := councilWaitLabel(); !strings.Contains(got, "카운슬") || !strings.Contains(got, "대기") {
		t.Fatalf("wait label should name the council wait, got %q", got)
	}

	// A decision ends the open round → the footer reverts to "working…".
	m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 1, Decision: "done", Tally: council.Breakdown{Done: 3},
	}))
	if m.councilRound != 0 {
		t.Fatalf("councilRound should clear after a decision, got %d", m.councilRound)
	}
}
func TestCollapsePreReviewReport(t *testing.T) {
	seedReport := func(t *testing.T) (*Model, int) {
		t.Helper()
		mm := newTestModel(t)
		m := &mm
		m.blocks = append(m.blocks,
			block{kind: blockUser, text: "검수해줘"},
			block{kind: blockAssistant, text: "여기 전체 보고서입니다 …(긴 내용)"},
		)
		return m, len(m.blocks) - 1
	}

	// report → review continue → the report is NOT folded yet (revision hasn't landed)…
	// then the revision's PartText arrives → the pre-review report folds to a stub and the
	// revision appends in full below it.
	t.Run("continue folds only when the revision lands", func(t *testing.T) {
		m, reportIdx := seedReport(t)
		m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
			Round: 1, Decision: "continue", Tally: council.Breakdown{Done: 1, Continue: 2},
			Feedback: "누락된 항목 추가",
		}))
		if m.blocks[reportIdx].kind != blockAssistant {
			t.Fatalf("report must stay intact until the revision lands, got kind=%d", m.blocks[reportIdx].kind)
		}
		if !m.reviewFoldNext {
			t.Fatalf("a review continue should arm the deferred fold")
		}
		m.onPartAppended(part(session.PartText, "개정된 최종 보고서"), time.Time{})
		if m.blocks[reportIdx].kind != blockInfo || !strings.Contains(m.blocks[reportIdx].text, "접힘") {
			t.Fatalf("pre-review report should fold to a stub once revised: %+v", m.blocks[reportIdx])
		}
		last := m.blocks[len(m.blocks)-1]
		if last.kind != blockAssistant || last.text != "개정된 최종 보고서" {
			t.Fatalf("the revision should append in full below the stub, got %+v", last)
		}
		if m.reviewFoldNext {
			t.Fatalf("the deferred fold should disarm after firing")
		}
	})

	// Edge: the revision comes back identical to the pre-review report. The model didn't
	// actually change anything, so the original must stay EXACTLY as it is — not folded,
	// not re-printed. A blink-out-then-identical-reappear reads as a glitch; the duplicate
	// re-send is dropped silently and nothing is appended.
	t.Run("identical revision keeps the original untouched", func(t *testing.T) {
		m, reportIdx := seedReport(t)
		orig := m.blocks[reportIdx].text
		m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
			Round: 1, Decision: "continue", Tally: council.Breakdown{Done: 1, Continue: 2},
			Feedback: "다시 확인",
		}))
		before := len(m.blocks)
		m.onPartAppended(part(session.PartText, orig), time.Time{}) // same text back
		if m.blocks[reportIdx].kind != blockAssistant || m.blocks[reportIdx].text != orig {
			t.Fatalf("original report must stay untouched, got %+v", m.blocks[reportIdx])
		}
		if len(m.blocks) != before {
			t.Fatalf("identical re-send must add no block (no fold, no re-print), got %d new", len(m.blocks)-before)
		}
	})

	// The MAJOR-fix guarantee: a review continue whose revision never materializes
	// (user interrupt / error / tool-only forced finish) must leave the original report
	// fully readable, not a stub pointing at a non-existent "final below".
	t.Run("interrupted revision preserves the report", func(t *testing.T) {
		m, reportIdx := seedReport(t)
		m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
			Round: 1, Decision: "continue", Tally: council.Breakdown{Done: 1, Continue: 2},
			Feedback: "누락된 항목 추가",
		}))
		m.onTurnFinished(event.Event{Type: event.TypeTurnFinished}) // no PartText ever arrived
		if m.blocks[reportIdx].kind != blockAssistant {
			t.Fatalf("an aborted revision must keep the original report, got kind=%d", m.blocks[reportIdx].kind)
		}
		if m.reviewFoldNext {
			t.Fatalf("turn end should disarm the deferred fold")
		}
	})

	// A "done" review keeps the report (it IS the final result — nothing follows).
	t.Run("review done keeps the report", func(t *testing.T) {
		m, reportIdx := seedReport(t)
		m.applyEvent(ev(t, event.TypeCouncilDecided, event.CouncilDecidedData{
			Round: 1, Decision: "done", Tally: council.Breakdown{Done: 3},
		}))
		if m.reviewFoldNext {
			t.Fatalf("a done review must not arm the fold")
		}
		if m.blocks[reportIdx].kind != blockAssistant {
			t.Fatalf("a done review must keep the report, got kind=%d", m.blocks[reportIdx].kind)
		}
	})
}

// The convened milestone line (contract gate / plan audit / termination round) shows each council
// member's name in ITS OWN hue, not one flat string — coloredMembers embeds per-member ANSI while the
// stripped text stays the plain "A, B, C" (so widths/hit-tests are unaffected).
func TestCouncilRoundMembersColored(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	members := []string{"Melchior", "Balthasar", "Casper"}

	colored := m.coloredMembers(members)
	if got := ansi.Strip(colored); got != "Melchior, Balthasar, Casper" {
		t.Fatalf("stripped coloredMembers = %q, want the plain join", got)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Error("coloredMembers must embed ANSI color, unlike a flat strings.Join")
	}

	// A convened round earns a milestone line when it carries round-specific signals, and that
	// line carries the colored members.
	before := len(m.blocks)
	m.onCouncilConvened(event.CouncilConvenedData{
		Round: 1, Members: members, Rule: "majority", Signals: []string{"verify: pass"},
	})
	if len(m.blocks) == before {
		t.Fatal("a convened round with signals must add a milestone line")
	}
	line := m.blocks[len(m.blocks)-1].text
	if !strings.Contains(line, "\x1b[") {
		t.Error("the council round milestone line must render members in color")
	}
	if s := ansi.Strip(line); !strings.Contains(s, "Melchior") || !strings.Contains(s, "council round 1") {
		t.Errorf("milestone line lost its content: %q", s)
	}
}
