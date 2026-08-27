package main

import (
	"fmt"
	"github.com/sayaya1090/magi/internal/webassets"
	"github.com/sayaya1090/magi/internal/webdemo"
	"os"
	"path/filepath"
	"strings"
)

// The console as a static page, answered by a mock magi in the browser.
//
// # Why this exists
//
// Nobody can look at this screen without running a daemon, a console and at least one companion.
// That is a lot of setup to answer "what does it look like", and it means a change to the front end
// is reviewed as a diff of a Go string. A published demo turns the review into looking at it.
//
// # Why the mock is in the browser
//
// GitHub Pages serves files, not processes, so there is no /fleet to fetch. The demo replaces the
// page's `fetch` with one that answers from a fixture — the same trick the render tests use under
// node, for the same reason: the page's REAL javascript runs, against data shaped like the real
// handlers' output.
//
// The page itself is not copied or edited. This writes indexHTML verbatim and appends one script,
// so a demo can never drift from what the binary serves: there is only one page.
//
// # What it proves, and what it does not
//
// It proves the markup, the styling and the page's own logic — the states, the filters, the tabs,
// the forms, how it behaves on a phone. It does not prove a single handler: every answer is a
// fixture. The Go tests are what check the handlers, and the workflow runs those first, so a demo
// that deploys is a demo whose server side passed its own tests.

// emitDemo writes the static demo into dir.
//
// The page verbatim plus the mock, and a .nojekyll so a host that would otherwise hide files
// starting with an underscore does not eat part of the site. Nothing here is templated: the moment
// this function edits the page, the demo stops being evidence about the page.
func emitDemo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Root-relative becomes relative, for the demo only.
	//
	// The binary serves the page AT the root, so `/vendor/rxjs.js` is right there. GitHub Pages
	// serves a project site under /<repo>/, where a leading slash escapes to the domain root and
	// 404s — the module import among them, which means the script never runs and the page is
	// blank. Shipped exactly that way, and a local check that served the directory at the root
	// said everything was fine, which is how it got past.
	//
	// Only the prefixes the page loads are rewritten. Navigation hrefs keep their absolute form:
	// they are pushState targets read by the page's own router, not fetches.
	page := indexHTML
	for _, prefix := range []string{"/vendor/", "/font/"} {
		page = strings.ReplaceAll(page, "'"+prefix, "'."+prefix)
		page = strings.ReplaceAll(page, `"`+prefix, `".`+prefix)
		page = strings.ReplaceAll(page, "url("+prefix, "url(."+prefix)
	}
	for _, file := range []string{"/icon.svg", "/icon-maskable.svg", "/manifest.webmanifest"} {
		page = strings.ReplaceAll(page, `"`+file+`"`, `".`+file+`"`)
	}
	page += webdemo.Script
	if strings.Contains(indexHTML, webdemo.Script) {
		return fmt.Errorf("the page already carries the demo script — it must only ever be appended here")
	}
	for name, body := range map[string]string{
		"index.html": page,
		".nojekyll":  "",
		// The two the page links for a home-screen install. Same bytes the server answers with;
		// without them the demo is a page with a broken manifest, which is what it looked like
		// until a check started walking every path the page references.
		"manifest.webmanifest": webassets.Manifest,
		"icon.svg":             webassets.Icon,
		"icon-maskable.svg":    webassets.IconMaskable,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	// Everything the page IMPORTS, at the path it imports it from. A missing font degrades the
	// look; a missing module means the script never runs and the page is blank — which is exactly
	// what shipped the first time this became a module, because emitDemo copied the fonts and
	// nobody had written down that the list had grown.
	// Everything in vendor/, not a list somebody keeps in step: the page imports what it imports,
	// and a hand-written list is one more thing to forget when a second bundle arrives — which is
	// exactly what happened with the Material Web one.
	vendored, verr := assetFS.ReadDir("vendor")
	if verr != nil {
		return verr
	}
	var carry []string
	for _, f := range vendored {
		if strings.HasSuffix(f.Name(), ".js") {
			carry = append(carry, "vendor/"+f.Name())
		}
	}
	// Every language pack, for the same reason and read the same way. The demo used to answer these
	// from a copy inlined in its own script, which meant the deployed page was English whatever the
	// reader's browser asked for — the one part of the console a visitor can check against their own
	// machine, and it was the part that did not work.
	packs, perr := assetFS.ReadDir("i18n")
	if perr != nil {
		return perr
	}
	for _, f := range packs {
		if strings.HasSuffix(f.Name(), ".json") {
			carry = append(carry, "i18n/"+f.Name())
		}
	}
	for _, name := range carry {
		b, rerr := assetFS.ReadFile(name)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	// The language seed the handler inlines for a real page. Written into the file here for the
	// same reason: without it the first paint is a screen of dotted keys.
	if pack, perr := assetFS.ReadFile("i18n/language.en.json"); perr == nil {
		page = "<script>window.__LANG=" + string(pack) + "</script>\n" + page
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
			return err
		}
	}

	// The font the page asks for, at the path it asks for it. Without this the demo falls back to
	// the system serif and stops being a fair look at the thing.
	fonts, err := fontFS.ReadDir("fonts")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "font"), 0o755); err != nil {
		return err
	}
	for _, f := range fonts {
		if !strings.HasSuffix(f.Name(), ".woff2") {
			continue
		}
		b, rerr := fontFS.ReadFile("fonts/" + f.Name())
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(filepath.Join(dir, "font", f.Name()), b, 0o644); werr != nil {
			return werr
		}
	}
	return nil
}
