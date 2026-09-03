package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// intentionalNulls are the fields where null is a THIRD value and not an empty list.
//
// autocomplete's settings are tri-state on purpose: true, false, and "the operator has not chosen",
// which is what a *bool is for. Everything not listed here is a list or an object, and for those
// null and empty are the same picture with different bytes.
var intentionalNulls = map[string]bool{
	"/autocomplete ambient":      true,
	"/autocomplete crossSession": true,
}

// A list that is empty goes out as a list, never as null.
//
// `encoding/json` writes a nil slice as `null`, and a reader cannot tell that from "we have not
// been told yet" — so a screen that draws nothing for null draws nothing forever. Measured on the
// transcript stream: a companion nobody had spoken to was sent `null` every 2.5 seconds and its
// page stayed blank, with no way from the outside to tell an empty conversation from an unanswered
// question. The console's own screens survive it because each of them happens to coerce, which is
// six separate places remembering to; a client written against the documented shape does not get
// that for free.
func TestAnEmptyListIsNeverNull(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "quiet", true)
	f.session("quiet", wd, "hello", 0, true)

	// The read-only routes. A write route needs a body and a live engine behind it; these are the
	// ones a page GETs to paint itself, which is where an empty answer has to be legible.
	routes := []string{"/access", "/autocomplete", "/context", "/council", "/cron", "/files",
		"/fleet", "/git", "/handoffs", "/history", "/jobs", "/look", "/loop", "/mcp", "/me",
		"/meet", "/model", "/plan", "/pr", "/transcript"}

	var bad []string
	for _, route := range routes {
		r := httptest.NewRequest(http.MethodGet, route+"?d="+url.QueryEscape(sock), nil)
		h, ok := f.srv.routes()[route]
		if !ok {
			t.Fatalf("this guard names %s, which the server does not serve — a route list that "+
				"drifts is a guard that quietly stops covering things", route)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusOK {
			continue // not every route answers for every fixture; this guard is about the shape
		}
		var any any
		if json.Unmarshal(w.Body.Bytes(), &any) != nil {
			continue // not JSON (a file body, say)
		}
		for _, path := range nullPaths(any, "") {
			if intentionalNulls[route+" "+strings.TrimPrefix(path, "/")] {
				continue
			}
			bad = append(bad, route+" "+path)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d field(s) go out as null, which a reader cannot tell from \"not known yet\":\n  %s\n\n"+
			"Give the slice an empty value before writing it, or — if null really is a third state —\n"+
			"add it to intentionalNulls with the reason.", len(bad), strings.Join(bad, "\n  "))
	}
}

// nullPaths lists the JSON paths under v whose value is null.
func nullPaths(v any, at string) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if t[k] == nil {
				out = append(out, at+"/"+k)
				continue
			}
			out = append(out, nullPaths(t[k], at+"/"+k)...)
		}
	case []any:
		// Only the first element: a list of a thousand rows with the same shape reports the same
		// field a thousand times, and the first one is the finding.
		if len(t) > 0 {
			out = append(out, nullPaths(t[0], at+fmt.Sprintf("[0]"))...)
		}
	}
	return out
}
