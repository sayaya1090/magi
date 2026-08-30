//go:build ignore

// Bake the icons the console asks for into a sprite this binary carries.
//
// # Why a generator and not files in the repository
//
// The icons are Font Awesome Pro. Their licence permits using them in something you deploy and
// does not permit republishing them as files, so they cannot live in a public repository — and a
// CDN is not an option either: this console is read over ssh into machines that have no route to
// the internet, and it must not tell a third party which workspace is being looked at.
//
// So the art arrives at BUILD time and the repository holds only the names. Run:
//
//	MAGI_FA_DIR=~/Downloads/kit-…-web go generate ./clients/web/server
//
// or, in CI, point it at the Pro package restored from the registry with a token:
//
//	MAGI_FA_DIR=node_modules/@fortawesome/fontawesome-pro go generate ./clients/web/server
//
// Both layouts are the same — svgs/<style>/<name>.svg — which is why there is one reader here
// rather than one per source.
//
// # What it emits, and what happens without it
//
// internal/webassets/sprite_gen.go, which is git-ignored, and which sets Sprite in an init.
// Without it Sprite stays empty, the screens draw the shapes they have always drawn, and nothing is
// broken: a build with no licence produces a working console with plainer icons. The test suite
// runs both ways.
//
// # Which icons
//
// The ones the CONSOLE names. Every reference is written <use href="#i-<style>-<icon>">, so the
// screens themselves are the list — a manifest kept beside them would be a second place to edit
// and the place where an icon goes missing. They are mined from clients/web/ui, whose modules draw every
// screen; this command's own directory has none left to name.
package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// styleDir maps the prefix used in an id to the directory it comes from. Short, because it is
// typed into every reference on a screen: sl = sharp light, ss = sharp solid, b = brands.
var styleDir = map[string]string{
	"sl": "sharp-light",
	"ss": "sharp-solid",
	"b":  "brands",
}

