// The demo is the only build output a stranger sees (Pages), and nothing in the browser tells it
// when it is wrong: a path the shim does not know answers 404 and the screen just stays empty.
// Three such holes shipped before these tests existed — /console, /context, and the typeface. So
// both tests here read the real thing rather than a description of it: the first asks the modules
// themselves what they fetch, the second runs the emitter and looks at what it wrote.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 화면이 묻는 길은 목이 답해야 한다 — 하나라도 빠지면 그 판은 배포된 데모에서 빈 채로 뜬다.
//
// 목이 화면마다 있던 시절의 검사(모듈마다 Demo* 구현을 요구)를 대신한다. 이제 목은 회선의
// 이음매에 걸린 모듈 하나이고, 답하는 단위도 화면이 아니라 <b>경로</b>다. 그래서 여기서
// 재는 것도 경로다: 화면들이 부르는 길을 소스에서 캐고, demo-ui가 그 길에 답하는지 본다.
func TestTheMockAnswersEveryPathTheScreensAsk(t *testing.T) {
	asked := map[string]string{} // 경로 → 그 길을 부르는 모듈
	answers := map[string]bool{} // 목이 답하는 경로
	root := filepath.Join("..", "..", "web", "ui")
	// 부르는 모양을 하나라도 빠뜨리면 이 검사는 통과하면서 아무것도 안 본다. postText와
	// postSaid가 빠져 있던 동안 /suggest·/complete·/git-msg·/git-pr·/pr-msg 다섯이 이 눈
	// 밖에 있었고, 그 중 /suggest는 공개 데모에서 501이었다(실측). 그러니 이름을 열거하지
	// 말고 Console의 <b>모든</b> 부름을 본다 — 다음에 늘어나는 이름도 저절로 들어온다.
	call := regexp.MustCompile(`Console\.[a-zA-Z]+\("(/[a-zA-Z0-9_-]+)`)
	// 목이 답하는 모양 둘: switch의 case와, 길 하나만 보는 자리의 equals(스트림이 그렇다).
	answered := regexp.MustCompile(`(?:case "(/[a-zA-Z0-9_-]+)"|"(/[a-zA-Z0-9_-]+)"\.equals\()`)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".java") ||
			!strings.Contains(p, filepath.Join("src", "main", "java")) {
			return err
		}
		rel := strings.TrimPrefix(p, root+string(filepath.Separator))
		mod := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if mod == "demo-ui" {
			for _, m := range answered.FindAllStringSubmatch(string(src), -1) {
				answers[m[1]+m[2]] = true
			}
			return nil
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			asked[m[1]] = mod
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(asked) < 20 {
		t.Fatalf("only %d paths found under %s — the miner has gone stale, and a stale miner "+
			"passes by finding nothing", len(asked), root)
	}
	for path, mod := range asked {
		// 파일로 서빙되는 것들은 목의 것이 아니다 — 데모 사이트가 제 사본을 함께 낸다.
		if served[path] {
			continue
		}
		if !answers[path] {
			t.Errorf("%s asks for %s and the mock does not answer it — that screen comes up "+
				"empty on the published demo", mod, path)
		}
	}
}

// 목이 아니라 파일이 답하는 길 — 데모 사이트가 그 사본을 함께 내보낸다.
var served = map[string]bool{
	"/i18n": true,
}

