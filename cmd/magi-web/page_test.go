package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

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

	// ES imports, which are not href or src and are how this check was got past. `import
	// '/vendor/material.js'` answered 404 on a real console — and a module whose import fails does
	// not run AT ALL, so there were no components, no script and no language beyond the seed. It
	// went unnoticed because the static demo writes those files to disk beside the page, and every
	// review of this console for weeks was a review of the demo.
	imp := regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"]*from\s*)?['"]([^'"]+)['"]`)
	for _, m := range imp.FindAllStringSubmatch(indexHTML, -1) {
		u := m[1]
		if !strings.HasPrefix(u, "/") {
			t.Errorf("the page imports %q, which is not a root-relative path on this server", u)
			continue
		}
		if _, ok := served[u]; ok {
			continue
		}
		if i := strings.LastIndexByte(u, '/'); i > 0 {
			if _, ok := served[u[:i+1]]; ok {
				continue
			}
		}
		t.Errorf("the page imports %q and this server has no such route — a module whose import "+
			"404s does not run at all, so the whole page goes dark", u)
	}

	// And the language packs, which the page fetches at runtime from a path it BUILDS, so no
	// scanner of this text can see them. Every pack that ships has to be reachable, or a reader
	// whose browser asks for one gets the English seed and no way to tell why.
	packs, err := assetFS.ReadDir("i18n")
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("no language packs are embedded; this check is measuring nothing")
	}
	for _, p := range packs {
		if _, ok := served["/i18n/"]; !ok {
			t.Fatalf("nothing serves /i18n/, so %s never reaches a browser", p.Name())
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
// The reader's theme choice must be able to beat the machine in both directions, which takes two
// rulesets carrying the same declarations: one under the media query for the machine's answer, one
// on the attribute for the reader's. CSS cannot give a single ruleset both selectors across a
// media-query boundary, so the copy is unavoidable — this is what stops it drifting.
// The language a reader can PICK and the language the page can LOAD are two lists, and they are
// only the same list because this says so. A code in one and not the other is either an option
// that loads nothing or a pack nobody can reach.
func TestTheLanguageChoicesAreThePacksThatExist(t *testing.T) {
	codes := func(re string) []string {
		m := regexp.MustCompile(re).FindStringSubmatch(indexHTML)
		if m == nil {
			t.Fatalf("no %s in the page", re)
		}
		var out []string
		for _, c := range regexp.MustCompile(`'([a-z]{2})'`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, c[1])
		}
		sort.Strings(out)
		return out
	}
	loadable := codes(`const PACKS = \[([^\]]*)\]`)
	// The option list carries pairs; the value of each is the code.
	pickable := codes(`(?s)label: 'pref\.lang',.*?options: \[(.*?)\],\n`)
	if len(loadable) == 0 {
		t.Fatal("the page can load no language at all")
	}
	if !slices.Equal(loadable, pickable) {
		t.Errorf("the page loads %v but offers %v — one of those is a dead end", loadable, pickable)
	}
	// And each has a file behind it.
	for _, c := range loadable {
		if _, err := assetFS.ReadFile("i18n/language." + c + ".json"); err != nil {
			t.Errorf("the page loads %q and there is no pack for it: %v", c, err)
		}
	}
}

func TestBothLightThemesSayTheSameThing(t *testing.T) {
	decls := func(after string) string {
		at := strings.Index(indexHTML, after)
		if at < 0 {
			t.Fatalf("no %q in the page; the theme override is gone or renamed", after)
		}
		body := indexHTML[at+len(after):]
		end := strings.Index(body, "}")
		if end < 0 {
			t.Fatalf("the rule after %q is unterminated", after)
		}
		// Whitespace and indentation differ by nesting depth; the declarations are what must match.
		return strings.Join(strings.Fields(body[:end]), " ")
	}
	machine := decls(":root:not([color-theme]) {")
	reader := decls(`:root[color-theme="light"] {`)
	if machine == "" {
		t.Fatal("the light theme declares nothing")
	}
	if machine != reader {
		t.Errorf("the two light themes have drifted.\n  under the media query: %s\n  on the attribute:     %s",
			machine, reader)
	}
}

// A theme chosen on a previous visit has to be on the root element before the stylesheet paints,
// or the reader sees the other theme first — a white flash in a dark room.
func TestTheChosenThemeIsAppliedBeforeTheStylesheet(t *testing.T) {
	script := strings.Index(indexHTML, "localStorage.getItem('theme')")
	style := strings.Index(indexHTML, "<style>")
	if script < 0 {
		t.Fatal("nothing reads the stored theme before the page paints")
	}
	if script > style {
		t.Error("the stored theme is applied after the stylesheet, so the other theme paints first")
	}
}

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

// A string the language pack has an entry for must not be written into the page as itself.
//
// This is the defect this file keeps finding from the other side. The pack carried translations for
// "can't reach magi-web", "handed out", "context", "summarised away" and "say who it is for" while
// the script printed each of them as an English literal — five keys with no reader, and five
// English words on a Korean page. Nothing failed, because the pack was complete and the code simply
// did not ask it anything.
//
// Only values long enough to be unambiguous are checked. A pack entry of "to" or "url" would match
// half the source by accident and say nothing.
func TestNoLabelIsWrittenInEnglishBesideItsOwnTranslation(t *testing.T) {
	raw, err := assetFS.ReadFile("i18n/language.en.json")
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]string
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatal(err)
	}
	// Comments do not print. One of them mentions a label by name to explain what it is for, and a
	// scan that counted it would be asking the author to stop writing about the page.
	body := withoutComments(scriptBody(t, indexHTML))
	checked := 0
	for key, val := range pack {
		if len([]rune(val)) < 8 || strings.Contains(val, "{") {
			continue // too short to be distinctive, or a template that never appears literally
		}
		checked++
		for _, quoted := range []string{"'" + val + "'", `"` + val + `"`} {
			if strings.Contains(body, quoted) {
				t.Errorf("the script writes %s directly, and %q is its translation key — "+
					"that label stays English in every other language", quoted, key)
			}
		}
	}
	if checked < 15 {
		t.Errorf("only %d pack entries were long enough to check; this guard has stopped looking "+
			"at the pack", checked)
	}
}

