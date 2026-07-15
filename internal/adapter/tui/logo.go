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

// splashIdentity renders the two identity lines shown under the wordmark on the
// startup splash: the council nameplates — each configured seat in its member hue,
// numbered like the MAGI consoles (MELCHIOR·1  BALTHASAR·2  CASPER·3) — and a dim
// boot readout of the session (model · workdir, home shortened to ~). The
// nameplates use the CONFIGURED seats, so a custom council shows its own names.
func (m *Model) splashIdentity() string {
	var seats []string
	for i, name := range m.app.CouncilMemberNames() {
		st := lipgloss.NewStyle().Foreground(m.councilColor(name)).Bold(true)
		seats = append(seats, st.Render(strings.ToUpper(strings.TrimSpace(name))+"·"+strconv.Itoa(i+1)))
	}
	plates := strings.Join(seats, "   ")
	wd := m.workdir
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(wd, home) {
		wd = "~" + strings.TrimPrefix(wd, home)
	}
	readout := styleToolResult.Render(m.model + " · " + wd)
	return lipgloss.JoinVertical(lipgloss.Center, plates, readout)
}

// splashView renders the startup splash centered in a width×height area: the MAGI
// wordmark in NERV red + the build version, with the identity lines (council
// nameplates + boot readout) beneath when non-empty. Used as a fallback (e.g. when
// a modal is open on a fresh session); the normal fresh screen uses splashCompose
// to place the input prompt directly beneath the wordmark.
func splashView(width, height int, identity string) string {
	block := logoBlock()
	if identity != "" {
		block = lipgloss.JoinVertical(lipgloss.Center, block, "", identity)
	}
	return lipgloss.Place(width, max(1, height), lipgloss.Center, lipgloss.Center, block)
}

// splashCompose renders the fresh-screen content: the wordmark, the identity lines
// (when non-empty), and the input box centered beneath, the group centered in a
// vpw×height area. It returns the content and the viewport-relative (row, col) of
// the input box's first text cell, so the caller can place the real cursor inside
// the box.
func splashCompose(vpw, height int, identity, inputBox string) (content string, curRow, curCol int) {
	logoLines := strings.Split(logoBlock(), "\n")
	var idLines []string
	if identity != "" {
		idLines = strings.Split(identity, "\n")
	}
	boxLines := strings.Split(inputBox, "\n")
	boxW := lipgloss.Width(inputBox)
	boxLeft := max(0, (vpw-boxW)/2)

	const gap = 1 // one blank row between each splash section
	groupH := len(logoLines) + gap + len(boxLines)
	if len(idLines) > 0 {
		groupH += len(idLines) + gap
	}
	top := max(0, (height-groupH)/2)

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
	rows = append(rows, "") // gap
	for _, l := range boxLines {
		rows = append(rows, center(l))
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	// Cursor: first text cell of the box = top border + left border + left padding(1).
	curRow = top + len(logoLines) + gap + 1
	if len(idLines) > 0 {
		curRow += len(idLines) + gap
	}
	curCol = boxLeft + 2
	return strings.Join(rows, "\n"), curRow, curCol
}
