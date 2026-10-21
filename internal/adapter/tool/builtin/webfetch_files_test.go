package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A 1×1 PNG: the signature, then an IHDR saying 1 by 1.
var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func fetchOut(t *testing.T, w WebFetch, args webFetchArgs) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, _ := w.Execute(context.Background(), raw, port.ToolEnv{})
	var out string
	_ = json.Unmarshal(res.Content, &out)
	return out, res.IsError
}

// A picture is not text: it is saved as a file and the answer is the path an image tool reads.
func TestWebFetchSavesAPictureAndAnswersItsPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream") // the header lies; the bytes decide
		_, _ = w.Write(onePixelPNG)
	}))
	defer srv.Close()
	dir := t.TempDir()
	out, isErr := fetchOut(t, WebFetch{SaveDir: func() (string, error) { return dir, nil }}, webFetchArgs{URL: srv.URL + "/fig1"})
	if isErr {
		t.Fatalf("webfetch: %s", out)
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, dir) {
			path = line
		}
	}
	if !strings.HasSuffix(path, ".png") || !strings.Contains(out, "1×1") {
		t.Fatalf("answer does not name a saved .png with its size: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, onePixelPNG) {
		t.Fatalf("saved file differs from what was served (err %v)", err)
	}
}

// Long text is paged instead of cut, and an HTML page lists its images as absolute URLs.
func TestWebFetchPagesLongTextAndListsImages(t *testing.T) {
	long := strings.Repeat("가", webFetchPage+10) + "끝"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body><img src="x/fig1.png" alt="f"><img src='data:image/png;base64,AA'><p>`+long+`</p></body></html>`)
	}))
	defer srv.Close()
	out, isErr := fetchOut(t, WebFetch{}, webFetchArgs{URL: srv.URL + "/paper/"})
	if isErr || strings.Contains(out, "끝") || !strings.Contains(out, "offset") {
		t.Fatalf("first page should stop short of the end and say how to go on: err=%v tail=%q", isErr, out[len(out)-300:])
	}
	if !strings.Contains(out, srv.URL+"/paper/x/fig1.png") || strings.Contains(out, "data:image") {
		t.Fatalf("image list missing the absolute figure URL or carrying a data: URL")
	}
	rest, _ := fetchOut(t, WebFetch{}, webFetchArgs{URL: srv.URL + "/paper/", Offset: webFetchPage})
	if !strings.Contains(rest, "끝") {
		t.Fatalf("offset page did not reach the end: %q", rest)
	}
}

// githubFake answers the GitHub API and records which requests carried the login.
type githubFake struct{ authed map[string]string }

func (g *githubFake) RoundTrip(r *http.Request) (*http.Response, error) {
	g.authed[r.URL.Host+r.URL.Path] = r.Header.Get("Authorization")
	body, status, hdr := "", 200, http.Header{}
	switch {
	case r.URL.Host == "api.github.com" && r.URL.Path == "/repos/o/r/issues/43":
		body = `{"title":"초안","state":"open","body":"본문","updated_at":"2026-10-21","user":{"login":"me"}}`
	case r.URL.Host == "api.github.com" && r.URL.Path == "/repos/o/r/issues/43/comments":
		body = `[{"body":"고친 부분","created_at":"2026-10-21","user":{"login":"me"}}]`
	case r.URL.Host == "github.com" && r.URL.Path == "/user-attachments/assets/1":
		status = http.StatusFound
		hdr.Set("Location", "https://objects.example.net/signed/1")
	case r.URL.Host == "github.com" && r.URL.Path == "/o/r/raw/main/fig.png":
		status = http.StatusFound // a subdomain of the asked host: Go itself would forward the login here
		hdr.Set("Location", "https://media.github.com/o/r/fig.png")
	case r.URL.Host == "objects.example.net" || r.URL.Host == "media.github.com":
		hdr.Set("Content-Type", "image/png")
		return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(bytes.NewReader(onePixelPNG)), Request: r}, nil
	default:
		status = 404
	}
	return &http.Response{StatusCode: status, Header: hdr, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// An issue URL reads through the API with the gh login — body and comments — and the login is
// not carried to the host a GitHub link redirects to.
func TestWebFetchReadsAPrivateIssueWithTheGhLogin(t *testing.T) {
	fake := &githubFake{authed: map[string]string{}}
	w := WebFetch{Transport: fake, Token: func() string { return "tok" }, SaveDir: func() (string, error) { return t.TempDir(), nil }}
	out, isErr := fetchOut(t, w, webFetchArgs{URL: "https://github.com/o/r/issues/43"})
	if isErr || !strings.Contains(out, "# 초안") || !strings.Contains(out, "본문") || !strings.Contains(out, "고친 부분") {
		t.Fatalf("issue answer missing title, body or comment: %q", out)
	}
	if fake.authed["api.github.com/repos/o/r/issues/43"] != "Bearer tok" {
		t.Fatalf("the issue was not read with the gh login: %v", fake.authed)
	}
	if _, isErr := fetchOut(t, w, webFetchArgs{URL: "https://github.com/user-attachments/assets/1"}); isErr {
		t.Fatal("attachment fetch failed")
	}
	if fake.authed["github.com/user-attachments/assets/1"] != "Bearer tok" || fake.authed["objects.example.net/signed/1"] != "" {
		t.Fatalf("login must go to GitHub and stop at the redirect: %v", fake.authed)
	}
	if _, isErr := fetchOut(t, w, webFetchArgs{URL: "https://github.com/o/r/raw/main/fig.png"}); isErr {
		t.Fatal("raw fetch failed")
	}
	if fake.authed["media.github.com/o/r/fig.png"] != "" {
		t.Fatalf("login followed a redirect to a GitHub subdomain that is not one of the three API hosts: %v", fake.authed)
	}
}

// Without a login a private repository's 404 says why, instead of "not found" alone.
func TestWebFetchSaysA404MayBeAMissingLogin(t *testing.T) {
	w := WebFetch{Transport: &githubFake{authed: map[string]string{}}, Token: func() string { return "" }}
	out, isErr := fetchOut(t, w, webFetchArgs{URL: "https://github.com/o/r/issues/99"})
	if !isErr || !strings.Contains(out, "gh auth login") {
		t.Fatalf("404 without a login should point at gh auth login: %q", out)
	}
}

// An SVG is XML but it is a picture: it is saved, rendered to PNG for image tools, and both paths
// are answered — or the answer says no PNG could be made. arXiv lists such figures as <object data>.
func TestWebFetchSavesAnSVGAndRendersAPNG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".svg") {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body><object type="image/svg+xml" data="2303.11366v4/fig.svg"></object><p>paper</p></body></html>`)
	}))
	defer srv.Close()
	page, _ := fetchOut(t, WebFetch{}, webFetchArgs{URL: srv.URL + "/html/2303.11366v4"})
	if !strings.Contains(page, srv.URL+"/html/2303.11366v4/fig.svg") {
		t.Fatalf("an <object data> figure is not listed: %q", page)
	}
	dir := t.TempDir()
	var rendered string
	w := WebFetch{SaveDir: func() (string, error) { return dir, nil }, RenderSVG: func(svg, png string) error {
		rendered = png
		return os.WriteFile(png, onePixelPNG, 0o600)
	}}
	out, isErr := fetchOut(t, w, webFetchArgs{URL: srv.URL + "/html/2303.11366v4/fig.svg"})
	if isErr || rendered == "" || !strings.Contains(out, rendered) || strings.Contains(out, "BEGIN UNTRUSTED") {
		t.Fatalf("SVG should be saved and its PNG path answered, not returned as text: %q", out)
	}
	w.RenderSVG = func(string, string) error { return os.ErrNotExist }
	out, _ = fetchOut(t, w, webFetchArgs{URL: srv.URL + "/html/2303.11366v4/fig.svg"})
	if !strings.Contains(out, "No PNG was made") {
		t.Fatalf("a failed render must say the image tool cannot place it: %q", out)
	}
}

// GH_TOKEN wins over gh, so a machine without gh (Windows, CI) can still read private repositories.
func TestGitHubLoginPrefersTheEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", " from-env ")
	if got := ghAuthToken(); got != "from-env" {
		t.Fatalf("GITHUB_TOKEN not used: %q", got)
	}
	t.Setenv("GH_TOKEN", "gh-first")
	if got := ghAuthToken(); got != "gh-first" {
		t.Fatalf("GH_TOKEN should come first: %q", got)
	}
}
