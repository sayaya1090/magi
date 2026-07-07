package tui

import (
	_ "embed"
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

// splashView renders the startup splash centered in a width×height area: the MAGI
// wordmark in NERV red + the build version. Shown until the first message.
func splashView(width, height int) string {
	return lipgloss.Place(width, max(1, height), lipgloss.Center, lipgloss.Center, logoBlock())
}
