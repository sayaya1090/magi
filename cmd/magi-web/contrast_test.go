package main

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Text on this page has to be readable, including the parts it deliberately quietens.
//
// An editorial layout gets its hierarchy from dimming secondary text — the path under a name, the
// label beside a transcript row, a stopped agent's whole entry — and the arithmetic is easy to get
// wrong twice over: the muted role is already lowered against the page, and the light theme has
// less headroom than the dark one. Measured when this check was written, eight of thirteen dimmed
// pairs were under WCAG AA and the worst was 2.47:1, which is a label most people cannot read.
//
// The roles themselves come from the terminal (styles.go, pinned by another test), so what is
// checked here is what the page DOES with them: the opacity it applies, in both themes, against
// the background it actually sits on.
func TestDimmedTextStillClearsAA(t *testing.T) {
	dark, light := themeRoles(t)
	// role, opacity, and what it is — the cases the layout dims.
	for _, c := range []struct {
		what    string
		role    string
		opacity float64
	}{
		{"the state kicker", "muted", 1.0},
		{"a workspace path", "muted", 0.9},
		{"a card's meta line", "muted", 0.8},
		{"a transcript label", "muted", 0.8},
		{"a tool row's label", "accent", 1.0},
		{"a user row's label", "primary", 1.0},
		{"reasoning text", "muted", 0.8},
		{"the session id", "muted", 0.8},
		{"the composer's placeholder", "muted", 0.8},
		{"a stopped agent's entry", "muted", 0.8},
		{"the empty state", "muted", 1.0},
	} {
		for theme, roles := range map[string]map[string]string{"dark": dark, "light": light} {
			got := contrast(blend(roles[c.role], roles["bg"], c.opacity), roles["bg"])
			if got < 4.5 {
				t.Errorf("%s in the %s theme is %.2f:1 at opacity %.2f — AA wants 4.5 for text this size",
					c.what, theme, got, c.opacity)
			}
		}
	}
}

// Nothing may be dimmed past the point where the role itself could carry it. A role at full
// strength that is already under AA is a colour problem; a role dimmed under it is this page's.
func TestNoOpacityInTheStylesheetGoesBelowWhatItsRoleAllows(t *testing.T) {
	dark, light := themeRoles(t)
	// Every `opacity:N` in the stylesheet, with the colour declared alongside it in the same rule.
	// The selector comes along, because whether an opacity dims TEXT depends on what it is on.
	rule := regexp.MustCompile(`(?s)([^{}]*)\{([^{}]*opacity:[^{}]*)\}`)
	colour := regexp.MustCompile(`color:var\(--([a-zA-Z]+)\)`)
	op := regexp.MustCompile(`opacity:([0-9.]+)`)
	css := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	// Keyframes first. This check is about text that RESTS at an unreadable opacity; a keyframe is a
	// tenth of a second on the way somewhere, and "from { opacity:0 }" is what an entrance IS. A
	// keyframe that ended dimmed would still be caught, because whatever it left the element at is
	// also written in an ordinary rule.
	css = regexp.MustCompile(`(?s)@keyframes[^{]*\{.*?\}\s*\}`).ReplaceAllString(css, "")
	checked := 0
	for _, m := range rule.FindAllStringSubmatch(css, -1) {
		selector, body := m[1], m[2]
		om := op.FindStringSubmatch(body)
		if om == nil {
			continue
		}
		// A state layer is not dimmed text. M3 expresses hover and press by laying the on- colour
		// over a surface at a fixed opacity, and it lives on a pseudo-element that has no text of
		// its own (content:'') — measuring one as a label reports 1.00:1 for an overlay that is
		// SUPPOSED to start invisible. Skipped by selector rather than by name, so a rule that
		// dims real text can never be excused by what it happens to contain.
		if strings.Contains(selector, "::after") || strings.Contains(selector, "::before") {
			continue
		}
		// Hidden is not dimmed. A rule that takes the element out of the page in the same breath —
		// visibility:hidden, display:none — is not showing anybody text at 0%; it is showing them
		// nothing, which is the point. (A pane fades to 0 as it closes and is then hidden, and
		// without this the check read that as unreadable text left on the screen.) Narrow on
		// purpose: opacity alone, with the element still in the page, is still caught.
		if strings.Contains(body, "visibility:hidden") || strings.Contains(body, "display:none") {
			continue
		}
		role := "muted"
		if cm := colour.FindStringSubmatch(body); cm != nil {
			role = cm[1]
		}
		// A rule that dims without naming a colour dims a CONTAINER, and everything inside it goes
		// with it — a stopped agent's whole entry, label and path and all. The dimmest thing that
		// can be in there is the muted role, so that is what it is measured against; the check
		// missed this shape entirely until a mutation walked through it.
		a, err := strconv.ParseFloat(om[1], 64)
		if err != nil {
			t.Errorf("unreadable opacity %q", om[1])
			continue
		}
		checked++
		for theme, roles := range map[string]map[string]string{"dark": dark, "light": light} {
			hex, ok := roles[role]
			if !ok {
				continue // a colour that is not one of the themed roles
			}
			if got := contrast(blend(hex, roles["bg"], a), roles["bg"]); got < 4.5 {
				t.Errorf("%s at opacity %s is %.2f:1 in the %s theme:\n%s",
					role, om[1], got, theme, strings.TrimSpace(body))
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d dimmed rules were found; the parser has lost its subject", checked)
	}
}

// themeRoles reads both palettes out of the page.
func themeRoles(t *testing.T) (dark, light map[string]string) {
	t.Helper()
	lightAt := strings.Index(indexHTML, "@media (prefers-color-scheme: light)")
	endAt := strings.Index(indexHTML, "* { box-sizing")
	if lightAt < 0 || endAt < 0 {
		t.Fatal("the page's palette blocks are not where this check expects them")
	}
	// The reference layer wears its namespace now, and the role is what follows it — the pattern
	// that read a bare name found none and reported every role missing from both themes.
	pair := regexp.MustCompile(`--magi-ref-([A-Za-z]+):(#[0-9A-Fa-f]{6})`)
	read := func(s string) map[string]string {
		out := map[string]string{}
		for _, m := range pair.FindAllStringSubmatch(s, -1) {
			out[m[1]] = m[2]
		}
		return out
	}
	dark, light = read(indexHTML[:lightAt]), read(indexHTML[lightAt:endAt])
	for _, need := range []string{"bg", "fg", "muted", "primary", "accent"} {
		if dark[need] == "" || light[need] == "" {
			t.Fatalf("role %q is missing from one of the themes", need)
		}
	}
	return dark, light
}

// blend is fg drawn at opacity a over bg, which is what the browser composites.
func blend(fg, bg string, a float64) [3]float64 {
	f, b := channels(fg), channels(bg)
	var out [3]float64
	for i := range out {
		out[i] = f[i]*a + b[i]*(1-a)
	}
	return out
}

func channels(hex string) [3]float64 {
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, _ := strconv.ParseUint(hex[1+i*2:3+i*2], 16, 8)
		out[i] = float64(v)
	}
	return out
}

// contrast is the WCAG ratio between a composited colour and a background.
func contrast(fg [3]float64, bgHex string) float64 {
	l1, l2 := relLum(fg), relLum(channels(bgHex))
	hi, lo := math.Max(l1, l2), math.Min(l1, l2)
	return (hi + 0.05) / (lo + 0.05)
}

func relLum(c [3]float64) float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}
