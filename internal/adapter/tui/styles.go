package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Material Design 3 color *roles*, themed after NERV/MAGI (amber primary, cyan
// accent on a warm dark surface — the Evangelion command-terminal look that gives
// magi its name and its three-councillor signature). lipgloss v2 has no
// AdaptiveColor, so colors are resolved once for the active theme via applyTheme;
// styles are then plain (non-adaptive) values.
var (
	colPrimary  color.Color // NERV amber — main emphasis
	colAccent   color.Color // cyan — secondary emphasis
	colMuted    color.Color // on-surface-variant — secondary text
	colOutline  color.Color // borders / dividers
	colError    color.Color
	colSuccess  color.Color
	colSurface  color.Color // elevated surface tint
	colPrimCont color.Color // primary-container — low-emphasis selected/active fill
	colOutlVar  color.Color // outline-variant — dividers
	colWarn     color.Color // caution (e.g. "allow" / YOLO permission)

	// Council member hues (the MAGI): distinct, theme-overridable colors for
	// Melchior/Balthasar/Casper. Custom or extra members fall back to agentPalette
	// (see councilColor).
	colMelchior  color.Color
	colBalthasar color.Color
	colCasper    color.Color

	// agentPalette gives each subagent a distinct, stable color (M3 tonal set).
	// Resolved per theme in applyTheme. Used for pane borders, the breadcrumb,
	// the header badge, and transcript name highlights.
	agentPalette []color.Color
)

var (
	styleHeader     lipgloss.Style
	styleBrand      lipgloss.Style
	styleUserLabel  lipgloss.Style
	styleAsstLabel  lipgloss.Style
	styleBar        lipgloss.Style
	styleToolName   lipgloss.Style
	styleToolArgs   lipgloss.Style
	styleToolOK     lipgloss.Style
	styleToolErr    lipgloss.Style
	styleWarn       lipgloss.Style // caution accent (e.g. a council "revise" verdict)
	styleToolResult lipgloss.Style
	styleError      lipgloss.Style
	styleInput      lipgloss.Style
	styleInputFocus lipgloss.Style
	styleFooter     lipgloss.Style
	stylePermBox    lipgloss.Style
	stylePermTitle  lipgloss.Style
	stylePalBox     lipgloss.Style
	stylePalSelRow  lipgloss.Style
	stylePalName    lipgloss.Style
	styleThink      lipgloss.Style
	styleDivider    lipgloss.Style
	styleBadge      lipgloss.Style // running-subagent count badge
	styleKeyLabel   lipgloss.Style // footer key (M3 Label: emphasized)
	styleSelection  lipgloss.Style // mouse text-selection highlight
	styleToast      lipgloss.Style // floating transient notice (toast)
	styleBtn        lipgloss.Style // action button (unselected)
	styleBtnSel     lipgloss.Style // action button (selected)
)

// palette maps Material Design 3 color roles to hex strings for one mode.
type palette map[string]string

// nervDark/nervLight are the built-in NERV/MAGI defaults: amber chrome on a warm
// dark surface (or burnt orange on warm cream in light). Green=affirmative,
// red=rejected — the MAGI vote colors. A config theme overrides any subset of
// these roles per mode (see SetThemePalettes).
var nervDark = palette{
	"primary": "#FF7A1A", "accent": "#5CD8E6", "muted": "#C9C2B8", "outline": "#5A5048",
	"error": "#F2B8B5", "success": "#86EFAC", "surface": "#211B14",
	"primaryContainer": "#4A2E0B", "outlineVariant": "#463E34", "warn": "#FFD479",
	// The MAGI — amber / cyan / coral, the NERV-console hues.
	"melchior": "#FFB454", "balthasar": "#5CD8E6", "casper": "#FF8A8A",
}
var nervLight = palette{
	"primary": "#B45309", "accent": "#0E7490", "muted": "#4A453C", "outline": "#8A7E6E",
	"error": "#B3261E", "success": "#15803D", "surface": "#F5EEE3",
	"primaryContainer": "#F8D9A8", "outlineVariant": "#D8CFC0", "warn": "#92600A",
	"melchior": "#B45309", "balthasar": "#0E7490", "casper": "#B3261E",
}

// themeDarkOverride/themeLightOverride hold config-supplied color overrides
// (nil = defaults only). Set once from config before the first applyTheme.
var themeDarkOverride, themeLightOverride palette

// SetThemePalettes installs config-provided color overrides, keyed by role name,
// merged over the built-in NERV/MAGI defaults. An empty value or unknown role is
// ignored. Call before applyTheme (e.g. from main, after loading config).
func SetThemePalettes(dark, light map[string]string) {
	themeDarkOverride = dark
	themeLightOverride = light
}

// resolvePalette returns the active palette for the mode: built-in defaults
// overlaid with any config override (non-empty values only).
func resolvePalette(isDark bool) palette {
	base, over := nervLight, themeLightOverride
	if isDark {
		base, over = nervDark, themeDarkOverride
	}
	p := make(palette, len(base))
	for k, v := range base {
		p[k] = v
	}
	for k, v := range over {
		if v != "" {
			p[k] = v
		}
	}
	return p
}

// applyTheme resolves the color roles for the active theme and (re)builds all
// styles. Call once before rendering.
func applyTheme(isDark bool) {
	ld := lipgloss.LightDark(isDark)
	p := resolvePalette(isDark)
	col := func(role string) color.Color { return lipgloss.Color(p[role]) }
	colPrimary = col("primary")
	colAccent = col("accent")
	colMuted = col("muted")
	colOutline = col("outline")
	colError = col("error")
	colSuccess = col("success")
	colSurface = col("surface")
	colPrimCont = col("primaryContainer")
	colOutlVar = col("outlineVariant")
	colWarn = col("warn")
	colMelchior = col("melchior")
	colBalthasar = col("balthasar")
	colCasper = col("casper")

	// Distinct per-subagent hues (left=light theme tone, right=dark theme tone).
	// Chosen to stay legible on both surfaces and apart from amber/cyan chrome.
	agentPalette = []color.Color{
		ld(lipgloss.Color("#6A4FB0"), lipgloss.Color("#C9B6FF")), // violet
		ld(lipgloss.Color("#4F46E5"), lipgloss.Color("#A5B4FC")), // indigo (was amber — now reserved for primary)
		ld(lipgloss.Color("#1E6F50"), lipgloss.Color("#7FE3B2")), // green
		ld(lipgloss.Color("#1565A8"), lipgloss.Color("#8FC8FF")), // blue
		ld(lipgloss.Color("#9A2D6B"), lipgloss.Color("#FF9CD2")), // magenta
		ld(lipgloss.Color("#856500"), lipgloss.Color("#E6D072")), // gold
		ld(lipgloss.Color("#0F6E73"), lipgloss.Color("#6FE0E6")), // cyan
		ld(lipgloss.Color("#A33A3A"), lipgloss.Color("#FF9E9E")), // coral
	}

	n := lipgloss.NewStyle
	styleHeader = n().Foreground(colMuted).Padding(0, 1)
	styleBrand = n().Foreground(colPrimary).Bold(true)
	styleUserLabel = n().Foreground(colAccent).Bold(true)
	styleAsstLabel = n().Foreground(colPrimary).Bold(true)
	styleBar = n().Foreground(colOutline)
	styleToolName = n().Foreground(colPrimary)
	styleToolArgs = n().Foreground(colMuted)
	styleToolOK = n().Foreground(colSuccess)
	styleToolErr = n().Foreground(colError)
	styleWarn = n().Foreground(colWarn)
	styleToolResult = n().Foreground(colMuted)
	styleError = n().Foreground(colError).Bold(true)
	styleInput = n().Border(lipgloss.RoundedBorder()).BorderForeground(colOutline).Padding(0, 1)
	styleInputFocus = n().Border(lipgloss.RoundedBorder()).BorderForeground(colPrimary).Padding(0, 1)
	styleFooter = n().Foreground(colMuted).Padding(0, 1)
	stylePermBox = n().Border(lipgloss.RoundedBorder()).BorderForeground(colPrimary).Background(colSurface).Padding(0, 2)
	stylePermTitle = n().Foreground(colPrimary).Bold(true)
	stylePalBox = n().Border(lipgloss.RoundedBorder()).BorderForeground(colOutline).Background(colSurface).Padding(0, 1)
	// Selected row: clear amber fill with contrasting text (reads as a selection,
	// not a near-white block, in both light and dark).
	stylePalSelRow = n().Foreground(colSurface).Background(colPrimary).Bold(true)
	stylePalName = n().Foreground(colAccent)
	styleThink = n().Foreground(colMuted).Italic(true)
	styleDivider = n().Foreground(colOutlVar)
	// Badge: filled primary-container pill (M3 badge).
	styleBadge = n().Foreground(colPrimary).Background(colPrimCont).Bold(true).Padding(0, 1)
	// Footer key (M3 Label): accent + bold for emphasis.
	styleKeyLabel = n().Foreground(colAccent).Bold(true)
	// Selection highlight: primary-container fill (reads as a selection band).
	styleSelection = n().Foreground(colSurface).Background(colPrimary)
	// Toast: a floating accent chip overlaid in a corner, auto-dismissed.
	styleToast = n().Foreground(colSurface).Background(colAccent).Bold(true).Padding(0, 1)
	// Action button (e.g. the profile form's Save): a filled pill, brighter when
	// selected, so it reads as a button distinct from the field rows.
	styleBtn = n().Foreground(colSurface).Background(colOutline).Bold(true).Padding(0, 2)
	styleBtnSel = n().Foreground(colSurface).Background(colPrimary).Bold(true).Padding(0, 2)
}
