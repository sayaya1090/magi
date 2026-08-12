package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rtFunc adapts a function to http.RoundTripper so Latest (which hardcodes the
// api.github.com host) can be tested without real network.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cannedResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// Latest must hit the releases/latest endpoint and select the asset for THIS
// platform by name prefix — not the first asset, and not another OS/arch.
func TestGitHubLatestPicksPlatformAsset(t *testing.T) {
	asset := AssetName()
	body := fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[
		{"name":"magi_someotheros_otherarch.tar.gz","browser_download_url":"http://x/wrong"},
		{"name":"%s.tar.gz","browser_download_url":"http://x/right"},
		{"name":"checksums.txt","browser_download_url":"http://x/sums"}]}`, asset)
	sums := "beef  " + asset + ".tar.gz\ncafe  magi_someotheros_otherarch.tar.gz\n"
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "http://x/sums" {
			return cannedResp(http.StatusOK, sums), nil
		}
		if !strings.Contains(r.URL.String(), "/repos/o/r/releases/latest") {
			t.Errorf("unexpected URL: %s", r.URL)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q", got)
		}
		return cannedResp(http.StatusOK, body), nil
	})}}
	rel, err := g.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", rel.Version)
	}
	if rel.URL != "http://x/right" {
		t.Errorf("should pick the %s asset, got URL %q", asset, rel.URL)
	}
	// The digest for THIS asset, and not the first line of the file.
	if rel.SHA256 != "beef" {
		t.Errorf("checksum = %q, want the line for %s", rel.SHA256, asset)
	}
}

// A release with no digest list is not installed.
//
// Download has always verified a checksum when it was given one, and nothing ever gave it one, so
// the field sat empty and the check was skipped on every update this program has ever done. The
// list is published for every build of this project; its absence means a release built some other
// way, which is exactly when not to overwrite the running binary.
func TestAReleaseWithoutChecksumsIsRefused(t *testing.T) {
	asset := AssetName()
	body := fmt.Sprintf(`{"tag_name":"v9.9.9","assets":[
		{"name":"%s.tar.gz","browser_download_url":"http://x/right"}]}`, asset)
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return cannedResp(http.StatusOK, body), nil
	})}}
	_, err := g.Latest(context.Background())
	if err == nil {
		t.Fatal("a release with no checksums.txt was accepted for download")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("the reason does not name what is missing: %v", err)
	}
}

// And a list that does not mention this asset is the same answer.
func TestAChecksumListMissingThisAssetIsRefused(t *testing.T) {
	asset := AssetName()
	body := fmt.Sprintf(`{"tag_name":"v9.9.9","assets":[
		{"name":"%s.tar.gz","browser_download_url":"http://x/right"},
		{"name":"checksums.txt","browser_download_url":"http://x/sums"}]}`, asset)
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "http://x/sums" {
			return cannedResp(http.StatusOK, "beef  magi_someotheros_otherarch.tar.gz\n"), nil
		}
		return cannedResp(http.StatusOK, body), nil
	})}}
	if _, err := g.Latest(context.Background()); err == nil {
		t.Fatal("an asset absent from the digest list was accepted for download")
	}
}

func TestGitHubLatestNon200(t *testing.T) {
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return cannedResp(http.StatusNotFound, ""), nil
	})}}
	if _, err := g.Latest(context.Background()); err == nil {
		t.Fatal("expected an error on a non-200 releases status")
	}
}

// A release that has no asset for this platform must be a clear error, not a
// zero-value Release that would later look like an empty download.
func TestGitHubLatestNoMatchingAsset(t *testing.T) {
	body := `{"tag_name":"v1.0.0","assets":[{"name":"unrelated.zip","browser_download_url":"u"}]}`
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return cannedResp(http.StatusOK, body), nil
	})}}
	_, err := g.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no asset") {
		t.Fatalf("want a no-asset error, got %v", err)
	}
}

func TestGitHubLatestBadJSON(t *testing.T) {
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return cannedResp(http.StatusOK, "not json"), nil
	})}}
	if _, err := g.Latest(context.Background()); err == nil {
		t.Fatal("expected a JSON decode error")
	}
}

func TestGitHubDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("BINDATA"))
	}))
	defer srv.Close()
	g := NewGitHubSource("o", "r")
	b, err := g.Download(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(b) != "BINDATA" {
		t.Errorf("downloaded %q, want BINDATA", b)
	}
}

func TestGitHubDownloadNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	g := NewGitHubSource("o", "r")
	if _, err := g.Download(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error on a non-200 download status")
	}
}

// client() falls back to http.DefaultClient when HTTP is nil (so a zero-value
// GitHubSource is still usable).
func TestGitHubClientFallback(t *testing.T) {
	if (&GitHubSource{}).client() != http.DefaultClient {
		t.Error("nil HTTP should fall back to http.DefaultClient")
	}
	custom := &http.Client{}
	if (&GitHubSource{HTTP: custom}).client() != custom {
		t.Error("set HTTP client should be used")
	}
}
