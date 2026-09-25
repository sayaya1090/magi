package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/sayaya1090/magi/internal/webassets"
)

//go:embed fonts/*.woff2
var fontFS embed.FS

//go:embed vendor/*.js i18n/*.json
var assetFS embed.FS

// asset serves the vendored javascript and the language packs.
//
// Both are in the binary for the same reason the typeface is: a page that fetched them from
// somewhere else would depend on that machine being up, tell it when you look at your agents, and
// behave differently on a laptop with no route out. See vendor/README.md for how the bundle is
// built — once, by hand, from a pinned version, with its hash written down.
func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	b, err := assetFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	// Cached and REVALIDATED, which are not the same thing. These were served immutable for a day
	// on the reasoning that they change with a release — and the consequence is that they change
	// with a release and nobody sees it: an upgraded console served its new pack to a browser that
	// went on using yesterday's for up to a day, so a label added in the same build rendered as its
	// own dotted key. Observed twice while working on this page, both times read as a bug in the
	// page rather than as a stale file.
	//
	// no-cache is "ask first", not "do not store". The browser keeps the bytes and gets a 304 back
	// on every check, which costs a round trip with no body and is always right.
	sum := sha256.Sum256(b)
	etag := "\"" + hex.EncodeToString(sum[:8]) + "\""
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if _, err := w.Write(b); err != nil {
		log.Printf("magi-web: serving %s: %v", name, err)
	}
}

// font serves the display face the page sets its headlines in.
//
// Embedded and served from here rather than fetched from a font CDN. A viewer that reached out for
// its typeface would make this page's appearance depend on a machine that is not yours, tell that
// machine when you look at your agents, and fall back to something else entirely on a laptop with
// no route out — and this binary exists to hold everything it serves. See fonts/README.md for how
// the files were built, and fonts/OFL.txt for the licence that travels with them.
func (s *server) font(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	b, err := fontFS.ReadFile("fonts/" + name)
	if err != nil || !strings.HasSuffix(name, ".woff2") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	// The bytes are baked into the binary, so they cannot change without the binary changing —
	// and a page that re-fetches its typeface every poll is a page that flashes.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(b); err != nil {
		log.Printf("magi-web: writing %s: %v", name, err)
	}
}

// manifest makes the page installable: added to a phone's home screen it opens without the browser
// chrome, which is the difference between "a website about my agents" and something you reach for.
//
// Served rather than inlined as a data: URI because iOS ignores a manifest it cannot fetch, and it
// is small enough that a route costs less than the explanation of the workaround would.
// webassets.Manifest and webassets.Icon are package-level so the static demo can write the same bytes this
// server answers with. They were consts inside their handlers, and the demo shipped without either
// — found by a check that walks every path the page references.

// webassets.IconMaskable is the same three councillors with the plate back under them.
//
// A maskable icon is not a picture with rounded corners applied — the platform crops it to
// whatever shape it likes, circle on one launcher and squircle on the next, and a transparent one
// is cropped to nothing with the launcher filling the rest in a colour this file did not choose.
// So this one keeps the ground, and it is the only place that needs it.

func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	// display:standalone and a theme colour that matches the page's BACKGROUND — the masthead sits
	// on it directly now, so a surface colour here would draw a band the page does not have. start_url is the fleet: the phone is where you check on things.
	if _, err := io.WriteString(w, webassets.Manifest); err != nil {
		log.Printf("magi-web: writing the manifest: %v", err)
	}
}

// icon is the mark: the three councillors, in their own hues, on nothing.
//
// SVG so there is one file for every size, and drawn here rather than shipped as a PNG because a
// binary asset in a source tree is a thing nobody can review. The maskable safe zone is the middle
// 80%, so nothing meaningful goes near the edge.
//
// No ground under it. This is the favicon, and a tab strip is whatever colour the browser and its
// theme make it — a dark brown square there is a sticker on the tab rather than a mark in it. The
// same file is the notification icon, where a launcher tints what it is given and a plate is a
// plate. Where a ground IS required, there is a second file that has one; see webassets.IconMaskable.
func (s *server) icon(w http.ResponseWriter, r *http.Request) {
	s.svg(w, webassets.Icon)
}

// iconMaskable is the same mark for a home screen, which crops it.
func (s *server) iconMaskable(w http.ResponseWriter, r *http.Request) {
	s.svg(w, webassets.IconMaskable)
}

func (s *server) svg(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	if _, err := io.WriteString(w, body); err != nil {
		log.Printf("magi-web: writing an icon: %v", err)
	}
}
