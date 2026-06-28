package tui

import (
	_ "embed"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/version"
)

//go:embed logo.txt
var magiLogo string

// splashView renders the startup splash centered in a width×height area: the MAGI
// wordmark in amber + the build version. Shown until the first message.
func splashView(width, height int) string {
	art := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(strings.TrimRight(magiLogo, "\n"))
	ver := styleToolResult.Render(version.String())
	block := lipgloss.JoinVertical(lipgloss.Center, art, "", ver)
	return lipgloss.Place(width, max(1, height), lipgloss.Center, lipgloss.Center, block)
}
