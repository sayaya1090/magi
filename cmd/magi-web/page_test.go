package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The browser wears magi's colours, not a second scheme.
//
// A view that invented its own palette would be one more thing to keep in step, and the first time
// somebody retuned the terminal the two would disagree about what magi looks like. This reads the
// terminal's palette out of styles.go and requires every role to appear here with the same value —
// a source check, because nothing in the type system connects a Go colour to a CSS variable.
func TestTheBrowserUsesTheTerminalsPalette(t *testing.T) {
	b, err := os.ReadFile("../../internal/adapter/tui/styles.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	// nervDark and nervLight, each as `"role": "#RRGGBB"` pairs.
	pair := regexp.MustCompile(`"([a-zA-Z]+)":\s*"(#[0-9A-Fa-f]{6})"`)
	for _, name := range []string{"nervDark", "nervLight"} {
		i := strings.Index(src, "var "+name+" = palette{")
		if i < 0 {
			t.Fatalf("%s is not in styles.go any more — this check has lost its subject", name)
		}
		j := strings.Index(src[i:], "\n}")
		if j < 0 {
			t.Fatalf("could not find the end of %s", name)
		}
		roles := pair.FindAllStringSubmatch(src[i:i+j], -1)
		if len(roles) < 10 {
			t.Fatalf("%s parsed to %d roles; the shape of the palette changed", name, len(roles))
		}
		for _, m := range roles {
			role, hex := m[1], m[2]
			// The page declares each role as a custom property of the same name.
			want := "--" + role + ":" + hex
			if !strings.Contains(strings.ReplaceAll(indexHTML, " ", ""), strings.ReplaceAll(want, " ", "")) {
				t.Errorf("%s role %q is %s in the terminal and the page does not declare it (%s)",
					name, role, hex, want)
			}
		}
	}
}

// The page fetches nothing this binary does not serve. A strict answer to "why is there no build
// step": an offline machine sees the same page, and there is no CDN whose outage takes the viewer
// with it.
//
// Not "no links at all" — the manifest and the icon that make it installable on a phone are links,
// to routes this same process answers. So the check is the one that matters: every href and src is
// root-relative, and every path is one this server actually serves.
func TestThePageFetchesNothingItDoesNotServe(t *testing.T) {
	for _, bad := range []string{"http://", "https://", "//cdn", "@import"} {
		if strings.Contains(indexHTML, bad) {
			t.Errorf("the page references something external (%q) — it must be self-contained", bad)
		}
	}
	served := (&server{}).routes()
	// href="…", src="…" in the markup AND url(…) in the CSS: a @font-face pointing at a CDN is the
	// same dependency as a <link>, one layer down, and it was the first thing that wanted to be one.
	ref := regexp.MustCompile(`(?:href|src)="([^"]*)"|url\(([^)]*)\)`)
	for _, m := range ref.FindAllStringSubmatch(indexHTML, -1) {
		u := m[1] + m[2]
		if strings.HasPrefix(u, "data:") {
			continue // carried in the page itself, which is the property this is protecting
		}
		if !strings.HasPrefix(u, "/") {
			t.Errorf("the page references %q, which is not a root-relative path on this server", u)
			continue
		}
		p := strings.SplitN(u, "?", 2)[0]
		if _, ok := served[p]; ok {
			continue
		}
		// A subtree route ("/font/") serves everything under it.
		if i := strings.LastIndexByte(p, '/'); i > 0 {
			if _, ok := served[p[:i+1]]; ok {
				continue
			}
		}
		t.Errorf("the page references %q and this server has no such route", u)
	}
}

// Both themes are declared. A terminal that follows the system theme and a browser stuck in dark
// would be the same disagreement one layer down.
func TestBothThemesAreDeclared(t *testing.T) {
	if !strings.Contains(indexHTML, "prefers-color-scheme: light") {
		t.Error("the page has no light theme; the terminal has one")
	}
	if !strings.Contains(indexHTML, "color-scheme: dark light") {
		t.Error("the page does not tell the browser it supports both, so form controls will not follow")
	}
}

// fontSizePx reads a rule's font size from either spelling: the longhand, or the size slot of the
// `font:` shorthand (which is whatever precedes the / or the family list).
func fontSizePx(rule string) (float64, bool) {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`font-size:([0-9.]+)px`),
		regexp.MustCompile(`[;{]?font:(?:[a-z0-9]+ )*?([0-9.]+)px[/ ]`),
		// A component sets its text size through a token rather than a property, and the reason
		// this check exists — iOS zooming on focus — does not care which of the two says 16px.
		regexp.MustCompile(`input-text-size:\s*([0-9.]+)px`),
	} {
		if m := re.FindStringSubmatch(rule); m != nil {
			var px float64
			if _, err := fmt.Sscanf(m[1], "%g", &px); err == nil {
				return px, true
			}
		}
	}
	return 0, false
}

