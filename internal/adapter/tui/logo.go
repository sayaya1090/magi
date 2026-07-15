package tui

import (
	_ "embed"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/version"
)

//go:embed logo.txt
var magiLogo string

// logoColor is the NERV crimson the MAGI wordmark is painted in (the NERV logo's
// signature red — distinct from the amber UI chrome). Swap to EVA-01 purple
// ("#9B5DE5") or the theme amber (colPrimary) here to retheme the splash.
var logoColor = lipgloss.Color("#E5383B")

// logoBlock is the MAGI wordmark (NERV red) stacked over the build version, center
// aligned. Shared by the startup splash and the plugin prompt so a login screen shows
// the same identity as the main TUI's startup page.
func logoBlock() string {
	art := lipgloss.NewStyle().Foreground(logoColor).Bold(true).Render(strings.TrimRight(magiLogo, "\n"))
	ver := styleToolResult.Render(version.String())
	return lipgloss.JoinVertical(lipgloss.Center, art, "", ver)
}

// splashConsole renders the MAGI tri-console diagram in the deliberation display's
// triangular composition: the first seat's block on top, its stem tee-ing into a
// bus bar that feeds the second and third seats below — the wordmark beneath. The
// frame is NERV red; each seat's nameplate is tinted its member hue. Custom
// councils show their own seat names, center-fitted into the canonical slots.
//
// Geometry (1-based columns; axis = 25): top box spans 18..32 with its ╦ stem on
// the axis; the bus bar spans 11..39 with ╩ on the axis; the side boxes span 3..19
// and 31..47, each with its ╩ centered under a bar end (11 / 39). Every line is
// padded to one common width (artW): splashCompose centers each line independently
// by its width, and equal widths are what keep the diagram's internal alignment
// intact. The wordmark/version sit on the same axis.
func (m *Model) splashConsole() []string {
	frame := lipgloss.NewStyle().Foreground(logoColor).Bold(true)
	names := m.app.CouncilMemberNames()
	seat := func(i, w int) string {
		if i >= len(names) {
			return strings.Repeat(" ", w)
		}
		label := strings.ToUpper(strings.TrimSpace(names[i])) + " - " + strconv.Itoa(i+1)
		if lipgloss.Width(label) > w {
			r := []rune(label)
			for lipgloss.Width(string(r)) > w && len(r) > 0 {
				r = r[:len(r)-1]
			}
			label = string(r)
		}
		pad := w - lipgloss.Width(label)
		left := pad / 2
		return lipgloss.NewStyle().Foreground(m.councilColor(names[i])).Bold(true).
			Render(strings.Repeat(" ", left) + label + strings.Repeat(" ", pad-left))
	}
	f := frame.Render
	const artW, axis = 49, 25
	centerAt := func(s string, w int) string {
		left := axis - (w+1)/2
		if left < 0 {
			left = 0
		}
		return strings.Repeat(" ", left) + s
	}
	sp := strings.Repeat
	bar := func(n int) string { return sp("═", n) }
	lines := []string{
		sp(" ", 16) + f("╔"+bar(15)+"╗"),
		sp(" ", 16) + f("║") + seat(0, 15) + f("║"),
		sp(" ", 16) + f("╚"+bar(7)+"╦"+bar(7)+"╝"),
		sp(" ", 10) + f("╔"+bar(13)+"╩"+bar(13)+"╗"),
		sp(" ", 2) + f("╔"+bar(7)+"╩"+bar(7)+"╗") + sp(" ", 11) + f("╔"+bar(7)+"╩"+bar(7)+"╗"),
		sp(" ", 2) + f("║") + seat(1, 15) + f("║") + sp(" ", 11) + f("║") + seat(2, 15) + f("║"),
		sp(" ", 2) + f("╚"+bar(15)+"╝") + sp(" ", 11) + f("╚"+bar(15)+"╝"),
		"",
		centerAt(f("M  A  G  I"), 10),
		"",
		centerAt(styleToolResult.Render(version.String()), lipgloss.Width(version.String())),
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w < artW {
			lines[i] = l + sp(" ", artW-w)
		}
	}
	return lines
}

// splashIdentity is the dim boot readout under the console diagram: the session's
// model and workdir (home shortened to ~). The seat nameplates live in the diagram
// itself, so this is a single line.
func (m *Model) splashIdentity() string {
	wd := m.workdir
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(wd, home) {
		wd = "~" + strings.TrimPrefix(wd, home)
	}
	return styleToolResult.Render(m.model + "  -  " + wd)
}

// splashView renders the startup splash centered in a width×height area: the
// console diagram (equal-width lines) with the identity readout beneath when
// non-empty. Used as a fallback (e.g. when a modal is open on a fresh session);
// the normal fresh screen uses splashCompose to place the input prompt directly
// beneath the diagram.
func splashView(width, height int, logo []string, identity string) string {
	lines := append([]string(nil), logo...)
	if identity != "" {
		lines = append(lines, "", identity)
	}
	content, _, _ := splashCompose(width, max(1, height), lines, "", "")
	return content
}

// splashCompose renders the fresh-screen content: the wordmark, the identity lines
// (when non-empty), and the input box centered beneath, the group centered in a
// vpw×height area. It returns the content and the viewport-relative (row, col) of
// the input box's first text cell, so the caller can place the real cursor inside
// the box.
func splashCompose(vpw, height int, logo []string, identity, inputBox string) (content string, curRow, curCol int) {
	logoLines := logo
	var idLines []string
	if identity != "" {
		idLines = strings.Split(identity, "\n")
	}
	var boxLines []string
	if inputBox != "" {
		boxLines = strings.Split(inputBox, "\n")
	}
	boxW := lipgloss.Width(inputBox)
	boxLeft := max(0, (vpw-boxW)/2)

	const gap = 1 // one blank row between each splash section
	groupH := func() int {
		h := len(logoLines) + len(boxLines)
		if len(boxLines) > 0 {
			h += gap
		}
		if len(idLines) > 0 {
			h += len(idLines) + gap
		}
		return h
	}
	// The input box must stay fully visible: a multi-line prompt (newlines or soft
	// wraps, up to maxInputRows) can make the group taller than the viewport, and an
	// overflowing compose pushes the box's lower rows — where the user is typing —
	// off screen. Shed decoration before box rows: identity lines first, then the
	// wordmark. (The trailing truncate below is the last resort for a viewport
	// shorter than the box itself.)
	if groupH() > height && len(idLines) > 0 {
		idLines = nil
	}
	if groupH() > height && len(logoLines) > 0 {
		logoLines = nil
	}
	top := max(0, (height-groupH())/2)

	center := func(s string) string {
		return strings.Repeat(" ", max(0, (vpw-lipgloss.Width(s))/2)) + s
	}
	rows := make([]string, 0, height)
	for i := 0; i < top; i++ {
		rows = append(rows, "")
	}
	for _, l := range logoLines {
		rows = append(rows, center(l))
	}
	if len(idLines) > 0 {
		rows = append(rows, "") // gap
		for _, l := range idLines {
			rows = append(rows, center(l))
		}
	}
	if len(boxLines) > 0 {
		rows = append(rows, "") // gap
		for _, l := range boxLines {
			rows = append(rows, center(l))
		}
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	if len(rows) > height {
		rows = rows[:height] // never hand the viewport more rows than it can show
	}
	// Cursor: first text cell of the box = top border + left border + left padding(1).
	curRow = top + len(logoLines) + gap + 1
	if len(idLines) > 0 {
		curRow += len(idLines) + gap
	}
	curCol = boxLeft + 2
	return strings.Join(rows, "\n"), curRow, curCol
}
