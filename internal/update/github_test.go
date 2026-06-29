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
		{"name":"%s.tar.gz","browser_download_url":"http://x/right"}]}`, asset)
	g := &GitHubSource{Owner: "o", Repo: "r", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
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
