//go:build !windows

package tui

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// probeAmbiguousWidth prints one ambiguous test glyph (probeGlyph, the panel
// border │) at column 1 and
// asks the terminal for the cursor position (CPR); the reported column minus one
// is the glyph's real cell width. Returns (width, true) only on a clean
// round-trip. It requires a bounded read (SetReadDeadline) so a terminal that
// ignores CPR times out instead of hanging; on platforms/handles that can't set
// a deadline it skips entirely. The 150ms raw-mode window can, in the worst case,
// swallow a keystroke typed during startup — acceptable for a one-shot probe, and
// avoidable via MAGI_WIDTH_PROBE=0.
func probeAmbiguousWidth(out, in *os.File) (int, bool) {
	if out == nil || in == nil {
		return 0, false
	}
	if !term.IsTerminal(out.Fd()) || !term.IsTerminal(in.Fd()) {
		return 0, false
	}
	if err := in.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		return 0, false
	}
	defer in.SetReadDeadline(time.Time{})

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return 0, false
	}
	defer term.Restore(in.Fd(), state)

	// Save cursor, go to column 1, emit the test glyph, request cursor position.
	if _, err := io.WriteString(out, "\x1b7\r"+probeGlyph+"\x1b[6n"); err != nil {
		return 0, false
	}
	col, ok := readCPRColumn(in)
	// Restore the cursor and erase the test glyph, whatever happened above.
	_, _ = io.WriteString(out, "\x1b8\x1b[K")
	if !ok || col < 1 {
		return 0, false
	}
	return col - 1, true
}

// probeEmojiWidth measures the real cell width of emojiProbeGlyph the same way as
// probeAmbiguousWidth — emit at column 1, read the CPR column, restore+erase — in
// its own bounded raw-mode/deadline window. Returns (width, true) only on a clean
// round-trip; any uncertainty (non-tty, no CPR answer) yields ok=false so the
// default (wide, no correction) stands.
func probeEmojiWidth(out, in *os.File) (int, bool) {
	if out == nil || in == nil {
		return 0, false
	}
	if !term.IsTerminal(out.Fd()) || !term.IsTerminal(in.Fd()) {
		return 0, false
	}
	if err := in.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		return 0, false
	}
	defer in.SetReadDeadline(time.Time{})

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return 0, false
	}
	defer term.Restore(in.Fd(), state)

	if _, err := io.WriteString(out, "\x1b7\r"+emojiProbeGlyph+"\x1b[6n"); err != nil {
		return 0, false
	}
	col, ok := readCPRColumn(in)
	_, _ = io.WriteString(out, "\x1b8\x1b[K") // restore + erase, whatever happened
	if !ok || col < 1 {
		return 0, false
	}
	return col - 1, true
}

// probeDecorWidths measures each decorative glyph (decorGlyphs) the same way as
// probeAmbiguousWidth — emit at column 1, read the CPR column, erase — but in a
// SINGLE raw-mode/deadline window so the whole batch is bounded (not N×150ms).
// Returns a rune→isWide map. If the terminal doesn't answer CPR the batch is
// abandoned (ok=false) rather than half-measured. An empty map with ok=true means
// "measured, all narrow" — a valid result that yields no correction.
func probeDecorWidths(out, in *os.File) (map[rune]bool, bool) {
	if out == nil || in == nil {
		return nil, false
	}
	if !term.IsTerminal(out.Fd()) || !term.IsTerminal(in.Fd()) {
		return nil, false
	}
	if err := in.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		return nil, false
	}
	defer in.SetReadDeadline(time.Time{})

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return nil, false
	}
	defer term.Restore(in.Fd(), state)

	m := make(map[rune]bool, len(decorGlyphs))
	for _, r := range decorGlyphs {
		if _, err := io.WriteString(out, "\x1b7\r"+string(r)+"\x1b[6n"); err != nil {
			return nil, false
		}
		col, ok := readCPRColumn(in)
		_, _ = io.WriteString(out, "\x1b8\x1b[K") // restore + erase, whatever happened
		if !ok || col < 1 {
			return nil, false // CPR unsupported — keep default narrow
		}
		if col-1 >= 2 {
			m[r] = true
		}
	}
	return m, true
}

// readCPRColumn reads a Cursor-Position-Report (ESC [ row ; col R) and returns
// the column. The read is bounded by the caller's deadline; a malformed or
// missing report yields ok=false.
func readCPRColumn(in io.Reader) (col int, ok bool) {
	buf := make([]byte, 0, 32)
	one := make([]byte, 1)
	for len(buf) < cap(buf) {
		n, err := in.Read(one)
		if n > 0 {
			buf = append(buf, one[0])
			if one[0] == 'R' {
				break
			}
		}
		if err != nil {
			break
		}
	}
	s := string(buf)
	i := strings.IndexByte(s, '[')
	j := strings.IndexByte(s, 'R')
	if i < 0 || j <= i {
		return 0, false
	}
	parts := strings.SplitN(s[i+1:j], ";", 2)
	if len(parts) != 2 {
		return 0, false
	}
	c, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, false
	}
	return c, true
}
