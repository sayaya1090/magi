package update

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// WithAPIBase must retarget the releases lookup at a GitHub Enterprise host, keeping
// the /repos/<owner>/<repo>/releases/latest path shape.
func TestGitHubWithAPIBaseRoutesToEnterprise(t *testing.T) {
	var gotURL string
	body := fmt.Sprintf(`{"tag_name":"v2.0.0","assets":[{"name":"%s.tar.gz","browser_download_url":"http://x/pub"},`+
		`{"name":"checksums.txt","browser_download_url":"http://x/sums"}]}`, AssetName())
	g := NewGitHubSource("o", "r", WithAPIBase("https://ghe.corp/api/v3/"))
	g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "http://x/sums" {
			return cannedResp(http.StatusOK, sumsFor(AssetName())), nil
		}
		gotURL = r.URL.String()
		return cannedResp(http.StatusOK, body), nil
	})}
	if _, err := g.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if want := "https://ghe.corp/api/v3/repos/o/r/releases/latest"; gotURL != want {
		t.Errorf("Latest URL = %q, want %q (trailing slash on base must be trimmed)", gotURL, want)
	}
}

// With a token, Latest must authenticate and select the asset-API URL (not the
// browser_download_url) so a PRIVATE asset is downloadable.
func TestGitHubWithTokenPicksAssetAPIURLAndAuthorizes(t *testing.T) {
	var auth string
	body := fmt.Sprintf(`{"tag_name":"v3.1.0","assets":[{"name":"%s.tar.gz",`+
		`"browser_download_url":"http://x/pub","url":"https://api.github.com/repos/o/r/releases/assets/42"},`+
		`{"name":"checksums.txt","browser_download_url":"http://x/pubsums","url":"http://x/apisums"}]}`, AssetName())
	g := NewGitHubSource("o", "r", WithToken("secret-tok"))
	var sumsFrom string
	g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		auth = r.Header.Get("Authorization")
		if u := r.URL.String(); u == "http://x/apisums" || u == "http://x/pubsums" {
			sumsFrom = u
			return cannedResp(http.StatusOK, sumsFor(AssetName())), nil
		}
		return cannedResp(http.StatusOK, body), nil
	})}
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if auth != "Bearer secret-tok" {
		t.Errorf("Authorization = %q, want Bearer secret-tok", auth)
	}
	if want := "https://api.github.com/repos/o/r/releases/assets/42"; rel.URL != want {
		t.Errorf("token source should download via the asset-API URL, got %q", rel.URL)
	}
	// The digest list is an asset like any other, so a private release needs it fetched the same
	// authenticated way — read over the public URL it would 403 and the update would fail with a
	// checksum error, which says the wrong thing about a working release.
	if sumsFrom != "http://x/apisums" {
		t.Errorf("the digest list was read from %q, not the asset-API URL", sumsFrom)
	}
}

// sumsFor is a checksums.txt with one line that matches, as goreleaser writes it.
func sumsFor(asset string) string { return "beef  " + asset + ".tar.gz\n" }

// Without a token the public browser_download_url is used and no auth header is sent —
// the anonymous public path stays exactly as before.
func TestGitHubAnonymousKeepsBrowserURL(t *testing.T) {
	var auth string
	body := fmt.Sprintf(`{"tag_name":"v3.1.0","assets":[{"name":"%s.tar.gz",`+
		`"browser_download_url":"http://x/pub","url":"http://x/api"},`+
		`{"name":"checksums.txt","browser_download_url":"http://x/sums"}]}`, AssetName())
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		auth = r.Header.Get("Authorization")
		if r.URL.String() == "http://x/sums" {
			return cannedResp(http.StatusOK, sumsFor(AssetName())), nil
		}
		return cannedResp(http.StatusOK, body), nil
	})}}
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if auth != "" {
		t.Errorf("anonymous source must send no Authorization header, got %q", auth)
	}
	if rel.URL != "http://x/pub" {
		t.Errorf("anonymous source should keep browser_download_url, got %q", rel.URL)
	}
}

// A private download authenticates and requests the raw octet-stream (required for the
// asset-API URL); the anonymous download does neither.
func TestGitHubDownloadTokenHeaders(t *testing.T) {
	check := func(token, wantAuth, wantAccept string) {
		var auth, accept string
		g := NewGitHubSource("o", "r", WithToken(token))
		g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			auth, accept = r.Header.Get("Authorization"), r.Header.Get("Accept")
			return cannedResp(http.StatusOK, "BIN"), nil
		})}
		b, err := g.Download(context.Background(), "http://x/a")
		if err != nil || string(b) != "BIN" {
			t.Fatalf("Download token=%q: b=%q err=%v", token, b, err)
		}
		if auth != wantAuth {
			t.Errorf("token=%q Authorization = %q, want %q", token, auth, wantAuth)
		}
		if accept != wantAccept {
			t.Errorf("token=%q Accept = %q, want %q", token, accept, wantAccept)
		}
	}
	check("tok", "Bearer tok", "application/octet-stream")
	check("", "", "")
}

// Option constructors must be safe against empty/degenerate input.
func TestGitHubOptionEdgeCases(t *testing.T) {
	g := NewGitHubSource("o", "r", WithAPIBase(""), WithAPIBase("https://h/api/v3"), WithAPIBase("/"))
	if g.apiBase() != "https://h/api/v3" {
		t.Errorf("empty/slash-only WithAPIBase must not clobber a real one, got %q", g.apiBase())
	}
	if NewGitHubSource("o", "r").apiBase() != defaultGitHubAPIBase {
		t.Errorf("no option must keep the public default %q", defaultGitHubAPIBase)
	}
}

// **A non-default API base does not buy a weaker check.**
//
// ⚠ The runtime door for this (`MAGI_RELEASE_API_BASE`, read in cmd/magi) exists so a fork — or a
// test — can point every self-update path at another host. The rule that keeps it from being a way
// to install whatever that host offers is this one: the release is refused unless `checksums.txt`
// carries a line for the asset this build asked for. Pointing somewhere else moves the SOURCE only.
func TestANonDefaultBaseStillDemandsAChecksum(t *testing.T) {
	asset := AssetName() + ".tar.gz"
	// A release with the asset and NO checksums.txt, served from an Enterprise-shaped base.
	body := fmt.Sprintf(`{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":"http://x/pub"}]}`, asset)
	g := NewGitHubSource("o", "r", WithAPIBase("https://local.test/api/v3"))
	g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.String(), "https://local.test/api/v3") {
			return nil, fmt.Errorf("asked %s — the base did not travel", r.URL)
		}
		return cannedResp(http.StatusOK, body), nil
	})}
	if _, err := g.Latest(context.Background()); err == nil {
		t.Fatal("a release with no checksums.txt was accepted from a non-default base")
	} else if !strings.Contains(err.Error(), checksumsAsset) {
		t.Errorf("the refusal does not name what was missing: %v", err)
	}
}