// withoutComments strips // and /* */ from a script, so a scan reads what the page PRINTS.
func withoutComments(src string) string {
	src = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	var out []string
	for _, line := range strings.Split(src, "\n") {
		// Naive on purpose: a "//" inside a string literal would cut that line short, and a line cut
		// short can only make this check miss something, never invent one.
		if at := strings.Index(line, "//"); at >= 0 {
			line = line[:at]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// The rules that place the page around the rail must be the LAST media queries in the stylesheet.
//
// A media query adds no specificity, so these win only by coming after what they override — and
// three times now they have not. Written above a plain `#tabs { display:flex }` the tabs stayed on
// a desktop; written above a `padding:` shorthand for main the page offset became 0 and the fixed
// rail sat on top of the page; and moving the breakpoint to 600 put a 640px composer rule after
// them, which reset the offset again in the 40px between.
//
// Checked by position rather than by effect because the fake DOM has no CSS and a browser is not
// in this suite. Position is what was wrong all three times.
func TestTheLayoutQueriesComeLast(t *testing.T) {
	sheet := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	at := func(q string) int {
		i := strings.Index(sheet, q)
		if i < 0 {
			t.Fatalf("the stylesheet has no %q — this guard has lost its subject", q)
		}
		return i
	}
	nav := at("@media (min-width:37.5em)")
	for _, earlier := range []string{
		"@media (max-width:62.5em)", // the table's own collapse
		"@media (max-width:40em)",   // the composer's
	} {
		// The LAST occurrence, since a query may appear more than once.
		if last := strings.LastIndex(sheet, earlier); last > nav {
			t.Errorf("%s is written after the rail's layout rules, so it overrides the page offset "+
				"in the widths where both apply — the padding shorthand resets padding-left", earlier)
		}
	}
	if compact := at("@media (max-width:37.4375em)"); compact < nav {
		t.Error("the compact rules come before the rail's; they must be able to undo them")
	}
}

// Opening the drawer must not move the page.
//
// The rail's width and the gutter the page reserves were one number, so widening the rail widened
// the gutter: main and the header stayed put while everything inside them shifted 184px right and
// lost 184px of width, which is the reflow that kept being reported. Two names now — --rail-w for
// the gutter, --rail-now for how wide the rail is drawing itself — and the page must only ever read
// the first. Checked as a rule about which name appears where, because the effect is a layout in a
// browser and this is the thing that can be asserted about the text.
func TestTheDrawerDoesNotTakeWidthFromThePage(t *testing.T) {
	css := indexHTML
	// --rail-w is declared once, at :root, and never redefined. A second declaration anywhere is a
	// gutter that changes with something, which is the defect.
	if n := strings.Count(css, "--rail-w:"); n != 1 {
		t.Errorf("--rail-w is declared %d times; the gutter the page reserves is one constant", n)
	}
	if !strings.Contains(css, "--rail-now:16rem") {
		t.Error("nothing widens the rail; the drawer cannot open")
	}
	// Whatever reads the live width, it must be the rail itself. Anything else reading --rail-now
	// is a part of the page that moves when the drawer does.
	for _, line := range strings.Split(css, "\n") {
		if !strings.Contains(line, "var(--rail-now") {
			continue
		}
		if !strings.Contains(line, "width:") {
			t.Errorf("something other than the rail's own width reads the live width: %q",
				strings.TrimSpace(line))
		}
	}
}

// No rule may name an element this page stopped creating.
//
// The migration to Material Web replaced every button, textarea, select and input with a component,
// and four separate times a rule naming the old element was left behind: .answer button, .iv
// .promote button, .composer textarea, .composer button:focus-visible. Each one styles nothing and
// each one reads, to the next person, as the rule that governs that control — so the real rule gets
// written a second time somewhere else, or the control simply goes unstyled and nobody can say why.
//
// A selector matching NOTHING cannot be checked from the text alone. What can be checked is this
// narrower thing: the page's markup contains no such element, so a selector whose subject is one is
// dead by construction.
func TestNoRuleNamesAnElementThePageNoLongerHas(t *testing.T) {
	css := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	// Comments first. Half this stylesheet is prose about which element a rule used to name, and a
	// scanner that reads it finds every one of them.
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	body := indexHTML[strings.Index(indexHTML, "</style>"):]
	gone := []string{"button", "textarea", "select", "input"}
	// First: the premise. If the markup grows one of these back, this test is measuring nothing and
	// must say so rather than keep passing.
	for _, tag := range gone {
		if regexp.MustCompile(`<` + tag + `[\s>]`).MatchString(body) {
			t.Fatalf("the page has a bare <%s> again; this check assumed it did not", tag)
		}
	}
	// Then: every selector, split off its declaration block, with the component prefixes removed —
	// md-filled-button ends in "button" and is exactly what these rules SHOULD name.
	sel := regexp.MustCompile(`(?m)^\s*([^{}@/][^{}]*?)\s*\{`)
	word := regexp.MustCompile(`(?:^|[\s>+~(])(` + strings.Join(gone, "|") + `)(?:[\s>+~:.\[)]|$)`)
	for _, m := range sel.FindAllStringSubmatch(css, -1) {
		for _, one := range strings.Split(m[1], ",") {
			one = strings.TrimSpace(one)
			if one == "" || strings.HasPrefix(one, "*") {
				continue
			}
			// A component's own tag name is not the element it replaced.
			clean := regexp.MustCompile(`\bmd-[a-z-]+`).ReplaceAllString(one, "COMPONENT")
			if w := word.FindStringSubmatch(clean); w != nil {
				t.Errorf("%q names <%s>, which this page has not had since the migration — "+
					"the rule reaches nothing and reads as though it governs the control", one, w[1])
			}
		}
	}
}

// Nothing moves for somebody who asked their machine to stop moving things.
//
// The setting is an accessibility one — vestibular disorders, migraine — and a page that honours it
// halfway is a page that does not honour it. Checked as "there are keyframes AND there is an escape
// that overrides them", because either half alone passes for the wrong reason: no animations at all
// would satisfy a test that only looked for the media query.
func TestMotionCanBeTurnedOffByThePersonReadingIt(t *testing.T) {
	css := indexHTML
	if !strings.Contains(css, "@keyframes") {
		t.Skip("nothing animates yet; there is nothing to turn off")
	}
	i := strings.Index(css, "@media (prefers-reduced-motion: reduce)")
	if i < 0 {
		t.Fatal("the page animates and offers no way to stop it")
	}
	block := css[i:min(i+400, len(css))]
	for _, want := range []string{"animation-duration", "transition-duration", "!important"} {
		if !strings.Contains(block, want) {
			t.Errorf("the reduced-motion block does not override %s:\n%s", want, block)
		}
	}
	// A universal selector, because the override has to reach the components' own animations too —
	// md-tabs animates its indicator inside a shadow root this stylesheet cannot name, and the
	// inherited animation properties are the only thing that gets in there.
	if !strings.Contains(block, "*") {
		t.Errorf("the override names specific selectors, so a component's own motion escapes it:\n%s", block)
	}
}

// Every phrase in the pack reaches a screen, and every phrase on a screen comes from the pack.
//
// The second half is what this is really for. A key nobody asks for is not merely clutter: it is
// almost always a phrase somebody translated for a place that then rendered the English inline.
// Nineteen were sitting here at once — four state words, five parts of the context line, the plan
// heading — every one of them written, and its site hard-coded in English on a Korean page.
//
// Keys are reached two ways and both are counted: written out, or built from a prefix and a value
// off the wire ("state." + a.state). A prefix builder claims its whole family, which is as precise
// as this can be from the text and still honest about the mechanism.
func TestEveryPhraseInThePackIsAskedFor(t *testing.T) {
	raw, err := assetFS.ReadFile("i18n/language.en.json")
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]string
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatal(err)
	}
	if len(pack) == 0 {
		t.Fatal("the pack is empty; this check is measuring nothing")
	}
	// Prefixes the page concatenates onto, e.g. `'state.' + s`.
	built := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+\.)'\s*\+`).FindAllStringSubmatch(indexHTML, -1) {
		built[m[1]] = true
	}
	var dead []string
	for k := range pack {
		if strings.Contains(indexHTML, "'"+k+"'") {
			continue
		}
		if i := strings.IndexByte(k, '.'); i > 0 && built[k[:i+1]] {
			continue
		}
		dead = append(dead, k)
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Errorf("%d phrase(s) nobody asks for — usually a translated phrase whose site renders "+
			"the English inline:\n  %s", len(dead), strings.Join(dead, "\n  "))
	}
}

