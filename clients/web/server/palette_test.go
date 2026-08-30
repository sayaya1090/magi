package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The two surfaces draw the same product, so they must draw it in the same colours.
//
// The palette's origin is internal/adapter/tui/styles.go. The web has always been said to take its
// values from there "verbatim", and until this test that was a sentence in a document — the kind
// this tree keeps finding to be false. It was true when checked, but nothing would have said so
// when it stopped being.
//
// What is checked is every colour role the stylesheet declares that the palette also names. Roles
// only the terminal has (diff backgrounds, syntax hues) are not the web's business; a role the WEB
// has and the palette does not is a failure, because that is the web inventing a value.
//
// This replaced a check that went the other way — every palette role had to appear in the page as
// `--<roleName>:<hex>`. Two things were wrong with it. It demanded the web draw every colour the
// terminal has, which stops being true the moment a syntax hue is added; and it required the CSS
// variable to be spelled exactly like the Go map key, which the M3 roles are not
// (--md-sys-color-on-primary vs onPrimary). One rule, checked once, in the direction that is
// actually true.
// A check that reads a file it did not declare will silently not run. This one reads a file no
// package here imports (clients/web/server does not depend on adapter/tui at all), so nothing about
// compiling this package can notice that styles.go moved. What notices is the test cache: the go
// command records every file the test opened and hashes its size and mtime into the cache key — but
// only for files under the module root. An open outside it is ignored, and the result stays green
// while the file it was watching changes underneath. And no check here depends on an input outside
// the module root: the paths that do fall outside it — t.TempDir(), the sandbox probe's $HOME
// scratch — are files the test itself makes, so there is nothing stale for them to stay green on.
//
// Size and mtime, not the bytes: hashOpen calls hashWriteStat and stops there, and the comment at
// test.go:2106 says why outright — files can be very large, so the mtime and size are assumed good
// enough. Measured here rather than read: styles.go's dark primary was changed to a different hex
// of the same length and its mtime put back with touch -r, and this test reported ok (cached) while
// the palette and the page disagreed; the same tree with -count=1 fails on that line. Ordinary
// editing moves the mtime, so what this leaves uncovered is the copy that preserves it — cp -p,
// rsync --times, a tar extracted with its timestamps. If styles.go ever arrives that way, this
// check is the one that will not notice.
func TestTheWebTakesItsColoursFromHere(t *testing.T) {
	src, err := os.ReadFile("../../../internal/adapter/tui/styles.go")
	if err != nil {
		t.Fatal(err)
	}
	dark, light := paletteIn(t, string(src), "nervDark"), paletteIn(t, string(src), "nervLight")

	// The stylesheet's dark values sit on bare :root; the reader's light choice repeats the light
	// ones, and TestBothLightThemesSayTheSameThing below holds those two copies together.
	for _, c := range []struct {
		what, selector string
		want           map[string]string
	}{
		{"dark", ":root {", dark},
		{"light", `:root[color-theme="light"] {`, light},
	} {
		matched := 0
		for role, hex := range cssColoursIn(t, c.selector) {
			origin, known := c.want[role]
			if !known {
				t.Errorf("the %s stylesheet declares %s=%s and the palette does not name it — "+
					"the web is inventing a colour the TUI cannot follow", c.what, role, hex)
				continue
			}
			matched++
			if !strings.EqualFold(origin, hex) {
				t.Errorf("%s %s: the page says %s, styles.go says %s", c.what, role, hex, origin)
			}
		}
		// A floor, so the check cannot be satisfied by a page that declares nothing. Deleting the
		// token block would otherwise leave every assertion above with nothing to disagree with.
		if matched < 20 {
			t.Errorf("only %d %s roles were checked against the palette; the token block has "+
				"shrunk and this test is no longer looking at the theme", matched, c.what)
		}
	}
}

