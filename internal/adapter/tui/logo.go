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

// splashView renders the startup splash in a width×height area: the MAGI wordmark
// in NERV red + the build version, horizontally centered and biased below the
// vertical center so it sits just above the input prompt — grouping the logo with
// the prompt on the fresh screen instead of floating it in an empty viewport.
// Shown until the first message.
func splashView(width, height int) string {
	return lipgloss.Place(width, max(1, height), lipgloss.Center, splashVPos, logoBlock())
}

// splashVPos biases the wordmark below center so its base meets the prompt. Note
// lipgloss.PlaceVertical splits the gap as above=gap*(1-pos): a SMALLER pos means
// MORE space above, i.e. the content sits lower. 0.32 ≈ two-thirds down.
const splashVPos lipgloss.Position = 0.32