// The shape and type scales, checked rather than claimed.
//
// UI.md's audit table said "0 off the scale" for both, and both had drifted: three dots drawn with
// border-radius:50% and six literals at 10px and 13px, neither of which M3 has. A sentence in a
// document is true on the day it is written; this is the thing that stays true.
func TestTheShapeAndTypeScalesHaveNothingOffThem(t *testing.T) {
	css := indexHTML[strings.Index(indexHTML, "<style>"):strings.Index(indexHTML, "</style>")]
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")

	// Radii come from the scale tokens or are zero. A literal percentage is the same circle a
	// --shape-full gives on a 7px box, and it is off the list a reader is told everything is on.
	for _, m := range regexp.MustCompile(`border-radius:\s*([^;}]+)`).FindAllStringSubmatch(css, -1) {
		v := strings.TrimSpace(m[1])
		if strings.HasPrefix(v, "var(--shape-") || v == "0" || v == "inherit" {
			continue
		}
		t.Errorf("border-radius:%s is not on the shape scale", v)
	}

	// A size in px is off the scale whatever its value, because px does not move when a reader
	// raises their browser's default font size: the labels stayed at 11 while the rem parts around
	// them grew, and the row scrolled off the side. Sizes come from the typescale tokens now, so
	// the check is that there are none — a stronger statement than a list of the ones allowed,
	// which this test used to carry and which had stopped matching anything at all.
	var px []string
	for _, m := range regexp.MustCompile(`font(?:-size)?:[^;}]*?(\d+(?:\.\d+)?px)`).FindAllStringSubmatch(css, -1) {
		px = append(px, m[1])
	}
	if len(px) > 0 {
		sort.Strings(px)
		t.Errorf("%d font size(s) in px: %s — take the size from --md-sys-typescale-<role>-size, "+
			"which is what the components read and what a reader's default scales",
			len(px), strings.Join(px, ", "))
	}

	// Padding, gap and margin come from the spacing scale. The page carried twenty-six distinct
	// values between 1.6 and 38.4dp, which is not a rhythm but the absence of one. A literal here
	// is how the twenty-seventh gets in.
	spacing := regexp.MustCompile(`\b(?:padding|margin|gap|row-gap|column-gap)(?:-(?:top|right|bottom|left|inline|block|inline-start|inline-end|block-start|block-end))?\s*:\s*([^;}]+)`)
	rem := regexp.MustCompile(`\d*\.?\d+rem`)
	var off []string
	for _, m := range spacing.FindAllStringSubmatch(css, -1) {
		if v := rem.FindAllString(m[1], -1); v != nil {
			off = append(off, strings.TrimSpace(m[0]))
		}
	}
	if len(off) > 0 {
		sort.Strings(off)
		if len(off) > 8 {
			off = off[:8]
		}
		t.Errorf("spacing written as a rem literal rather than a --space-* token:\n  %s",
			strings.Join(off, "\n  "))
	}
}