// The page is both views: a fleet and one agent. It used to be only the second, and the check that
// it stays one document is worth having — the cheap way to add a dashboard is a second page, and
// two pages is how the two views end up looking like different products.
func TestThePageHasBothViews(t *testing.T) {
	for _, want := range []string{`id="fleet"`, `id="log"`, "/fleet", "/events", "pushState", "popstate"} {
		if !strings.Contains(indexHTML, want) {
			t.Errorf("the page is missing %q", want)
		}
	}
}

// Three things a phone needs that a desktop does not notice, each of them a bug you only see on the
// device: an input under 16px makes iOS Safari zoom on focus and never zoom back; a fixed composer
// without the safe-area inset sits under the home indicator; and a viewport without viewport-fit
// leaves the inset at zero, so asking for it changes nothing.
func TestThePageWorksOnAPhone(t *testing.T) {
	flat := strings.ReplaceAll(indexHTML, " ", "")
	if !strings.Contains(flat, "viewport-fit=cover") {
		t.Error("no viewport-fit=cover, so env(safe-area-inset-*) is always zero")
	}
	if !strings.Contains(flat, "env(safe-area-inset-bottom)") {
		t.Error("the composer does not clear the home indicator")
	}
	// Every text input must set a size of its own and it must be at least 16. They inherit the
	// body's 14 otherwise, and 14 is under the threshold that triggers the zoom.
	//
	// Read as a SIZE rather than as one spelling: `font-size:16px` and the `font:` shorthand say
	// the same thing, and a check that knows only one of them fails on a restyle that kept the
	// property it exists to protect.
	//
	// Asked of every size the page states rather than of a list of selectors. The list went stale
	// the moment a field became a component: it still named `.answer input`, which no longer
	// exists, while the rule it was really protecting had moved and was never looked at.
	sizes := regexp.MustCompile(`(?:font-size|input-text-size):([0-9.]+)px`).FindAllStringSubmatch(flat, -1)
	typed := 0
	for _, m := range sizes {
		var px float64
		if _, err := fmt.Sscanf(m[1], "%g", &px); err != nil {
			continue
		}
		if !strings.Contains(m[0], "input-text-size") {
			continue // a label or a heading; only what is typed into triggers the zoom
		}
		typed++
		if px < 16 {
			t.Errorf("a field is %gpx; under 16 iOS Safari zooms the page on focus and does not zoom back", px)
		}
	}
	if typed == 0 {
		t.Error("no field states its own text size, so they inherit the body's 14px and iOS zooms on focus")
	}
	// And nothing typed into is a bare element any more — one that was would inherit that 14px
	// without ever setting an input-text-size for the loop above to find.
	for _, raw := range []string{"createElement('input')", "createElement('textarea')", "<input", "<textarea"} {
		if strings.Contains(indexHTML, raw) {
			t.Errorf("the page still builds a bare %s; it has no text size of its own and iOS zooms on it", raw)
		}
	}
	// Enter must not be hijacked where the return key is the only way to break a line.
	if !strings.Contains(flat, "matchMedia('(hover:none)')") {
		t.Error("Enter is bound the same way on a touch keyboard, leaving no way to type a newline")
	}
}

