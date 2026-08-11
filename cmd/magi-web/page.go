package main

import (
	"embed"
	"strings"
)

// The whole front end: three files, no build step, nothing fetched at load.
//
// A framework would mean a bundler, a lockfile and a second toolchain for a transcript and a text
// box — and magi is one static binary precisely so there is nothing to install. The page is also
// readable end to end, which matters for something that puts a working directory's contents on a
// port.
//
// # Two views, one document
//
// `/` is the fleet: every daemon this config directory knows about. `/?d=<socket>` is one of them,
// with its transcript and a composer. Same page, switched by the query string and pushState, so
// entering an agent and coming back is instant and there is only ever one idea of what magi looks
// like. The server tells them apart the same way — see server.target.
//
// # The colours are magi's, not a second scheme
//
// Every value below is lifted from styles.go: the same Material Design 3 colour ROLES themed after
// NERV/MAGI — amber primary, cyan accent, warm dark surface — and the same three councillor hues.
// A browser view that invented its own palette would be a second thing to keep in step, and the
// first time somebody retuned the terminal the two would disagree about what magi looks like.
//
// Kept as CSS custom properties with the role names the Go side uses (primary, accent, muted,
// outline, surface, primaryContainer, outlineVariant, error, success, warn), so the correspondence
// is checkable by reading rather than by remembering.
//
// # Phones are not a smaller desktop
//
// The page is installable — a manifest and an icon, so adding it to a home screen opens it without
// browser chrome. That is the whole of magi's answer to the native mobile app other orchestrators
// ship: the thing you actually want on a phone is to see what your agents are doing and to say one
// sentence to one of them, and a page that launches full screen does that without a second client
// to keep in step with the daemon protocol.
//
// The layout starts at one column and grows, rather than starting wide and collapsing. Three things
// on a phone are not preferences: inputs at 16px (anything smaller makes iOS Safari zoom the page
// on focus, and it does not zoom back), the composer padded by env(safe-area-inset-bottom) (a home
// indicator otherwise sits on top of the send button), and controls at least 44px tall. The
// transcript's label column folds above its text below 640px, because five and a half characters of
// gutter is most of a narrow screen.
//
// # Three files, stitched here
//
// page.html, page.css and page.js, embedded and assembled into one document at startup. It was one
// Go raw string of five and a half thousand lines, which cost two things every day: no editor knew
// what language it was reading, and a raw string cannot contain a backtick — so the page could not
// use a template literal, and a comment that happened to quote one broke the build. Measured, more
// than once.
//
// Assembled rather than served as three requests, deliberately. One document is one round trip on
// a phone over a tunnel, the CSS cannot arrive after the first paint, and the demo export stays a
// single file somebody can open.
//
// Nothing is interpolated but the two markers below. The page used to substitute a session id, and
// the format verb that did it read a CSS width as a verb and broke the build; there is nothing to
// substitute now because the page learns what it is looking at from /fleet.

//go:embed page.html page.css page.js
var pageFS embed.FS

// indexHTML is the assembled document. A variable rather than a const because it is stitched at
// startup; everything that reads it — the server, the demo export, the tests that walk the markup
// for ids and fetched paths — sees exactly what a browser gets.
var indexHTML = assemblePage()

// assemblePage puts the stylesheet and the module back where the markup says they go.
//
// Panics on a missing marker, at startup, in every build: the alternative is a console that serves
// a page with no styles or no script and looks merely broken. This runs before anything is served,
// so the failure is a process that does not start rather than a screen that does not work.
func assemblePage() string {
	read := func(name string) string {
		b, err := pageFS.ReadFile(name)
		if err != nil {
			panic("magi-web: " + name + ": " + err.Error())
		}
		return string(b)
	}
	out := read("page.html")
	for marker, file := range map[string]string{"<!--magi:css-->": "page.css", "<!--magi:js-->": "page.js"} {
		if !strings.Contains(out, marker) {
			panic("magi-web: page.html has no " + marker + " for " + file)
		}
		out = strings.Replace(out, marker, read(file), 1)
	}
	return out
}

// The icons go in after the page is assembled, and they have to: the sprite is set by an init in a
// generated file, and every package-level variable in Go — indexHTML included — is initialised
// BEFORE any init runs. Injected inside assemblePage the sprite was always the empty string, which
// looked exactly like a build with no licence and was not one. Measured: 6 symbols baked, 0 in the
// page.
//
// This init is in page.go, which sorts after icons_gen.go, and init functions run in file order.
// TestTheSpriteReachesThePage holds that: it fails if this ordering ever stops being true.
func init() { indexHTML = withSprite(indexHTML) }
