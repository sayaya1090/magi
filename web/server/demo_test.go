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
	"strings"
	"testing"
)

// 목은 모듈마다 함께 실린다 — 포트가 있으면 그 모듈 안에 데모 구현이 있어야 한다.
//
// 페이지가 fetch를 갈아끼우던 시절의 검사(경로 채굴)를 대신한다: 화면이 제 iframe에서 제
// 회선으로 말하므로, 데모에서 무엇을 답할지는 그 모듈만 답할 수 있다. 새 화면이 포트를
// 하나 늘리고 목을 잊으면, 그 화면은 배포된 데모에서 빈 채로 뜬다 — 여기서 걸린다.
func TestEveryPortShipsItsOwnDemo(t *testing.T) {
	ports := map[string]string{} // 포트 이름 → 그 모듈
	demos := map[string]bool{}   // 데모 구현이 있는 포트 이름
	root := filepath.Join("..", "ui")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".java") {
			return err
		}
		rel := strings.TrimPrefix(p, root+string(filepath.Separator))
		mod := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		name := strings.TrimSuffix(d.Name(), ".java")
		switch {
		case strings.Contains(p, filepath.Join("src", "main", "java")) &&
			strings.Contains(p, string(filepath.Separator)+"usecase"+string(filepath.Separator)) &&
			(strings.HasSuffix(name, "Source") || strings.HasSuffix(name, "Repository") ||
				strings.HasSuffix(name, "Commander")):
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			// 포트만 — 그 안의 구현 클래스는 세지 않는다.
			if strings.Contains(string(src), "public interface "+name) {
				ports[mod+"/"+name] = mod
			}
		case strings.HasPrefix(name, "Demo") && strings.Contains(p, filepath.Join("src", "main", "java")):
			demos[mod+"/"+strings.TrimPrefix(name, "Demo")] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(ports) < 8 {
		t.Fatalf("only %d ports found under %s — the miner has gone stale, and a stale miner "+
			"passes by finding nothing", len(ports), root)
	}
	for port, mod := range ports {
		// 브리지로만 사는 포트는 목이 필요 없다: 답하는 쪽이 다른 모듈이라서(명단이 그렇다).
		if bridged[port] {
			continue
		}
		if !demos[port] {
			t.Errorf("%s has no Demo implementation in %s — on the published demo that screen "+
				"comes up empty, and nothing else can answer for it", port, mod)
		}
	}
}

// 셸이 답하는 것을 받아 쓰는 포트들 — 그 모듈에 목이 없는 것이 옳다(있으면 세상이 둘이 된다).
var bridged = map[string]bool{
	"companion-ui/FleetRepository": true,
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
	// 페이지가 하는 일은 한 줄이다: 지금은 데모라는 사실. 답은 모듈들이 제 목으로 한다.
	if !strings.Contains(page, "MAGI_DEMO") {
		t.Error("the page never says it is a demo — every module would then ask the real network")
	}
	if strings.Index(page, "MAGI_DEMO") > strings.Index(page, "<script src=") {
		t.Error("the flag goes in after the first script — a module could boot before reading it")
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