// The page must not use array methods on a DOM collection.
//
// This is the one mistake the fake DOM cannot catch, because its collections ARE arrays. It has
// already shipped once: `box.children.indexOf(grid)` passed every test and threw in a browser —
// children is an HTMLCollection there, with no indexOf — so the guard it was in rejected, an async
// function died with nobody awaiting it, and the whole context panel silently stopped rendering.
//
// HTMLCollection has no array methods at all. NodeList has forEach and nothing else. Both are
// fine once spread, which is what the rest of this page already does.
func TestNoArrayMethodOnADOMCollection(t *testing.T) {
	js := indexHTML[strings.Index(indexHTML, "</style>"):]
	js = regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(js, "")
	arrayOnly := `(indexOf|lastIndexOf|map|filter|reduce|reduceRight|find|findIndex|some|every|slice|sort|concat|join|includes|flatMap)`
	for _, pat := range []struct{ what, re string }{
		{"children", `\.children\.` + arrayOnly + `\(`},
		{"querySelectorAll(...)", `querySelectorAll\([^)]*\)\.` + arrayOnly + `\(`},
		{"childNodes", `\.childNodes\.` + arrayOnly + `\(`},
	} {
		for _, m := range regexp.MustCompile(pat.re).FindAllString(js, -1) {
			t.Errorf("%q calls an array method on a %s — that is an HTMLCollection or a NodeList "+
				"in a browser and an array only in the fake DOM, so this passes every test and "+
				"throws where it matters. Spread it first.", strings.TrimSpace(m), pat.what)
		}
	}
}
