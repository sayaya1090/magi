package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A written record of frames that came out WRONG, for diagnosing a live session.
//
// The random walk checks these same invariants after every action and prints the offending frame,
// which is how every layout defect in this package has been found. What it cannot reach is a real
// session: a terminal at a size nobody scripted, a plan panel holding a real run's steps, an
// emoji in a real filename. In one of those the only report is "the screen looks wrong", and the
// state that produced it is gone by the time anyone asks — the next repaint overwrites it.
//
// So the same checks run at the end of View when MAGI_DEBUG_FRAMES is set, and a frame that trips
// one is appended to the file with the numbers behind it. Quiet otherwise: a healthy session
// writes nothing, and a persistent fault writes once rather than once per repaint, because a
// sixty-frames-a-second log of the same broken row is not evidence, it is noise.
//
// This does NOT change what is drawn. It reads the frame that was already built.

// frameDumpPath is where anomalous frames are appended; empty disables the whole thing.
var frameDumpPath = strings.TrimSpace(os.Getenv("MAGI_DEBUG_FRAMES"))

var frameDumpFile = func() *os.File {
	if frameDumpPath == "" {
		return nil
	}
	f, err := os.Create(frameDumpPath)
	if err != nil {
		return nil
	}
	fmt.Fprintf(f, "# magi frame anomalies — started %s\n", time.Now().Format(time.RFC3339))
	return f
}()

// frameSeq counts every frame, so the report can say WHICH one — a fault that appears at frame
// 900 after a resize is a different story from one present since the first paint.
var frameSeq int

// lastFrameReport is the previous frame's anomaly text, so an unchanged fault is not repeated.
var lastFrameReport string

// frameAnomalies returns what is wrong with the frame just built, or nothing.
//
// The checks are the walk's, in the walk's words. Deliberately NOT shared code with the test: the
// test's copies assert and fail, these describe and continue, and a shared version would have to
// do both badly. What must not drift is the list, so a check added there belongs here.
func (m *Model) frameAnomalies(content string) []string {
	var out []string
	rows := strings.Split(content, "\n")
	if m.height > 0 && len(rows) > m.height {
		out = append(out, fmt.Sprintf("frame is %d rows in a %d-row terminal", len(rows), m.height))
	}
	// The widest row, measured on its own content: the vertical join pads every row to the widest
	// one, so the first over-wide row is almost never the culprit.
	if m.width > 0 {
		worst, worstW, worstI := "", 0, -1
		for i, r := range rows {
			if w := ansi.StringWidth(strings.TrimRight(ansi.Strip(r), " ")); w > worstW {
				worst, worstW, worstI = r, w, i
			}
		}
		if worstW > m.width {
			out = append(out, fmt.Sprintf("row %d draws %d cells in a %d-column terminal: %q",
				worstI, worstW, m.width, ansi.Strip(worst)))
		}
	}
	if len(m.blockLineStart) != len(m.blocks) {
		out = append(out, fmt.Sprintf("%d start lines for %d blocks", len(m.blockLineStart), len(m.blocks)))
	}
	if len(m.cache) > len(m.blocks) {
		out = append(out, fmt.Sprintf("%d cached renders for %d blocks", len(m.cache), len(m.blocks)))
	}
	if off, h := m.vp.YOffset(), m.vp.TotalLineCount(); off < 0 || (h > 0 && off > h) {
		out = append(out, fmt.Sprintf("viewport offset %d in %d lines of content", off, h))
	}
	for _, h := range m.searchHits {
		if h < 0 || h >= len(m.contentPlain) {
			out = append(out, fmt.Sprintf("search hit %d outside the %d-line transcript", h, len(m.contentPlain)))
			break
		}
	}
	return out
}

// dumpFrame appends the frame's anomalies and the geometry behind them. No-op unless enabled.
//
// The geometry is the part that is otherwise unrecoverable: chrome, modalRoom and the viewport
// are what the fix turns out to be about every time, and by the time a user reports "the screen
// looked wrong" they are three repaints in the past.
func (m *Model) dumpFrame(content string) {
	if frameDumpFile == nil {
		return
	}
	frameSeq++
	bad := m.frameAnomalies(content)
	if len(bad) == 0 {
		lastFrameReport = ""
		return
	}
	report := strings.Join(bad, "\n")
	if report == lastFrameReport {
		return // the same fault still on screen — already recorded
	}
	lastFrameReport = report

	var b strings.Builder
	fmt.Fprintf(&b, "\n=== frame %d  %s  term %dx%d\n", frameSeq, time.Now().Format("15:04:05.000"), m.width, m.height)
	for _, s := range bad {
		fmt.Fprintf(&b, "  ! %s\n", s)
	}
	fmt.Fprintf(&b, "  chrome=%d modalRoom=%d vp=%d/%d@%d blocks=%d cache=%d lines=%d\n",
		m.chromeHeight(), m.modalRoom(), m.vp.Height(), m.vp.TotalLineCount(), m.vp.YOffset(),
		len(m.blocks), len(m.cache), len(m.contentPlain))
	fmt.Fprintf(&b, "  perm=%v quest=%v palette=%d route=%v search=%v resume=%v zoom=%v council=%v panelW=%d\n",
		m.perm != nil, m.quest != nil, len(m.paletteMatches()), m.routing, m.searching,
		m.resuming, m.zoom, m.councilDetail != nil, m.panelW)
	if _, err := frameDumpFile.WriteString(b.String()); err == nil {
		_ = frameDumpFile.Sync()
	}
}
