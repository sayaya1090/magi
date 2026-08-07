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

	// Diff line backgrounds: a subtle green/red wash behind added/removed code, so
	// the +/- is shown by background while the code keeps its syntax-highlight colors.
	colDiffAddBg color.Color
	colDiffDelBg color.Color

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
	styleQueuedBar  lipgloss.Style // a mid-turn queued user bubble's bar (distinct from ▌)
	styleToolName   lipgloss.Style
	styleToolArgs   lipgloss.Style
	styleToolOK     lipgloss.Style
	styleToolErr    lipgloss.Style
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
	styleKeyLabel   lipgloss.Style // footer key (M3 Label: emphasized)
	styleSelection  lipgloss.Style // mouse text-selection highlight
	styleToast      lipgloss.Style // floating transient notice (toast)
	styleBtn        lipgloss.Style // action button (unselected)
	styleBtnSel     lipgloss.Style // action button (selected)
	styleClickable  lipgloss.Style // inline clickable control (e.g. the "‹ back" breadcrumb)
	styleFoldChip   lipgloss.Style // in-transcript expand/collapse toggle (fold affordance)
)

// palette maps Material Design 3 color roles to hex strings for one mode.
type palette map[string]string

// nervDark/nervLight are the built-in NERV/MAGI defaults: amber chrome on a warm
// dark surface (or burnt orange on warm cream in light). Green=affirmative,
// red=rejected — the MAGI vote colors. A config theme overrides any subset of
// these roles per mode (see SetThemePalettes).
//
// # This is the origin, including for roles the terminal cannot draw
//
// Some roles below are never read by lipgloss: the tonal surface containers, the `on-` pairs, the
// scrim. A terminal has no stacked surfaces to tone and no scrim to lay over them. They are here
// because the web console draws the same product and must not invent its own values for it — one
// thing in two colours is one thing a person has to learn twice.
//
// They are not decoration either. TestTheWebTakesItsColoursFromHere reads this file and the web's
// stylesheet and fails when a shared role disagrees, so every entry here has a reader.
var nervDark = palette{
	"primary": "#FF7A1A", "accent": "#5CD8E6", "muted": "#C9C2B8", "outline": "#5A5048",
	"error": "#F2B8B5", "success": "#86EFAC", "surface": "#211B14",
	"primaryContainer": "#4A2E0B", "outlineVariant": "#463E34", "warn": "#FFD479",
	// The MAGI — amber / cyan / coral, the NERV-console hues.
	"melchior": "#FFB454", "balthasar": "#5CD8E6", "casper": "#FF8A8A",

	// The page and the text on it. The terminal has its own default background and the TUI has
	// always let it show through, so these two are here for the web to read rather than for
	// lipgloss to paint — see the note above the type.
	"bg": "#14110D", "fg": "#E8E2D8",

	// The `on-` half of each container role. M3's colours are PAIRS: the pair is what guarantees
	// contrast, and a scheme that names only the containers has role names and nothing else.
	"onPrimary": "#2A1500", "onPrimaryContainer": "#FFD9B8", "onError": "#3A0A08",
	"onSurface": "#E8E2D8", "onSurfaceVariant": "#C9C2B8",

	// Tonal surfaces. M3 expresses height with TONE rather than shadow, and in a dark scheme the
	// layers get brighter as they rise. A terminal cannot draw them and does not try; they are the
	// origin for the web's cards, bars and sheets.
	"surfaceDim":             "#14110D",
	"surfaceContainerLowest": "#0F0D0A", "surfaceContainerLow": "#1B1712",
	"surfaceContainer": "#211B14", "surfaceContainerHigh": "#2B251C",
	"surfaceContainerHighest": "#352E24",

	"scrim": "#000000", "shadow": "#000000",
}
var nervLight = palette{
	"primary": "#B45309", "accent": "#0E7490", "muted": "#4A453C", "outline": "#8A7E6E",
	"error": "#B3261E", "success": "#15803D", "surface": "#F5EEE3",
	"primaryContainer": "#F8D9A8", "outlineVariant": "#D8CFC0", "warn": "#92600A",
	"melchior": "#B45309", "balthasar": "#0E7490", "casper": "#B3261E",

	"bg": "#FBF8F3", "fg": "#221D16",

	"onPrimary": "#FFFFFF", "onPrimaryContainer": "#3A1B00", "onError": "#FFFFFF",
	"onSurface": "#221D16", "onSurfaceVariant": "#4A453C",

	// The layers INVERT here: a light scheme gets darker as it rises. Built as its own ramp rather
	// than by dimming the dark one — a light scheme has less headroom, and this palette has been
	// caught before with eight of thirteen dimmed pairs under AA, the worst at 2.47:1.
	"surfaceDim":             "#EFE9DF",
	"surfaceContainerLowest": "#FFFFFF", "surfaceContainerLow": "#F7F3EC",
	"surfaceContainer": "#F2ECE2", "surfaceContainerHigh": "#ECE5D9",
	"surfaceContainerHighest": "#E6DED1",

	"scrim": "#000000", "shadow": "#000000",
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

	// Diff washes: light tints on light theme, deep low-chroma tints on dark — both
	// quiet enough that syntax-highlighted code stays legible on top.
	colDiffAddBg = ld(lipgloss.Color("#DCF0E0"), lipgloss.Color("#173A23"))
	colDiffDelBg = ld(lipgloss.Color("#FBE3E1"), lipgloss.Color("#3C1D1D"))

	n := lipgloss.NewStyle
	styleHeader = n().Foreground(colMuted).Padding(0, 1)
	styleBrand = n().Foreground(colPrimary).Bold(true)
	styleUserLabel = n().Foreground(colAccent).Bold(true)
	styleAsstLabel = n().Foreground(colPrimary).Bold(true)
	styleBar = n().Foreground(colOutline)
	styleQueuedBar = n().Foreground(colWarn)
	styleToolName = n().Foreground(colPrimary)
	styleToolArgs = n().Foreground(colMuted)
	styleToolOK = n().Foreground(colSuccess)
	styleToolErr = n().Foreground(colError)
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
	// Inline clickable control: a filled accent pill so an actionable element (the
	// "‹ back" breadcrumb) reads as a BUTTON, not as plain accent-colored text. Uses
	// the accent (interactive) hue, matching the toast/other tappable chrome.
	styleClickable = n().Foreground(colSurface).Background(colAccent).Bold(true).Padding(0, 1)
	// In-transcript fold toggle (expand/collapse): a low-emphasis filled chip (badge
	// container) so the clickable part of an otherwise-dim fold line reads as tappable,
	// without a wall of bright accent down the transcript.
	styleFoldChip = n().Foreground(colAccent).Background(colPrimCont).Padding(0, 1)
}
