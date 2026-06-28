package builtin

import (
	"context"
	"github.com/sayaya1090/magi/internal/port"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cannedClient(status int, body string) *http.Client {
	return &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func TestBraveSearch(t *testing.T) {
	body := `{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"the <b>Go</b> site"},{"title":"Pkg","url":"https://pkg.go.dev","description":"index"}]}}`
	got, err := braveSearch(context.Background(), cannedClient(200, body), "key", "go", 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("braveSearch = %v, err=%v", got, err)
	}
	if got[0].URL != "https://go.dev" || got[0].Title != "Go" || got[0].Snippet != "the Go site" {
		t.Errorf("brave result[0] = %+v", got[0])
	}
	// HTTP error status surfaces as an error.
	if _, err := braveSearch(context.Background(), cannedClient(403, ""), "key", "go", 5); err == nil {
		t.Error("403 should error")
	}
}

func TestTavilySearch(t *testing.T) {
	body := `{"results":[{"title":"T","url":"https://t.io","content":"snippet"}]}`
	got, err := tavilySearch(context.Background(), cannedClient(200, body), "key", "q", 3)
	if err != nil || len(got) != 1 || got[0].URL != "https://t.io" || got[0].Snippet != "snippet" {
		t.Fatalf("tavilySearch = %+v, err=%v", got, err)
	}
}

func TestDDGSearch(t *testing.T) {
	html := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fx.com%2Fa">X</a>` +
		`<a class="result__snippet" href="#">about x</a>`
	got, err := ddgSearch(context.Background(), cannedClient(200, html), "x")
	if err != nil || len(got) != 1 || got[0].URL != "https://x.com/a" {
		t.Fatalf("ddgSearch = %+v, err=%v", got, err)
	}
}

func TestWebSearchExecuteEndToEnd(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k") // route to Brave
	body := `{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"d"}]}}`
	w := WebSearch{HTTP: cannedClient(200, body)}
	r, _ := w.Execute(context.Background(), []byte(`{"query":"go"}`), port.ToolEnv{})
	if r.IsError {
		t.Fatalf("execute errored: %s", resultText(t, r))
	}
	out := resultText(t, r)
	if !strings.Contains(out, "go.dev") || !strings.Contains(out, "1. Go") {
		t.Errorf("websearch output = %q", out)
	}
}
