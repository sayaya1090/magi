package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultGitHubAPIBase is the public GitHub REST API host. GitHub Enterprise Server
// exposes the same API under https://<host>/api/v3, which a caller supplies via
// WithAPIBase.
const defaultGitHubAPIBase = "https://api.github.com"

// GitHubSource fetches releases from a GitHub repository's releases API. It targets
// public github.com by default; WithAPIBase retargets it at a GitHub Enterprise host
// and WithToken authenticates it for private/Enterprise repos.
type GitHubSource struct {
	Owner string
	Repo  string
	HTTP  *http.Client
	// APIBase is the REST API root (no trailing slash), e.g. https://api.github.com or
	// https://ghe.corp/api/v3. Empty means the public default.
	APIBase string
	// Token, when set, is sent as a Bearer credential and switches asset downloads to
	// the authenticated asset-API path (see Latest/Download). Empty = anonymous, public.
	Token string
}

// GitHubOption configures a GitHubSource at construction. Options keep
// NewGitHubSource(owner, repo) backward compatible: existing call sites compile
// unchanged, while a fork adds WithAPIBase/WithToken for Enterprise/private repos.
type GitHubOption func(*GitHubSource)

// WithAPIBase points the source at a non-default REST API root (GitHub Enterprise).
// A trailing slash is trimmed. Empty is ignored (keeps the public default).
func WithAPIBase(base string) GitHubOption {
	return func(g *GitHubSource) {
		if b := strings.TrimRight(base, "/"); b != "" {
			g.APIBase = b
		}
	}
}

// WithToken sets the credential used for both the releases lookup and the asset
// download, enabling private and Enterprise repositories.
func WithToken(token string) GitHubOption {
	return func(g *GitHubSource) { g.Token = token }
}

// NewGitHubSource returns a source for owner/repo. Extra options (WithAPIBase,
// WithToken) are applied in order; with none it targets the public github.com API
// anonymously, exactly as before.
func NewGitHubSource(owner, repo string, opts ...GitHubOption) *GitHubSource {
	g := &GitHubSource{Owner: owner, Repo: repo, HTTP: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(g)
	}
	return g
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		// URL is the browser download URL (works anonymously for public releases).
		URL string `json:"browser_download_url"`
		// APIURL is the asset's REST API URL, required to download a PRIVATE asset with
		// a token + Accept: application/octet-stream (browser_download_url 403s there).
		APIURL string `json:"url"`
	} `json:"assets"`
}

// Latest returns the newest release's asset for the current platform. For a private
// source (Token set) it returns the asset-API URL so Download can authenticate;
// otherwise it returns the public browser_download_url.
func (g *GitHubSource) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", g.apiBase(), g.Owner, g.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	g.authorize(req)
	resp, err := g.client().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github: releases status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}

	want := AssetName() // e.g. magi_Darwin_arm64
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, want) {
			dl := a.URL
			if g.Token != "" && a.APIURL != "" {
				dl = a.APIURL // authenticated, private-capable download path
			}
			return Release{Version: rel.TagName, URL: dl}, nil
		}
	}
	return Release{}, fmt.Errorf("github: no asset for %s in %s", want, rel.TagName)
}

// Download fetches the asset bytes. With a token it authenticates and requests the raw
// octet-stream (required for the asset-API URL of a private release); anonymously it is
// a plain GET of the public URL.
func (g *GitHubSource) Download(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if g.Token != "" {
		req.Header.Set("Accept", "application/octet-stream")
	}
	g.authorize(req)
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: download status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// apiBase returns the configured REST API root or the public default.
func (g *GitHubSource) apiBase() string {
	if g.APIBase != "" {
		return g.APIBase
	}
	return defaultGitHubAPIBase
}

// authorize attaches the Bearer credential when a token is configured; a no-op for
// anonymous public access.
func (g *GitHubSource) authorize(req *http.Request) {
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
}

func (g *GitHubSource) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}
