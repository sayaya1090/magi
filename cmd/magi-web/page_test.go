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

// The page carries no external reference. A strict answer to "why is there no build step": nothing
// is fetched at load, so the binary is the whole thing and an offline machine sees the same page.
func TestThePageFetchesNothing(t *testing.T) {
	for _, bad := range []string{"http://", "https://", "//cdn", "<script src", "<link ", "@import"} {
		if strings.Contains(indexHTML, bad) {
			t.Errorf("the page references something external (%q) — it must be self-contained", bad)
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
