package main

import (
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
	ref := regexp.MustCompile(`(?:href|src)="([^"]*)"`)
	for _, m := range ref.FindAllStringSubmatch(indexHTML, -1) {
		u := m[1]
		if !strings.HasPrefix(u, "/") {
			t.Errorf("the page references %q, which is not a root-relative path on this server", u)
			continue
		}
		if _, ok := served[strings.SplitN(u, "?", 2)[0]]; !ok {
			t.Errorf("the page references %q and this server has no such route", u)
		}
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
	// The textarea must set a font-size of its own: it inherits 14px from body otherwise, and 14
	// is under the threshold that triggers the zoom.
	ta := strings.Index(flat, "textarea{")
	if ta < 0 {
		t.Fatal("no textarea rule in the page")
	}
	if end := strings.Index(flat[ta:], "}"); end < 0 || !strings.Contains(flat[ta:ta+end], "font-size:16px") {
		t.Error("the textarea is under 16px, so iOS Safari will zoom the page on focus")
	}
	// Enter must not be hijacked where the return key is the only way to break a line.
	if !strings.Contains(flat, "matchMedia('(hover:none)')") {
		t.Error("Enter is bound the same way on a touch keyboard, leaving no way to type a newline")
	}
}
