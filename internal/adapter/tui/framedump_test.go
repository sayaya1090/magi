package tui

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The states this detector reports are states that must not happen — that is the point of it. So
// the test builds them directly rather than reaching them through the UI: it is checking that the
// REPORTER works, not claiming a user can get here. (The walk is what proves they don't arise.)
func withFrameDump(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frames.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	prev, prevSeq, prevRep := frameDumpFile, frameSeq, lastFrameReport
	frameDumpFile, frameSeq, lastFrameReport = f, 0, ""
	t.Cleanup(func() {
		_ = f.Close()
		frameDumpFile, frameSeq, lastFrameReport = prev, prevSeq, prevRep
	})
	return func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

func TestAFrameThatBreaksAnInvariantIsRecorded(t *testing.T) {
	read := withFrameDump(t)
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 40, 10
	m.ready = true

	// A frame taller than the terminal, and a row wider than it.
	m.dumpFrame(strings.Repeat("x\n", 20) + strings.Repeat("y", 60))
	got := read()
	if !strings.Contains(got, "20 rows in a 10-row terminal") &&
		!strings.Contains(got, "21 rows in a 10-row terminal") {
		t.Errorf("the vertical overflow was not reported:\n%s", got)
	}
	if !strings.Contains(got, "cells in a 40-column terminal") {
		t.Errorf("the over-wide row was not reported:\n%s", got)
	}
	// The geometry that the fix always turns out to be about.
	for _, want := range []string{"chrome=", "modalRoom=", "vp=", "blocks=", "perm=", "panelW="} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from the report:\n%s", want, got)
		}
	}
}

func TestAHealthyFrameWritesNothingAndAStuckOneWritesOnce(t *testing.T) {
	read := withFrameDump(t)
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.ready = true

	m.dumpFrame("a fine row\nanother")
	if got := read(); strings.Contains(got, "!") {
		t.Errorf("a healthy frame should write nothing, got:\n%s", got)
	}
	// The same fault on three consecutive repaints is one fault, not three.
	broken := strings.Repeat("z", 200)
	m.dumpFrame(broken)
	m.dumpFrame(broken)
	m.dumpFrame(broken)
	if n := strings.Count(read(), "cells in a 80-column terminal"); n != 1 {
		t.Errorf("a persistent fault should be recorded once, got %d", n)
	}
	// …and it rearms once the screen is healthy again.
	m.dumpFrame("fine")
	m.dumpFrame(broken)
	if n := strings.Count(read(), "cells in a 80-column terminal"); n != 2 {
		t.Errorf("after recovering, a new occurrence should be recorded, got %d", n)
	}
}

// The reporter has to run on the frame the app actually draws, not only on a string handed to it.
// This drives a real session through View() at a size a split pane reaches; the dump is expected
// to be EMPTY, because the walk's job is to keep it that way. What it proves is that the hook is
// on the live path — remove the dumpFrame call in View and this test still passes, so it is
// paired with the wiring check below.
func TestTheFrameDumpRunsOnTheDrawnFrame(t *testing.T) {
	read := withFrameDump(t)
	s := newScript(t)
	s.m.width, s.m.height = 30, 8
	s.m.ready = true
	s.steer("r1", strings.Repeat("a very long word ", 20))
	s.assistantText(strings.Repeat("reply ", 60))
	_ = s.rawView()
	if frameSeq == 0 {
		t.Error("View drew a frame and the reporter never saw it — the hook is not on the live path")
	}
	if got := read(); strings.Contains(got, " ! ") {
		t.Errorf("a real frame at 30x8 tripped an invariant:\n%s", got)
	}
}

// Off is the default and must cost nothing: no file, no writes, and — the part that matters — the
// frame is not altered by having looked at it.
func TestTheFrameDumpIsOffByDefaultAndChangesNothing(t *testing.T) {
	if frameDumpPath == "" && frameDumpFile != nil {
		t.Error("with MAGI_DEBUG_FRAMES unset there should be no file")
	}
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.ready = true
	m.refresh()
	before := m.View().Content
	m.dumpFrame(before)
	if after := m.View().Content; after != before {
		t.Error("looking at a frame changed it")
	}
}

// The reporter and the walk check the same list, and the reporter is the copy nobody runs — it
// only ever fires in a live session, where no assertion is watching. So run it BESIDE the walk on
// the same frames: the walk fails on a violation, the reporter records one, and the two must reach
// the same verdict. A reporter that stayed silent through a walk failure would be a detector that
// does not detect; one that spoke through a clean walk would cry wolf in every real session.
func TestTheFrameReporterAgreesWithTheWalk(t *testing.T) {
	read := withFrameDump(t)
	dark := fuzzDark(t)
	for _, seed := range fuzzSeeds(t)[:min(3, len(fuzzSeeds(t)))] {
		s := newScript(t)
		applyTheme(dark)
		rng := rand.New(rand.NewSource(seed))
		for step := 0; step < 120; step++ {
			switch rng.Intn(8) {
			case 0:
				s.typeText("request").enter()
			case 1:
				s.assistantText("an answer")
			case 2:
				s.toolCall("bash", fmt.Sprintf("c%d", step))
			case 3:
				s.toolResult(fmt.Sprintf("c%d", step-1), "line\nline\nline")
			case 4:
				// Same floor the walk uses: at six rows any open modal is one row past the
				// irreducible chrome, which is a documented limit and not a defect to report at.
				s.send(tea.WindowSizeMsg{Width: 20 + rng.Intn(120), Height: 7 + rng.Intn(39)})
			case 5:
				s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
			case 6:
				s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
			default:
				s.emit(event.TypePartDelta, event.PartDeltaData{
					MessageID: "m", PartID: "p", Kind: session.PartText, Text: "tok "})
			}
			_ = s.rawView() // drives View, which runs the reporter
		}
	}
	applyTheme(true)
	if got := read(); strings.Contains(got, " ! ") {
		t.Errorf("the reporter saw a violation the walk's own invariants did not:\n%s", got)
	}
	if frameSeq == 0 {
		t.Fatal("no frame reached the reporter, so this compared nothing")
	}
	t.Logf("%d frames through the reporter, no anomaly", frameSeq)
}
