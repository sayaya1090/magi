package tui

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every filled thing in the terminal is legible, in both themes.
//
// The web has had this check for weeks and the terminal drew from the same palette without it, so
// the arithmetic was never done here — and it was wrong in four places. The terminal used
// colSurface as its on-colour for anything filled: a selected row, a button, a toast. That is the
// colour a thing sits ON, not the one written on top of it, and the palette has carried the real
// on-roles all along with only the web reading them. Measured before the fix: surface on primary
// was 4.36:1 in the light theme, surface on outline 3.09, accent on primary-container 3.95.
//
// Read out of the style definitions rather than listed here, so a pair added later is checked
// without anybody remembering to add it.
func TestEveryFilledPairInTheTerminalClearsAA(t *testing.T) {
	src := readStylesSource(t)
	dark, light := paletteLiteral(t, src, "nervDark"), paletteLiteral(t, src, "nervLight")

	// colX in the styles maps to a palette key; this is the one place the two vocabularies meet.
	role := map[string]string{
		"colPrimary": "primary", "colAccent": "accent", "colSurface": "surface",
		"colPrimCont": "primaryContainer", "colOutline": "outline", "colMuted": "muted",
		"colError": "error", "colSuccess": "success", "colWarn": "warn",
		"colOnPrimary": "onPrimary", "colOnPrimCont": "onPrimaryContainer",
		"colOnSurface": "onSurface", "colSurfContHigh": "surfaceContainerHigh",
		"colSurfContLow": "surfaceContainerLow", "colFg": "fg", "colBg": "bg",
	}
	// ⚠ Not BorderForeground, which ends in the same word and is not text. A border is a
	// non-text contrast and answers to 3:1, so it is asked separately below — the first version of
	// this check called a 3.23:1 border a failing label.
	pair := regexp.MustCompile(`(?:^|[^r])Foreground\((col\w+)\)\.Background\((col\w+)\)`)
	border := regexp.MustCompile(`BorderForeground\((col\w+)\)\.Background\((col\w+)\)`)
	found := 0
	for _, m := range border.FindAllStringSubmatch(src, -1) {
		fgKey, bgKey := role[m[1]], role[m[2]]
		if fgKey == "" || bgKey == "" {
			continue
		}
		for _, c := range []struct {
			what string
			p    map[string]string
		}{{"dark", dark}, {"light", light}} {
			if got := contrast(t, c.p[fgKey], c.p[bgKey]); got < 3 {
				t.Errorf("the %s border on %s is %.2f:1 in the %s theme — a boundary needs 3",
					fgKey, bgKey, got, c.what)
			}
		}
	}
	for _, m := range pair.FindAllStringSubmatch(src, -1) {
		fgKey, bgKey := role[m[1]], role[m[2]]
		if fgKey == "" || bgKey == "" {
			continue // a colour with no palette twin: the diff washes, the councillors
		}
		found++
		for _, c := range []struct {
			what string
			p    map[string]string
		}{{"dark", dark}, {"light", light}} {
			got := contrast(t, c.p[fgKey], c.p[bgKey])
			if got < 4.5 {
				t.Errorf("%s on %s is %.2f:1 in the %s theme — text needs 4.5",
					fgKey, bgKey, got, c.what)
			}
		}
	}
	// A floor, so a refactor that renames the styles cannot leave this measuring nothing.
	if found < 5 {
		t.Errorf("only %d filled pairs were found in the styles; this check has lost its subject", found)
	}
}

func readStylesSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("styles.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// paletteLiteral reads one of the palette maps out of the source, so the test sees the values a
// reader sees rather than whatever applyTheme happens to have resolved.
func paletteLiteral(t *testing.T, src, name string) map[string]string {
	t.Helper()
	at := strings.Index(src, "var "+name+" = palette{")
	if at < 0 {
		t.Fatalf("no %s palette in styles.go", name)
	}
	end := strings.Index(src[at:], "\n}")
	if end < 0 {
		t.Fatalf("the %s palette is unterminated", name)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`"([a-zA-Z]+)":\s*"(#[0-9A-Fa-f]{6})"`).
		FindAllStringSubmatch(src[at:at+end], -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatalf("the %s palette declares no colours", name)
	}
	return out
}

func contrast(t *testing.T, a, b string) float64 {
	t.Helper()
	if a == "" || b == "" {
		t.Fatalf("a colour in this pair is missing from the palette: %q / %q", a, b)
	}
	x, y := luminance(t, a), luminance(t, b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	ch := func(i int) float64 {
		v, err := strconv.ParseInt(hex[i:i+2], 16, 32)
		if err != nil {
			t.Fatalf("%q is not a colour: %v", hex, err)
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(1) + 0.7152*ch(3) + 0.0722*ch(5)
}
