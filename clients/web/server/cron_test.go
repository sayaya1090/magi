package main

import (
	"net/http"
	"net/url"
	"testing"
)

// A cron job's name becomes a [cron.<name>] TOML header the same way a profile's does, so it is held
// to the same bare-key allowlist. This pins that cronWrite actually applies it — a newline name (the
// audit's finding) would otherwise split the header and leave the target companion's config.toml
// unparseable, and cron, unlike /profiles, is not shared-console-refused, so the guard matters more.
func TestACronNameThatWouldBreakTheFileIsRefused(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	q := "/cron?d=" + url.QueryEscape(sock)

	for _, name := range []string{"foo\nbar", "a.b", "my job", "a,b", "café", ""} {
		w := post(t, f.srv, f.srv.cron, q, url.Values{
			"name": {name}, "schedule": {"@daily"}, "prompt": {"do the thing"}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("cron name %q: answered %d, want 400 (%s)", name, w.Code, w.Body.String())
		}
	}
	// A valid bare-key name is not over-rejected on the name check (it may fail later for other
	// reasons, but not with the name 400).
	w := post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"nightly-sweep_2"}, "schedule": {"@daily"}, "prompt": {"do the thing"}})
	if w.Code == http.StatusBadRequest {
		t.Errorf("a valid bare-key cron name was refused: %s", w.Body.String())
	}
}
