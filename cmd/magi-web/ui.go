package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

// The console this binary serves: web/ui, compiled by GWT and laid out by `gradlew assembleConsole`.
//
// # Why the directory is empty in the repository
//
// Compiling it needs a JDK and gradle. Making `go build ./cmd/magi-web` depend on those would break
// the one promise this repository keeps about itself — clone it, build it, run it — for everyone who
// never touches the front end. So the assembled tree is a BUILD artefact: CI produces it and copies
// it here, and .gitignore keeps everything but the README out.
//
// The consequence, said plainly because it is the surprising half: a plain `go build` here produces
// a binary with NO console in it. That binary is a working BFF — every route below `/` answers — and
// `/` says so in a sentence rather than serving a blank page. Empty is a supported state, the same
// way an unlicensed build has no icon sprite and draws its own shapes.
//
//go:embed console
var consoleFS embed.FS

// consoleTree is what `/` and `/ui/` read from.
//
// One value for two sources, resolved once at startup: the tree baked in above, or a directory on
// disk when -console names one. The disk case is the development loop — `gradlew assembleConsole`
// writes into web/ui/build/console and a reload picks it up, with no copy step and no second server
// in front. (There used to be a second server, web/server, that proxied to this one for exactly
// that reason. A flag is smaller than a process, and it cannot drift from what production serves,
// because it IS what production serves.)
func consoleTree(dir string) (fs.FS, error) {
	if dir != "" {
		st, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("-console %s: %w", dir, err)
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("-console %s is not a directory", dir)
		}
		return os.DirFS(dir), nil
	}
	// The embedded copy, with its directory name taken off, so both sources are rooted at the same
	// place and every reader below can say "console.html" and not care which one it got.
	return fs.Sub(consoleFS, "console")
}

// consolePage is the assembled page with the icons put in, or "" when this build has no console.
//
// Read at startup rather than per request: it is one file that cannot change while the process runs
// (embedded), and re-reading it on every load would let a -console directory be half-written when a
// browser asks. The disk case re-reads — see page().
func consolePage(tree fs.FS) string {
	b, err := fs.ReadFile(tree, "console.html")
	if err != nil {
		return ""
	}
	return withSprite(string(b))
}

// noConsole is what `/` answers when this binary was built without one.
//
// A page, not a 404, and it says which build it is and what to do about it. The alternative — an
// empty body, or the file server's "404 page not found" — is a person concluding that magi-web is
// broken, when what actually happened is a build that skipped one CI step. The API is untouched:
// everything this console fetches still answers, so a demo emitter or a peer console pointed at
// this process works exactly as before.
const noConsole = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>magi console — not in this build</title>
<style>
  :root { color-scheme: dark light }
  body { font: 15px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; margin: 3rem auto; max-width: 44rem; padding: 0 1.5rem }
  code { background: color-mix(in srgb, currentColor 12%, transparent); padding: .1em .35em; border-radius: 3px }
</style></head><body>
<h1>This build has no console in it.</h1>
<p>The screens are compiled from <code>web/ui</code> by gradle, and that output is a build artefact
   rather than something the repository carries. This binary is a working back end — every route it
   serves still answers — but there is no page to put in front of you.</p>
<p>To get one:</p>
<pre>cd web/ui &amp;&amp; ./gradlew assembleConsole
magi-web -console web/ui/build/console</pre>
<p>Or take a release binary, which is built by CI with that step in it.</p>
</body></html>
`

// page serves the console.
//
// The whole front end is the assembled tree under /ui/; this is the document that starts it. Cached
// and revalidated, like the assets under it: a browser holding yesterday's copy is a person looking
// at yesterday's console, and with no Cache-Control at all a browser applies its own heuristic,
// which is the worst of both — sometimes stale, never predictably. The ETag makes the check a round
// trip with no body.
func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page := s.consolePage
	// A -console directory is being rebuilt while somebody watches, so its page is read fresh. The
	// embedded one cannot change and is read once.
	if s.consoleDir != "" {
		page = consolePage(s.ui)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if page == "" {
		w.Header().Set("Cache-Control", "no-store")
		// 503 and not 404: the path is right and the thing is missing, which is what this code
		// means. A 404 here would read as "wrong URL" to every tool that checks one.
		w.WriteHeader(http.StatusServiceUnavailable)
		say(w, noConsole)
		return
	}
	if done := revalidate(w, r, []byte(page)); done {
		return
	}
	say(w, page)
}

// uiAsset serves the compiled modules and their stylesheets.
//
// # The cache contract, which is GWT's and not ours
//
// `<module>.nocache.js` is the selector: it is recompiled on every build, it decides which
// permutation this browser gets, and caching it is how a browser ends up asking for a permutation
// that no longer exists — an intermittent blank screen after a deploy. `<hash>.cache.js` is the
// opposite: the name IS the content, so it can be kept forever. Served with no headers at all, a
// browser's heuristic caches both, and only the first one is a bug.
func (s *server) uiAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/ui/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(s.ui, name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("magi-web: reading %s: %v", name, err)
		}
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".gif"):
		w.Header().Set("Content-Type", "image/gif")
	}
	if strings.Contains(name, ".cache.") && !strings.Contains(name, ".nocache.") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, werr := w.Write(b); werr != nil {
			log.Printf("magi-web: serving %s: %v", name, werr)
		}
		return
	}
	if done := revalidate(w, r, b); done {
		return
	}
	if _, werr := w.Write(b); werr != nil {
		log.Printf("magi-web: serving %s: %v", name, werr)
	}
}

// revalidate sets the ETag and answers 304 when the browser already has these bytes.
//
// no-cache is "ask first", not "do not store": the browser keeps the body and gets a 304 back on
// every check, which costs a round trip with no body and is always right. Used for everything whose
// NAME does not already carry its content — the document, the language packs, the vendored bundles.
// (Things whose name is their content — GWT's <hash>.cache.js — skip this and are immutable.)
//
// Returns true when it has answered and the caller must not write a body.
func revalidate(w http.ResponseWriter, r *http.Request, body []byte) bool {
	sum := sha256.Sum256(body)
	etag := "\"" + hex.EncodeToString(sum[:8]) + "\""
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// say writes a body and reports what went wrong instead of dropping it.
//
// A write that fails means the browser hung up mid-page. There is nobody left to tell and no second
// attempt worth making, but the reason is worth having in the log of a process whose whole job is
// to be watched.
func say(w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		log.Printf("magi-web: writing the response: %v", err)
	}
}
