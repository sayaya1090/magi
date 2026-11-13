// Package diagram contains the same offline renderer used by every graphical client.
package diagram

import (
	"embed"
	"fmt"
	"html"
)

//go:embed web/*
var assets embed.FS

func Script() []byte {
	engine, _ := assets.ReadFile("web/mermaid.min.js")
	view, _ := assets.ReadFile("web/view.js")
	return append(append(engine, '\n'), view...)
}

func HTML(title, source string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>%s</title><style>body{font:16px system-ui;margin:24px;max-width:1200px}button:focus-visible{outline:3px solid Highlight}pre{white-space:pre-wrap}</style></head><body><h1>%s</h1><pre><code data-lang="mermaid">%s</code></pre><script>%s</script></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(source), Script())
}
