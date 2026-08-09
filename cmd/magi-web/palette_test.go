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
func TestTheWebTakesItsColoursFromHere(t *testing.T) {
	src, err := os.ReadFile("../../internal/adapter/tui/styles.go")
	if err != nil {
		t.Fatal(err)
	}
	dark, light := paletteIn(t, string(src), "nervDark"), paletteIn(t, string(src), "nervLight")

	// The stylesheet's dark values sit on bare :root; the reader's light choice repeats the light
	// ones, and TestBothLightThemesSayTheSameThing already holds those two copies together.
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
	at := strings.Index(indexHTML, selector)
	if at < 0 {
		t.Fatalf("the page has no %q rule", selector)
	}
	body := indexHTML[at+len(selector):]
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
