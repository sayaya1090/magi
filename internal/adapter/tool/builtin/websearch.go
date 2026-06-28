package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// WebSearch runs a web search and returns the top results (title, url, snippet) so
// the agent can find docs, library APIs, and error fixes — webfetch only retrieves a
// URL you already know. Keyless: it queries DuckDuckGo's HTML endpoint. Network →
// permission-gated.
type WebSearch struct {
	// HTTP is overridable for tests; nil uses a default client.
	HTTP *http.Client
}

type webSearchArgs struct {
	Query string `json:"query"`
	Count int    `json:"count"` // max results (default 5, max 10)
}

func (WebSearch) Name() string { return "websearch" }
func (WebSearch) Description() string {
	return "Search the web and return the top results (title, url, snippet). Use to find documentation, library APIs, or fixes for errors. Use webfetch afterward to read a result in full. Uses Brave or Tavily when BRAVE_API_KEY / TAVILY_API_KEY is set, else keyless DuckDuckGo."
}
func (WebSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"count":{"type":"integer","description":"max results (default 5, max 10)"}},"required":["query"]}`)
}

func (w WebSearch) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a webSearchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return errResult("", "query is required"), nil
	}
	count := a.Count
	if count <= 0 {
		count = 5
	}
	if count > 10 {
		count = 10
	}

	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	// Provider: a paid API when its key is configured (better quality / no scraping),
	// else keyless DuckDuckGo.
	var (
		results  []searchResult
		provider string
		err2     error
	)
	switch {
	case os.Getenv("BRAVE_API_KEY") != "":
		provider = "brave"
		results, err2 = braveSearch(ctx, client, os.Getenv("BRAVE_API_KEY"), a.Query, count)
	case os.Getenv("TAVILY_API_KEY") != "":
		provider = "tavily"
		results, err2 = tavilySearch(ctx, client, os.Getenv("TAVILY_API_KEY"), a.Query, count)
	default:
		provider = "duckduckgo"
		results, err2 = ddgSearch(ctx, client, a.Query)
	}
	if err2 != nil {
		return errResult("", "search ("+provider+") failed: "+err2.Error()), nil
	}
	if len(results) == 0 {
		return okText("", "no results"), nil
	}
	if len(results) > count {
		results = results[:count]
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
	}
	return okText("", strings.TrimRight(b.String(), "\n")), nil
}

type searchResult struct {
	Title, URL, Snippet string
}

// ddgSearch is the keyless provider: it scrapes DuckDuckGo's HTML endpoint.
func ddgSearch(ctx context.Context, client *http.Client, query string) ([]searchResult, error) {
	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// A browser-like UA — the endpoint returns a blocked page to unknown agents.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; magi/agent)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	return parseDDGResults(string(body)), nil
}

// braveSearch uses the Brave Search API (X-Subscription-Token auth).
func braveSearch(ctx context.Context, client *http.Client, key, query string, count int) ([]searchResult, error) {
	endpoint := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?count=%d&q=%s", count, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http status %s", resp.Status)
	}
	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&data); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(data.Web.Results))
	for _, r := range data.Web.Results {
		out = append(out, searchResult{Title: cleanHTML(r.Title), URL: r.URL, Snippet: cleanHTML(r.Description)})
	}
	return out, nil
}

// tavilySearch uses the Tavily Search API (JSON body with the key).
func tavilySearch(ctx context.Context, client *http.Client, key, query string, count int) ([]searchResult, error) {
	reqBody, _ := json.Marshal(map[string]any{"api_key": key, "query": query, "max_results": count})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http status %s", resp.Status)
	}
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&data); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(data.Results))
	for _, r := range data.Results {
		out = append(out, searchResult{Title: cleanHTML(r.Title), URL: r.URL, Snippet: cleanHTML(r.Content)})
	}
	return out, nil
}

var (
	ddgLinkRE    = regexp.MustCompile(`(?s)class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>`)
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
)

// parseDDGResults extracts results from DuckDuckGo HTML. Result links are wrapped in
// a redirect ("//duckduckgo.com/l/?uddg=<encoded>"), which is decoded back to the
// real URL. Snippets are matched positionally to the links.
func parseDDGResults(html string) []searchResult {
	links := ddgLinkRE.FindAllStringSubmatch(html, -1)
	snips := ddgSnippetRE.FindAllStringSubmatch(html, -1)
	out := make([]searchResult, 0, len(links))
	for i, m := range links {
		r := searchResult{Title: cleanHTML(m[2]), URL: decodeDDGURL(m[1])}
		if i < len(snips) {
			r.Snippet = cleanHTML(snips[i][1])
		}
		if r.URL != "" {
			out = append(out, r)
		}
	}
	return out
}

// decodeDDGURL unwraps a DuckDuckGo redirect link to the real target URL.
func decodeDDGURL(href string) string {
	if i := strings.Index(href, "uddg="); i >= 0 {
		v := href[i+len("uddg="):]
		if amp := strings.IndexByte(v, '&'); amp >= 0 {
			v = v[:amp]
		}
		if dec, err := url.QueryUnescape(v); err == nil {
			return dec
		}
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

// cleanHTML strips tags and unescapes entities to plain text.
func cleanHTML(s string) string {
	s = htmlTagRE.ReplaceAllString(s, "")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#x27;", "'", "&#39;", "'", "&nbsp;", " ").Replace(s)
	return strings.TrimSpace(s)
}
