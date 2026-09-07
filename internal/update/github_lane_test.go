package update

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// laneServer answers the three URLs Latest may touch, and records which release endpoint was asked
// for. tag is what badges/core-latest.txt says; empty means the file is not there.
func laneServer(t *testing.T, tag string) (*GitHubSource, *string) {
	t.Helper()
	asset := AssetName()
	var asked string
	body := func(v string) string {
		return fmt.Sprintf(`{"tag_name":%q,"assets":[
			{"name":"%s.tar.gz","browser_download_url":"http://x/bin"},
			{"name":"checksums.txt","browser_download_url":"http://x/sums"}]}`, v, asset)
	}
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "raw.githubusercontent.com"):
			if tag == "" {
				return cannedResp(http.StatusNotFound, "404"), nil
			}
			return cannedResp(http.StatusOK, tag+"\n"), nil
		case u == "http://x/sums":
			return cannedResp(http.StatusOK, "beef  "+asset+".tar.gz\n"), nil
		case strings.Contains(u, "/releases/tags/"):
			asked = u[strings.Index(u, "/releases/"):]
			return cannedResp(http.StatusOK, body(strings.TrimPrefix(asked, "/releases/tags/"))), nil
		case strings.Contains(u, "/releases/latest"):
			asked = "/releases/latest"
			return cannedResp(http.StatusOK, body("office-v0.0.4")), nil
		}
		t.Errorf("unexpected URL: %s", u)
		return cannedResp(http.StatusNotFound, ""), nil
	})}}
	return g, &asked
}

// The updater asks for the CORE's latest release, not the repository's.
//
// GitHub's /releases/latest is the newest by DATE across every tag, and this repository runs four
// trains — v*, web-v*, jetbrains-v*, office-v*. Measured 2026-09-08, minutes after an office
// release: `magi --update-core` answered "no asset for magi_darwin_arm64 in office-v0.0.4". The
// message was honest and the update did not happen.
func TestTheUpdaterFollowsTheCoreTrain(t *testing.T) {
	g, asked := laneServer(t, "v0.41.0")
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Version != "v0.41.0" {
		t.Errorf("resolved %q — the updater is following whichever train shipped last", rel.Version)
	}
	if *asked != "/releases/tags/v0.41.0" {
		t.Errorf("asked %q, want the core tag by name", *asked)
	}
}

// With no such file — a fork with one train, a mirror without the branch — it does what it always
// did. The pointer is an improvement where it exists, not a new requirement.
func TestWithoutThePointerItUsesTheEndpoint(t *testing.T) {
	g, asked := laneServer(t, "")
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if *asked != "/releases/latest" {
		t.Errorf("asked %q, want the plain endpoint when there is no pointer", *asked)
	}
	if rel.Version != "office-v0.0.4" {
		t.Errorf("version = %q", rel.Version)
	}
}

// A body that is not a tag is not treated as one.
//
// Being wrong here pins the updater to a tag that does not exist, so anything unexpected has to
// read as "do not know" and fall back — a mirror answering an HTML error page with status 200 is
// the case this is guarding.
func TestSomethingThatIsNotATagIsNotOne(t *testing.T) {
	for _, junk := range []string{
		"<!DOCTYPE html><html>404</html>",
		"v1.0 and some words",
		"../../etc/passwd",
		"   ",
	} {
		g, asked := laneServer(t, junk)
		if _, err := g.Latest(context.Background()); err != nil {
			t.Fatalf("Latest(%q): %v", junk, err)
		}
		if *asked != "/releases/latest" {
			t.Errorf("%q was taken as a tag (asked %q)", junk, *asked)
		}
	}
}
