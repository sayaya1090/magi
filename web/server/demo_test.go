// The demo is the only build output a stranger sees (Pages, under next/), and nothing in the
// browser tells it when it is wrong: a path the shim does not know answers 404 and the screen
// just stays empty. Three such holes shipped before these tests existed — /console, /context,
// and the typeface. So both tests here read the real thing rather than a description of it: the
// first asks the modules themselves what they fetch, the second runs the emitter and looks at
// what it wrote.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every path a module GETs must be a path the demo's shim answers. The list is not written down
// here on purpose — it is mined from the Java, so a new screen that fetches something new fails
// this test on the commit that adds it, not on the day someone opens the demo.
func TestDemoShimAnswersWhatTheModulesAsk(t *testing.T) {
	gets := regexp.MustCompile(`Console\.fetchList\(\s*"(/[A-Za-z0-9_-]+)`)
	posts := regexp.MustCompile(`Console\.post\(\s*"(/[A-Za-z0-9_-]+)`)
	asked, sent := map[string]string{}, map[string]string{}
	root := filepath.Join("..", "ui")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".java") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range gets.FindAllStringSubmatch(string(src), -1) {
			asked[m[1]] = p
		}
		for _, m := range posts.FindAllStringSubmatch(string(src), -1) {
			sent[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(asked) < 8 {
		t.Fatalf("only %d GET paths found under %s — the miner's pattern has gone stale, "+
			"and a stale miner passes by finding nothing", len(asked), root)
	}
	for _, path := range keys(asked) {
		// 답하는 방식은 shim의 사정이다(정확 일치·접두사·표) — 재는 것은 "그 경로를 아는가"뿐:
		// 경로가 따옴표 안에 그대로 있으면 그 shim은 그 경로를 안다.
		if !strings.Contains(demoShim, `url.startsWith('`+path+`')`) {
			t.Errorf("%s fetches %s and the demo shim has no answer for it — on Pages that is a "+
				"404 and an empty panel (%s)", filepath.Base(asked[path]), path, asked[path])
		}
	}
	// Writes are answered as one: the demo accepts and forgets. That is a deliberate shape, so
	// pin it — without the catch-all every write path would 404 the same silent way.
	if len(sent) > 0 && !strings.Contains(demoShim, `init.method === 'POST'`) {
		t.Errorf("modules post to %v but the shim has no POST catch-all", keys(sent))
	}
}

// What the emitter leaves behind, read off disk. Pages serves the console from a subpath, so any
// path that stayed root-absolute is a 404 there and nowhere else — which is why this is checked
// here and not by opening the page.
func TestEmitDemoLeavesNothingRootAbsolute(t *testing.T) {
	dir := t.TempDir()
	ui, old, out := filepath.Join(dir, "ui"), filepath.Join(dir, "old"), filepath.Join(dir, "out")
	write(t, filepath.Join(ui, "console.html"),
		`<html><head><link rel="stylesheet" href="/ui/console.css">`+
			`<script src="/vendor/material.js"></script>`+
			`<script src="/ui/shell/shell.nocache.js"></script></head><body></body></html>`)
	write(t, filepath.Join(ui, "console.css"),
		"@font-face{src:url(/font/pretendard.woff2)}\n.row{color:red}\n")
	write(t, filepath.Join(ui, "shell", "shell.nocache.js"), `var p='/ui/'+m+'/';var s="/ui/companion.css";`)
	write(t, filepath.Join(old, "vendor", "material.js"), "export const md = 1;\n")
	write(t, filepath.Join(old, "i18n", "language.ko.json"), `{"nav.companions":"컴패니언"}`)

	if err := emitDemo(out, ui, old); err != nil {
		t.Fatalf("emitDemo: %v", err)
	}
	css := read(t, filepath.Join(out, "ui", "console.css"))
	if strings.Contains(css, "url(/font/") {
		t.Error("console.css still points at /font/ — two faces 404 on every demo page")
	}
	if !strings.Contains(css, "url(../../font/") {
		t.Error("the typeface was not repointed relative to the stylesheet's own place")
	}
	page := read(t, filepath.Join(out, "index.html"))
	for _, absolute := range []string{`"/ui/`, `'/ui/`, `"/vendor/`, `'/vendor/`} {
		if strings.Contains(page, absolute) {
			t.Errorf("index.html still carries %s — root-absolute under a subpath is a 404", absolute)
		}
	}
	// 두 콘솔이 같은 shim을 쓴다 — 그 shim이 fetch를 갈아끼운다는 사실이 표식이다.
	if !strings.Contains(page, "window.fetch =") {
		t.Error("the shim is missing — a demo with no mock answers nothing")
	}
	if strings.Index(page, "window.fetch =") > strings.Index(page, "<script src=") {
		t.Error("the shim goes in after the first script — the first module would ask the real network")
	}
	// The compiled module's own literals, both quote styles (GWT writes single).
	js := read(t, filepath.Join(out, "ui", "shell", "shell.nocache.js"))
	if strings.Contains(js, `'/ui/`) || strings.Contains(js, `"/ui/`) {
		t.Errorf("the loader still builds absolute module paths: %s", js)
	}
	// The single-source assets travelled: the packs and the bundle the page names.
	for _, want := range []string{"i18n/language.ko.json", "vendor/material.js"} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Errorf("%s did not travel into the demo: %v", want, err)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
