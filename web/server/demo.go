// The new console as a static page, answered by a mock in the browser — the old console's
// -emit-demo, translated. GitHub Pages serves files, not processes, so /fleet and /events are
// answered by a shim this emitter appends: fetch is patched for the BFF paths and EventSource is
// replaced with a fixture stream. The compiled page is otherwise byte-for-byte what the build
// produced — except the root-relative asset prefixes, which become relative the same way (and for
// the same reason) demo.go rewrites /vendor/ in the old console: Pages serves a subpath.
package main

import (
	"fmt"
	"github.com/sayaya1090/magi/internal/webassets"
	"os"
	"path/filepath"
	"strings"
)

// 이 데모에는 페이지가 갈아끼우는 목이 없다.
//
// 화면은 저마다 컴파일된 모듈이고 GWT는 그것을 제 iframe에서 돌린다 — 페이지의 fetch를
// 갈아끼워도 모듈에는 닿지 않고(실측: 모든 화면이 빈 채로 떴다), 닿게 하려고 모듈을 부모
// 창에 묶으면 배포 구조를 거스른다: 화면은 저마다의 주기로 배포되고 제 창에서 제 회선으로
// 말한다. 그래서 목은 모듈마다 함께 실리고(Demo*Source), 이 페이지가 하는 일은 "지금은
// 데모다"라는 사실 하나를 적어 두는 것뿐이다. 그 한 줄을 읽는 것은 각 모듈의 그래프다.
const demoShim = `
<script>window.MAGI_DEMO = true;</script>
`

// emitDemo writes the assembled console as a self-answering static site into dir.
func emitDemo(dir, ui, oldConsole string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The compiled modules and stylesheets, laid out under ui/ the way the server serves them.
	if err := copyTree(ui, filepath.Join(dir, "ui")); err != nil {
		return fmt.Errorf("ui assets: %w", err)
	}
	// The single-source assets the BFF serves in life: the material bundle and the language packs.
	if err := copyTree(filepath.Join(oldConsole, "i18n"), filepath.Join(dir, "i18n")); err != nil {
		return fmt.Errorf("i18n: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(oldConsole, "vendor", "material.js"),
		filepath.Join(dir, "vendor", "material.js")); err != nil {
		return fmt.Errorf("vendor: %w", err)
	}
	// The stylesheet's own absolute paths. The typeface lives at the site root (the old console's
	// demo puts it there) and this shell lives a directory down, so `url(/font/…)` inside the
	// copied console.css answers 404 — measured, two faces missing on every demo page. Rewritten
	// relative to the stylesheet's own place, which is next/ui/.
	cssPath := filepath.Join(dir, "ui", "console.css")
	if css, err := os.ReadFile(cssPath); err == nil {
		fixed := strings.ReplaceAll(string(css), "url(/font/", "url(../../font/")
		if err := os.WriteFile(cssPath, []byte(fixed), 0o644); err != nil {
			return err
		}
	}
	// 설치에 필요한 것들도 함께 나간다 — 데모도 홈 화면에 담을 수 있어야 그것이 이 콘솔의
	// 사본이다. 이 사본은 하위 경로에 살므로 페이지가 대는 이름도 상대경로로 바뀐다(아래).
	for _, one := range []struct{ name, body string }{
		{"manifest.webmanifest", webassets.Manifest},
		{"icon.svg", webassets.Icon},
		{"icon-maskable.svg", webassets.IconMaskable},
		{"sw.js", webassets.ServiceWorker},
	} {
		if err := os.WriteFile(filepath.Join(dir, one.name), []byte(one.body), 0o644); err != nil {
			return fmt.Errorf("%s: %w", one.name, err)
		}
	}
	// The page: root-relative prefixes become relative, and the shim goes in ahead of the shell
	// so fetch and EventSource are already the mock's when the first module asks.
	pageBytes, err := os.ReadFile(filepath.Join(ui, "console.html"))
	if err != nil {
		return err
	}
	page := string(pageBytes)
	for _, prefix := range []string{"/ui/", "/vendor/", "/manifest.webmanifest", "/icon.svg",
		"/icon-maskable.svg", "/sw.js"} {
		page = strings.ReplaceAll(page, `"`+prefix, `"./`+strings.TrimPrefix(prefix, "/"))
		page = strings.ReplaceAll(page, "'"+prefix, "'./"+strings.TrimPrefix(prefix, "/"))
	}
	page = strings.Replace(page, "<script src=", demoShim+"<script src=", 1)
	page = withSprite(page)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	// The compiled shell builds module paths as '/ui/'+name+… (ScriptModuleLoader) — absolute,
	// because in life relative paths leak into the proxy. Here there is no proxy and there IS a
	// subpath, so the demo's copy says it relatively. Same move as the old console's /vendor/.
	// GWT's obfuscated output writes string literals with SINGLE quotes (measured), and the
	// path shows up two ways: as the loader's own '/ui/' piece and as whole literals like
	// '/ui/companion.css'. So the rewrite is by PREFIX — quote + /ui/ — in both quote styles,
	// the same shape the old console's demo uses for /vendor/.
	for _, q := range []string{`'`, `"`} {
		if err := rewriteTree(filepath.Join(dir, "ui"), q+`/ui/`, q+`./ui/`); err != nil {
			return err
		}
		// 말도 제 사본에서 읽는다. 이 사본은 하위 경로에 살아서 `/i18n/`은 사이트 뿌리를 —
		// 즉 옛 콘솔의 팩을 — 가리킨다: 제 팩을 함께 내보내 놓고 남의 것을 읽고 있었다.
		// 옛 콘솔이 사라지면 그 자리에는 아무것도 없다.
		//
		// `./`이지 `../`가 아니다: JS의 상대경로는 스크립트가 아니라 <b>문서</b>를 기준으로 푼다.
		// 문서는 next/index.html이므로 `../i18n/`은 다시 뿌리를 가리킨다(실측: 고쳐 놓고도
		// 같은 자리를 물었다). 스타일시트의 글꼴이 `../../`인 것은 그쪽 기준이 시트 자신이라서다.
		if err := rewriteTree(filepath.Join(dir, "ui"), q+`/i18n/`, q+`./i18n/`); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dest := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(p, dest)
	})
}

func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o644)
}

func rewriteTree(root, from, to string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if !strings.Contains(string(b), from) {
			return nil
		}
		return os.WriteFile(p, []byte(strings.ReplaceAll(string(b), from, to)), 0o644)
	})
}
