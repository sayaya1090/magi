package main

import "github.com/sayaya1090/magi/internal/adapter/tui"

// ThemePalettes answers the colour roles this daemon's [theme] config resolves to — the same
// resolution the terminal paints with — so viewers draw diagrams with the person's palette.
func (d daemonEngine) ThemePalettes() (dark, light map[string]string) {
	return tui.ResolvedPalette(true, d.theme.Dark), tui.ResolvedPalette(false, d.theme.Light)
}
