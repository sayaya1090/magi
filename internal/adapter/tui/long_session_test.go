package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A long session is the one this UI is actually used in — a task runs for an hour and the
// transcript is thousands of blocks — and every scripted test in this package builds a handful.
// The random walk reaches a few hundred and then wipes them on a session switch, so nothing here
// has ever rendered a transcript at the size a real run produces.
//
// What is at stake is arithmetic that is fine at ten blocks: the prefix cache indexed by block,
// blockLineStart, the scroll offset, and the screen↔content mapping the mouse uses. This drives
// them at three thousand.
func longSession(t *testing.T, w, h, n int) *script {
	t.Helper()
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	for i := 0; i < n; i++ {
		switch i % 5 {
		case 0:
			s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
				event.PromptSubmittedData{MessageID: fmt.Sprintf("r%d", i),
					Parts: []session.Part{{Kind: session.PartText, Text: fmt.Sprintf("request %d", i)}}})
		case 1:
			s.assistantText(fmt.Sprintf("answer %d, which runs on for a sentence or two so the wrap has something to do", i))
		case 2:
			s.toolCall("bash", fmt.Sprintf("c%d", i))
		case 3:
			s.toolResult(fmt.Sprintf("c%d", i), fmt.Sprintf("output %d\nsecond line\nthird line", i))
		default:
			s.emit(event.TypePartDelta, event.PartDeltaData{
				MessageID: fmt.Sprintf("m%d", i), PartID: "p", Kind: session.PartReasoning, Text: "thinking "})
			s.emit(event.TypeTurnFinished, event.TurnFinishedData{})
		}
	}
	// Lay the viewport out before measuring it: the guard below reads TotalLineCount, which is
	// filled by refresh, and asking first reports zero for a transcript that is perfectly fine.
	s.m.refresh()
	// A guard on the fixture itself: if the events stopped becoming blocks, every assertion below
	// would pass over a short transcript and this file would be testing nothing at the one thing
	// it exists for.
	if len(s.m.blocks) < n/3 {
		t.Fatalf("%d events produced only %d blocks — this is not a long session", n, len(s.m.blocks))
	}
	if s.m.vp.TotalLineCount() < n/3 {
		t.Fatalf("%d blocks render only %d viewport lines", len(s.m.blocks), s.m.vp.TotalLineCount())
	}
	return s
}

// The frame stays inside the terminal at three thousand blocks, at every size, wherever the
// viewport is scrolled to.
func TestALongTranscriptStillFitsTheTerminal(t *testing.T) {
	applyTheme(true)
	rng := rand.New(rand.NewSource(20260801))
	for _, c := range []struct{ w, h int }{{80, 24}, {40, 12}, {120, 50}, {30, 9}} {
		s := longSession(t, c.w, c.h, 3000)
		for i := 0; i < 12; i++ {
			switch rng.Intn(4) {
			case 0:
				s.m.vp.GotoTop()
			case 1:
				s.m.vp.GotoBottom()
			case 2:
				s.m.vp.SetYOffset(rng.Intn(max(1, s.m.vp.TotalLineCount())))
			default:
				s.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
			}
			lines := strings.Split(s.rawView(), "\n")
			if len(lines) > c.h {
				t.Fatalf("w=%d h=%d: the frame is %d rows at offset %d",
					c.w, c.h, len(lines), s.m.vp.YOffset())
			}
			for j, l := range lines {
				trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
				if got := lipgloss.Width(trimmed); got > c.w {
					t.Fatalf("w=%d h=%d: row %d draws %d cells: %q", c.w, c.h, j, got, trimmed)
				}
			}
		}
	}
}

// The screen↔content mapping the mouse uses stays in bounds over a transcript this long. It reads
// blockLineStart and contentPlain, both of which are rebuilt every render — an index that outran
// either would panic on a click, and a click is not a place to find that out.
func TestClickingAnywhereInALongTranscriptIsInBounds(t *testing.T) {
	applyTheme(true)
	rng := rand.New(rand.NewSource(20260802))
	s := longSession(t, 80, 24, 3000)
	for round := 0; round < 12; round++ {
		// Lay the frame out once per offset, then click into it. Re-rendering a three-thousand
		// block transcript after every single click cost 124s under -race — the whole package's
		// budget — and bought nothing: what is under test is the coordinate mapping against a
		// laid-out frame, and the frame does not change because a click missed.
		s.m.vp.SetYOffset(rng.Intn(max(1, s.m.vp.TotalLineCount())))
		s.m.refresh()
		_ = s.rawView()
		for i := 0; i < 25; i++ {
			x, y := rng.Intn(80), rng.Intn(24)
			s.m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
			s.m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
		}
		// One render after the round, so a click that DID change something (a thought toggled,
		// the cache truncated behind it) still has to draw.
		s.m.refresh()
		_ = s.rawView()
	}
}

// The prefix cache is indexed by block, and at this length a stale or misaligned entry is the way
// a long session would start showing an old answer. Every finalized block still renders to what it
// says now.
func TestTheCacheStaysAlignedOverALongTranscript(t *testing.T) {
	applyTheme(true)
	s := longSession(t, 100, 30, 3000)
	_ = s.rawView()
	if len(s.m.cache) == 0 {
		t.Fatal("nothing was cached, so nothing here is under test")
	}
	checked := 0
	for i := 0; i < len(s.m.cache) && i < len(s.m.blocks); i += 97 {
		if s.m.running && s.m.blocks[i].reqID == s.m.turnReqID && s.m.blocks[i].kind == blockUser {
			continue // the in-flight bubble carries a spinner and is meant to differ
		}
		checked++
		if fresh := s.m.renderBlock(s.m.blocks[i]); fresh != s.m.cache[i] {
			t.Fatalf("block %d of %d is cached stale:\ncached: %q\nfresh:  %q",
				i, len(s.m.blocks), ansiSeq.ReplaceAllString(s.m.cache[i], ""),
				ansiSeq.ReplaceAllString(fresh, ""))
		}
	}
	if checked == 0 {
		t.Error("no block was compared, so this verified nothing")
	}
	// …and a resize re-keys the whole cache rather than leaving it at the old width.
	s.send(tea.WindowSizeMsg{Width: 60, Height: 30})
	_ = s.rawView()
	for i := 0; i < len(s.m.cache) && i < len(s.m.blocks); i += 97 {
		if fresh := s.m.renderBlock(s.m.blocks[i]); fresh != s.m.cache[i] {
			t.Fatalf("after a resize, block %d is cached at the old width:\ncached: %q\nfresh:  %q",
				i, ansiSeq.ReplaceAllString(s.m.cache[i], ""), ansiSeq.ReplaceAllString(fresh, ""))
		}
	}
}
