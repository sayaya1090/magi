package builtin

import "testing"

func TestParseDDGResults(t *testing.T) {
	// A trimmed sample of DuckDuckGo HTML: redirect-wrapped links + snippets.
	html := `
<div class="result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=x">Go <b>Documentation</b></a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=x">The Go <b>programming</b> language docs.</a>
</div>
<div class="result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F">pkg.go.dev</a>
  <a class="result__snippet" href="#">Package index.</a>
</div>`
	got := parseDDGResults(html)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].URL != "https://go.dev/doc/" {
		t.Errorf("url[0] = %q, want decoded https://go.dev/doc/", got[0].URL)
	}
	if got[0].Title != "Go Documentation" { // tags stripped
		t.Errorf("title[0] = %q", got[0].Title)
	}
	if got[0].Snippet != "The Go programming language docs." {
		t.Errorf("snippet[0] = %q", got[0].Snippet)
	}
	if got[1].URL != "https://pkg.go.dev/" {
		t.Errorf("url[1] = %q", got[1].URL)
	}
}

func TestDecodeDDGURL(t *testing.T) {
	if got := decodeDDGURL("//duckduckgo.com/l/?uddg=https%3A%2F%2Fx.com%2Fa&rut=y"); got != "https://x.com/a" {
		t.Errorf("decode = %q", got)
	}
	if got := decodeDDGURL("https://plain.example/x"); got != "https://plain.example/x" {
		t.Errorf("plain url should pass through, got %q", got)
	}
}