func main() {
	root := os.Getenv("MAGI_FA_DIR")
	if root == "" {
		fmt.Fprintln(os.Stderr, "gen_icons: MAGI_FA_DIR is not set — no sprite written, the screens will draw their own shapes")
		return
	}
	// Where it is looking, checked before what is in it.
	//
	// Without this a wrong path produced the same message as a missing icon — "the console asks for
	// icons this download does not have", followed by every name it uses, because none of them
	// were found. Two very different problems with one sentence between them, and the one that is
	// nearly always true (the path) is not the one it named.
	abs, _ := filepath.Abs(root)
	if st, serr := os.Stat(root); serr != nil || !st.IsDir() {
		die(fmt.Errorf("MAGI_FA_DIR points at %s, which is not a directory here.\n"+
			"On the kit download that is the folder holding svgs/; from npm it is "+
			"node_modules/@fortawesome/fontawesome-pro", abs))
	}
	if st, serr := os.Stat(filepath.Join(root, "svgs")); serr != nil || !st.IsDir() {
		names, _ := os.ReadDir(root)
		var got []string
		for _, n := range names {
			got = append(got, n.Name())
		}
		sort.Strings(got)
		// A dozen is enough to recognise a directory by; the whole of a wrong one is a wall.
		if len(got) > 12 {
			got = append(got[:12], "…")
		}
		die(fmt.Errorf("%s has no svgs/ directory — that is where both the kit download and the "+
			"Pro package keep the art.\nWhat is there: %s", abs, strings.Join(got, " ")))
	}
	for prefix, dir := range styleDir {
		if st, serr := os.Stat(filepath.Join(root, "svgs", dir)); serr != nil || !st.IsDir() {
			die(fmt.Errorf("%s/svgs has no %s/ — the console names icons as #i-%s-… and they come "+
				"from there.\nA kit download only contains the styles the kit was built with; the "+
				"Pro package has them all", abs, dir, prefix))
		}
	}
	// Everything a screen is made of. The console is a set of compiled modules now, so the names
	// are spread across their sources rather than sitting in one page — but they are still only in
	// what RENDERS: the stylesheet cannot name an icon (an id in CSS is a reference nothing draws),
	// and the modules' own tests are not screens, so a name that appears only in a test is a name
	// nobody sees and is not baked.
	want := map[string]bool{}
	ref := regexp.MustCompile(`#i-(sl|ss|b)-([a-z0-9-]+)`)
	uiRoot := filepath.Join("..", "..", "web", "ui")
	mainJava := filepath.Join("src", "main", "java")
	err := filepath.WalkDir(uiRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// build/ holds the compiled output, which repeats every name in obfuscated form and would
		// make this depend on whether the last build was for this tree.
		if d.IsDir() {
			if d.Name() == "build" || d.Name() == ".gradle" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".java") || !strings.Contains(path, mainJava) {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			want[m[1]+"-"+m[2]] = true
		}
		return nil
	})
	if err != nil {
		die(fmt.Errorf("reading the console's sources under %s: %w", uiRoot, err))
	}
	if len(want) == 0 {
		fmt.Fprintln(os.Stderr, "gen_icons: the console names no icons; nothing to bake")
		return
	}

	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)

	var sprite strings.Builder
	// aria-hidden and display:none, because this is a definition block and not a picture: left
	// visible it is a 0×0 svg that still takes a line box, and read out it is a list of nothing.
	sprite.WriteString(`<svg id="isprite" aria-hidden="true" style="display:none">`)
	var missing []string
	for _, n := range names {
		i := strings.Index(n, "-")
		style, icon := n[:i], n[i+1:]
		dir, ok := styleDir[style]
		if !ok {
			die(fmt.Errorf("unknown style prefix in #i-%s", n))
		}
		b, err := os.ReadFile(filepath.Join(root, "svgs", dir, icon+".svg"))
		if err != nil {
			missing = append(missing, n)
			continue
		}
		box, body, err := carve(string(b))
		if err != nil {
			die(fmt.Errorf("%s: %w", n, err))
		}
		fmt.Fprintf(&sprite, `<symbol id="i-%s" viewBox="%s">%s</symbol>`, n, box, body)
	}
	sprite.WriteString(`</svg>`)

	// A named icon with no file is a hole in a screen, not a warning to scroll past: the build
	// stops and says which, because the alternative is a screen with a gap where a control was.
	if len(missing) > 0 {
		die(fmt.Errorf("%s does not have %d of the %d icons the console names: %s\n"+
			"A kit download only contains what was added to that kit — add them and download "+
			"again, or point MAGI_FA_DIR at the Pro package (npm i @fortawesome/fontawesome-pro), "+
			"which has every icon", abs, len(missing), len(names), strings.Join(missing, ", ")))
	}

	out := "// Code generated by gen_icons.go. DO NOT EDIT.\n" +
		"// Font Awesome Pro icons, baked in at build time — see gen_icons.go for why this file is\n" +
		"// not in the repository. Font Awesome Pro is licensed to whoever built this binary:\n" +
		"// https://fontawesome.com/license\n\n" +
		"package webassets\n\n" +
		"func init() { Sprite = " + quote(sprite.String()) + " }\n"
	// Formatted, because the gate formats everything and a generated file is not exempt from
	// being read: an unformatted one fails the build for a reason that has nothing to do with the
	// change somebody just made.
	pretty, err := format.Source([]byte(out))
	if err != nil {
		die(fmt.Errorf("the generated file does not parse: %w", err))
	}
	if err := os.WriteFile(filepath.Join("..", "..", "internal", "webassets", "sprite_gen.go"), pretty, 0o644); err != nil {
		die(err)
	}
	fmt.Fprintf(os.Stderr, "gen_icons: %d icons baked from %s\n", len(names), root)
}

// carve takes the viewBox and the drawing out of one downloaded file.
//
// The file also carries an xmlns, a width, a licence comment and a fill — none of which belong in
// a symbol: the namespace is the parent svg's, the size is the use site's, and the fill is already
// currentColor. Keeping them would multiply the licence comment by a hundred and put an attribute
// on every shape that the stylesheet then has to fight.
func carve(s string) (box, body string, err error) {
	m := regexp.MustCompile(`viewBox="([^"]+)"`).FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf("no viewBox")
	}
	box = m[1]
	i := strings.Index(s, ">")
	j := strings.LastIndex(s, "</svg>")
	if i < 0 || j < 0 || j < i {
		return "", "", fmt.Errorf("not an svg")
	}
	body = s[i+1 : j]
	body = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(body, "")
	return box, strings.TrimSpace(body), nil
}

// quote writes a Go string literal. Backquotes are not usable — a path can contain one — and
// %q is exactly right for the rest.
func quote(s string) string { return fmt.Sprintf("%q", s) }

func die(err error) {
	fmt.Fprintln(os.Stderr, "gen_icons:", err)
	os.Exit(1)
}
