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

// GitHubSource fetches releases from a GitHub repository's releases API.
type GitHubSource struct {
	Owner string
	Repo  string
	HTTP  *http.Client
}

// NewGitHubSource returns a source for owner/repo.
func NewGitHubSource(owner, repo string) *GitHubSource {
	return &GitHubSource{Owner: owner, Repo: repo, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest returns the newest release's asset for the current platform.
func (g *GitHubSource) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", g.Owner, g.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
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
			return Release{Version: rel.TagName, URL: a.URL}, nil
		}
	}
	return Release{}, fmt.Errorf("github: no asset for %s in %s", want, rel.TagName)
}

// Download fetches the asset bytes.
func (g *GitHubSource) Download(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

func (g *GitHubSource) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}