// consoleCSS is the console's stylesheet, read off disk.
//
// It used to be a Go string: the old console was one page with its <style> in it, and these checks
// sliced that constant. The console is a set of compiled modules now and the stylesheet is a file
// they all share — clients/web/ui/console.css, which gradle copies into the assembled console verbatim. So
// the checks read the file, which is also the thing the build copies, rather than a second copy of
// it kept for testing.
//
// The test cache watches it, because it is under the module root — see the note on
// TestTheWebTakesItsColoursFromHere for what that does and does not cover.
func consoleCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../clients/web/ui/console.css")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The light theme is written twice and both copies have to say the same thing.
//
// CSS cannot give one ruleset two selectors across a media query, so the light palette exists as
// `@media (prefers-color-scheme: light) :root:not([color-theme])` — the machine's own choice — and
// again as `:root[color-theme="light"]`, which is the reader overriding it. The duplication is
// forced; the drift is not. A reader who picks light and gets a different orange from the one the
// machine would have given them is looking at a bug nobody can see in either theme alone.
//
// This check lived in page_test.go and died with the old console, while the comment in console.css
// went on saying it was there.
func TestBothLightThemesSayTheSameThing(t *testing.T) {
	machine := cssColoursIn(t, ":root:not([color-theme]) {")
	chosen := cssColoursIn(t, `:root[color-theme="light"] {`)
	if len(machine) < 20 {
		t.Fatalf("only %d colours in the media query's light block — the parser has lost it", len(machine))
	}
	for role, hex := range machine {
		got, ok := chosen[role]
		if !ok {
			t.Errorf("%s is light when the MACHINE is light and unset when the READER chooses light", role)
			continue
		}
		if !strings.EqualFold(got, hex) {
			t.Errorf("%s: the machine's light theme says %s, the reader's says %s", role, hex, got)
		}
	}
	for role := range chosen {
		if _, ok := machine[role]; !ok {
			t.Errorf("%s is light only when the reader asks — a light machine gets the dark value", role)
		}
	}
}

// paletteIn reads one `var <name> = palette{…}` literal.
func paletteIn(t *testing.T, src, name string) map[string]string {
	t.Helper()
	at := strings.Index(src, "var "+name+" = palette{")
	if at < 0 {
		t.Fatalf("styles.go has no %s", name)
	}
	end := strings.Index(src[at:], "\n}")
	if end < 0 {
		t.Fatalf("%s is unterminated", name)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`"(\w+)":\s*"(#[0-9A-Fa-f]{6})"`).FindAllStringSubmatch(src[at:at+end], -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatalf("%s parsed as empty", name)
	}
	return out
}

// cssColoursIn reads the literal hex declarations of one rule, keyed by role name.
//
// The names are brought to one spelling: the stylesheet writes --md-sys-color-on-primary and
// --magi-ref-surface-container-low, the palette writes onPrimary and surfaceContainerLow, and a test that
// compared the two spellings would find nothing and pass while saying it had checked.
func cssColoursIn(t *testing.T, selector string) map[string]string {
	t.Helper()
	css := consoleCSS(t)
	at := strings.Index(css, selector)
	if at < 0 {
		t.Fatalf("the stylesheet has no %q rule", selector)
	}
	body := css[at+len(selector):]
	end := strings.Index(body, "\n  }")
	if end < 0 {
		t.Fatalf("the %q rule is unterminated", selector)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`--([a-zA-Z-]+)\s*:\s*(#[0-9A-Fa-f]{6})`).FindAllStringSubmatch(body[:end], -1) {
		out[camel(role(m[1]))] = m[2]
	}
	if len(out) == 0 {
		t.Fatalf("the %q rule declares no colours", selector)
	}
	return out
}

// role strips whichever namespace a declaration wears down to the palette's own key. The page
// carries three: Material's own roles, magi's reference layer, and the layer that used to wear
// Material's prefix without being Material's.
func role(name string) string {
	for _, p := range []string{"md-sys-color-", "magi-ref-", "md-"} {
		if strings.HasPrefix(name, p) {
			return strings.TrimPrefix(name, p)
		}
	}
	return name
}

func camel(kebab string) string {
	parts := strings.Split(kebab, "-")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}