// Keyboard focus has to be visible.
//
// The fleet is a page of links and the answers are buttons, all reachable with tab — and this
// layout's own vocabulary works against that: it underlines things to press them, so an underline
// cannot also mean "focused", and it shifts border colours by one step, which is not a focus ring
// either. Two of the inputs additionally set outline:none for the mouse, which without a
// :focus-visible rule beside it leaves a keyboard user with nothing at all.
func TestFocusIsVisibleToAKeyboard(t *testing.T) {
	flat := strings.ReplaceAll(indexHTML, " ", "")
	// A blanket rule, so anything focusable added later inherits a ring instead of needing one.
	if !regexp.MustCompile(`(?m)^\s*:focus-visible\s*\{[^}]*outline:`).MatchString(indexHTML) {
		t.Error("there is no blanket :focus-visible outline — every focusable element then needs " +
			"its own, and the next one added will not have it")
	}
	// Every rule that removes the outline must have a :focus-visible rule for the same element.
	rule := regexp.MustCompile(`([.#]?[a-zA-Z][\w.#-]*)(::?[a-z-]+)?\s*\{[^}]*outline:none`)
	for _, m := range rule.FindAllStringSubmatch(indexHTML, -1) {
		sel := m[1]
		if !strings.Contains(flat, strings.ReplaceAll(sel, " ", "")+":focus-visible{outline:") {
			t.Errorf("%s removes its outline and declares no :focus-visible ring, so a keyboard "+
				"user cannot see where they are", sel)
		}
	}
}

// The components take their type from --md-sys-typescale-*-font, not from the ref typeface alone.
//
// Setting only --md-ref-typeface-plain leaves every component label in the library's fallback face
// while the rest of the page is in magi's — the same failure as the colours, one layer over.
func TestEveryMaterialTypeRoleIsMagis(t *testing.T) {
	css := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	for _, role := range []string{"label-small", "label-medium", "label-large",
		"body-small", "body-medium", "body-large", "title-medium", "title-large"} {
		if !strings.Contains(css, "--md-sys-typescale-"+role+"-font:") {
			t.Errorf("--md-sys-typescale-%s-font is never set, so a component using that role "+
				"renders in the library's fallback face", role)
		}
		if !strings.Contains(css, "--md-sys-typescale-"+role+"-size:") {
			t.Errorf("--md-sys-typescale-%s-size is never set", role)
		}
	}
	// The faces are ours; the sizes are the scale's. A literal font name here would be a third
	// place that has to be changed when the typeface does.
	bad := regexp.MustCompile(`--md-sys-typescale-[a-z-]+-font:\s*["']`)
	if m := bad.FindString(css); m != "" {
		t.Errorf("a typescale role names a face directly (%s) instead of pointing at magi's", m)
	}
}

// The components are themed by --md-sys-color-*, and by nothing else.
//
// Setting a few of them per component — which this page did first — leaves every role it did not
// mention drawn in the library's baseline purple. That is what "the colours are the default ones"
// looks like, and it is invisible in a test that only reads magi's own variables.
func TestEveryMaterialRoleIsMagisAndFollowsTheTheme(t *testing.T) {
	css := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	decl := regexp.MustCompile(`--md-sys-color-([a-z-]+):\s*([^;]+);`)
	found := map[string]string{}
	for _, m := range decl.FindAllStringSubmatch(css, -1) {
		found[m[1]] = strings.TrimSpace(m[2])
	}
	// The roles a component reaches for without being asked. A missing one is not a subtle
	// difference: it is Material's default palette on somebody's dashboard.
	for _, role := range []string{
		"primary", "on-primary", "primary-container", "on-primary-container",
		"secondary-container", "on-secondary-container", "error", "on-error",
		"surface", "on-surface", "surface-variant", "on-surface-variant",
		"surface-container", "surface-container-high", "outline", "outline-variant",
		"background", "on-background",
	} {
		if _, ok := found[role]; !ok {
			t.Errorf("--md-sys-color-%s is never set, so components draw it from Material's own "+
				"palette rather than magi's", role)
		}
	}
	// Each points at a magi role rather than carrying a colour of its own. That is what makes the
	// light theme work: it redefines the magi roles, and this layer follows without being told.
	for role, value := range found {
		if strings.HasPrefix(value, "#") && role != "shadow" && role != "scrim" {
			t.Errorf("--md-sys-color-%s is the literal %s — the light theme redefines magi's roles, "+
				"and a hex here stays dark in both", role, value)
		}
	}
}
