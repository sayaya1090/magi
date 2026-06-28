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

// splashView renders the startup splash centered in a width×height area: the MAGI
// wordmark in NERV red + the build version. Shown until the first message.
func splashView(width, height int) string {
	art := lipgloss.NewStyle().Foreground(logoColor).Bold(true).Render(strings.TrimRight(magiLogo, "\n"))
	ver := styleToolResult.Render(version.String())
	block := lipgloss.JoinVertical(lipgloss.Center, art, "", ver)
	return lipgloss.Place(width, max(1, height), lipgloss.Center, lipgloss.Center, block)
}