// What the emitter leaves behind, read off disk. The demo is opened from a directory rather than
// served by the binary that embeds it, so anything that stayed root-absolute is a 404 there and
// nowhere else — which is why this is checked here and not by opening the page.
func TestEmitDemoLeavesNothingRootAbsolute(t *testing.T) {
	dir := t.TempDir()
	ui, mock, out := filepath.Join(dir, "ui"), filepath.Join(dir, "demo-mock", "demo"), filepath.Join(dir, "out")
	demoWrite(t, filepath.Join(ui, "console.html"),
		`<html><head><link rel="stylesheet" href="/ui/console.css">`+
			`<!--DEMO-SHIM-->`+
			`<script type="module">import '/vendor/material.js';import * as rxjs from '/vendor/rxjs.js';`+
			`window.rxjs=rxjs;const b=document.createElement('script');b.src='/ui/shell/shell.nocache.js';`+
			`document.head.append(b);</script></head><body></body></html>`)
	demoWrite(t, filepath.Join(ui, "console.css"),
		"@font-face{src:url(/font/newsreader-400.woff2)}\n.row{color:red}\n")
	demoWrite(t, filepath.Join(ui, "shell", "shell.nocache.js"),
		`var p='/ui/'+m+'/';var s="/ui/companion.css";var t='/i18n/language.'+w+'.json';`)
	// 목은 콘솔 자산 옆의 제 디렉토리에서 온다(운영 번들에는 없다) — 데모를 낼 때만 실린다.
	demoWrite(t, filepath.Join(mock, "demo.nocache.js"), "// the mock\n")

	if err := emitDemo(out, os.DirFS(ui), mock); err != nil {
		t.Fatalf("emitDemo: %v", err)
	}
	css := demoRead(t, filepath.Join(out, "ui", "console.css"))
	if strings.Contains(css, "url(/font/") {
		t.Error("console.css still points at /font/ — two faces 404 on every demo page")
	}
	if !strings.Contains(css, "url(../font/") {
		t.Error("the typeface was not repointed relative to the stylesheet's own place")
	}
	page := demoRead(t, filepath.Join(out, "index.html"))
	for _, absolute := range []string{`"/ui/`, `'/ui/`, `"/vendor/`, `'/vendor/`} {
		if strings.Contains(page, absolute) {
			t.Errorf("index.html still carries %s — root-absolute under a subpath is a 404", absolute)
		}
	}
	// 페이지가 하는 일은 한 줄이다: 지금은 데모라는 사실. 답은 모듈들이 제 목으로 한다.
	if !strings.Contains(page, "MAGI_DEMO") {
		t.Error("the page never says it is a demo — every module would then ask the real network")
	}
	// 그리고 그 사실은 <b>첫 스크립트보다 먼저</b> 적혀야 한다. 부트스트랩이 태그가 아니라
	// 모듈 스크립트 안으로 들어가면서 "<script src=" 이라는 기준점이 사라졌고, 그때 목이
	// 조용히 빠져 데모가 통째로 404가 됐다(밟았다) — 그래서 기준점은 표식이 아니라 실제로
	// 회선을 여는 첫 스크립트다.
	if strings.Index(page, "MAGI_DEMO") > strings.Index(page, "shell.nocache.js") {
		t.Error("the flag goes in after the shell boots — modules would ask the real network first")
	}
	// The compiled module's own literals, both quote styles (GWT writes single).
	js := demoRead(t, filepath.Join(out, "ui", "shell", "shell.nocache.js"))
	if strings.Contains(js, `'/ui/`) || strings.Contains(js, `"/ui/`) {
		t.Errorf("the loader still builds absolute module paths: %s", js)
	}
	// 말은 제 사본에서 읽는다. 그리고 `./`이지 `../`가 아니다: JS의 상대경로는 스크립트가
	// 아니라 문서를 기준으로 푼다(실측).
	if strings.Contains(js, `'/i18n/`) || strings.Contains(js, `"/i18n/`) {
		t.Error("the console still asks the site root for its language pack")
	}
	if strings.Contains(js, `'../i18n/`) {
		t.Error("`../i18n/` resolves against the document, which is index.html at the root")
	}
	if !strings.Contains(js, `'./i18n/`) {
		t.Error("the language pack path was not repointed at this console's own copy")
	}

	// 살아서 바이너리가 나눠 주던 것들이 파일로 따라 나왔는가. 손으로 적은 목록이 아니라
	// 임베드한 디렉토리에서 통째로 복사하므로, 여기서는 각 갈래가 비지 않았는지를 본다 —
	// 번들 하나가 늘어도 이 검사는 저절로 따라온다.
	for _, one := range []struct{ dir, suffix string }{
		{"vendor", ".js"}, {"i18n", ".json"}, {"font", ".woff2"},
	} {
		names, err := os.ReadDir(filepath.Join(out, one.dir))
		if err != nil {
			t.Errorf("%s/ did not travel into the demo: %v", one.dir, err)
			continue
		}
		got := 0
		for _, n := range names {
			if strings.HasSuffix(n.Name(), one.suffix) {
				got++
			}
		}
		if got == 0 {
			t.Errorf("%s/ travelled empty — nothing in it ends in %s", one.dir, one.suffix)
		}
	}
	// 밑줄로 시작하는 파일을 감추는 호스트가 사이트의 일부를 먹지 않게.
	for _, want := range []string{".nojekyll", "manifest.webmanifest", "icon.svg", "sw.js"} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Errorf("%s is missing: %v", want, err)
		}
	}
}

// 콘솔이 없는 빌드에서 데모를 내라고 하면, 빈 디렉토리를 만들어 두고 성공했다고 하면 안 된다.
// 그 자리에서 이유를 대야 CI가 조립 단계를 빠뜨린 것을 그때 안다.
func TestEmitDemoWithoutAConsoleSaysSo(t *testing.T) {
	dir := t.TempDir()
	err := emitDemo(filepath.Join(dir, "out"), os.DirFS(filepath.Join(dir, "empty")), dir)
	if err == nil {
		t.Fatal("a build with no console emitted a demo anyway")
	}
	if !strings.Contains(err.Error(), "console") {
		t.Errorf("the error does not name what is missing: %v", err)
	}
}

func demoWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func demoRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
