package update

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// WithAPIBase must retarget the releases lookup at a GitHub Enterprise host, keeping
// the /repos/<owner>/<repo>/releases/latest path shape.
func TestGitHubWithAPIBaseRoutesToEnterprise(t *testing.T) {
	var gotURL string
	body := fmt.Sprintf(`{"tag_name":"v2.0.0","assets":[{"name":"%s.tar.gz","browser_download_url":"http://x/pub"}]}`, AssetName())
	g := NewGitHubSource("o", "r", WithAPIBase("https://ghe.corp/api/v3/"))
	g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
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
		`"browser_download_url":"http://x/pub","url":"https://api.github.com/repos/o/r/releases/assets/42"}]}`, AssetName())
	g := NewGitHubSource("o", "r", WithToken("secret-tok"))
	g.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		auth = r.Header.Get("Authorization")
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
}

// Without a token the public browser_download_url is used and no auth header is sent —
// the anonymous public path stays exactly as before.
func TestGitHubAnonymousKeepsBrowserURL(t *testing.T) {
	var auth string
	body := fmt.Sprintf(`{"tag_name":"v3.1.0","assets":[{"name":"%s.tar.gz",`+
		`"browser_download_url":"http://x/pub","url":"http://x/api"}]}`, AssetName())
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		auth = r.Header.Get("Authorization")
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
