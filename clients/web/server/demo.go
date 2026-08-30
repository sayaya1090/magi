// The console as a static page, answered by a mock in the browser.
//
// # Why this exists
//
// Nobody can look at these screens without running a daemon, a console and at least one companion.
// That is a lot of setup to answer "what does it look like", and it means a change to the front end
// gets reviewed as a diff. A published demo turns the review into looking at it.
//
// # Why the mock is not in this page
//
// Each screen is a compiled GWT module and GWT runs it in its own iframe — patching the page's fetch
// does not reach into one (measured: every screen came up empty), and binding the modules to the
// parent window to make it reach would go against the deployment shape, where a screen ships on its
// own clock and talks on its own line. So the mock rides WITH the modules (clients/web/ui/demo-ui, hung on
// Console.raw/stream), and all this page does is write down the single fact that this is a demo. The
// thing that reads that fact is each module's own object graph.
//
// # What it proves, and what it does not
//
// It proves the markup, the styling and the screens' own logic — the states, the filters, the tabs,
// the forms, how it behaves on a phone. It does not prove a single handler: every answer is a
// fixture. The Go tests are what check the handlers, and the workflow runs those first, so a demo
// that deploys is a demo whose server side passed its own tests.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/webassets"
)

// 이 데모에는 페이지가 갈아끼우는 목이 없다 — 위 주석의 그 이유. 여기서 넣는 것은 "지금은
// 데모다"라는 사실 하나와, 그것을 사람에게 알리는 띠뿐이다.
const demoShim = `
<script>window.MAGI_DEMO = true;</script>
<style>
  /* 창을 가로지르는 띠, 흐름 밖에. 흐름 안에 두면 페이지의 내용이 시작되는 자리(레일의 거터
     다음)에서 시작해, 드로어가 넓어질 때 그 밑으로 미끄러져 들어간다. 페이지 전체에 대한
     알림은 페이지 전체를 가로지른다. */
  .demo-banner {
    position:fixed; inset:0 0 auto 0; z-index:60; padding:.55rem 1.2rem;
    background:var(--magi-ref-primaryContainer); color:var(--magi-ref-fg);
    font:600 var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono);
    letter-spacing:.06em; border-bottom:1px solid var(--magi-ref-outlineVariant);
  }
  /* 창 위에 고정되는 것은 전부 이 띠 아래에서 시작해야 한다 — body의 padding은 흐름만 움직이고
     fixed·sticky는 움직이지 않는다. :has()로 띠 없는 페이지는 이 규칙에 닿지 않는다. */
  body:has(.demo-banner) header { top:var(--demo-banner, 0px); }
  /* 레일이 창 <b>위</b>에 붙는 폭에서만. 600px 아래에서 레일은 발치의 바이고, 거기에 top을
     주면 바가 화면을 덮는 시트가 된다(구 데모가 밟은 그 결함). */
  @media (min-width:37.5em) {
    body:has(.demo-banner) #rail {
      top:var(--demo-banner, 0px);
      padding-top:calc(var(--demo-banner, 0px) + var(--magi-sys-space-150));
    }
  }
</style>
<script>
  // 데모의 띠 — "이건 진짜 페이지이고, 답하는 쪽이 목이다". 페이지가 제 안에 이 알림에 대한
  // 규칙을 갖지 않도록, 자리를 밀어 주는 일도 여기서 한다.
  (function () {
    var banner = document.createElement('div');
    banner.className = 'demo-banner';
    banner.textContent = 'demo — the real page, answered by a mock. Nothing here is a running agent, ' +
      'and every action reports what it would have sent.';
    // 그 마지막 절은 이 띠가 혼자 지키는 약속이 아니다: 목(demo-ui의 Banner)이 아래 클래스
    // 이름으로 이 자리를 찾아 글자를 갈아 끼운다. 클래스 이름 하나가 계약의 전부이고 양쪽
    // 주석에 적혀 있다 — 띠가 없는 페이지(테스트 하네스)에서 목은 조용히 지나간다.
    var was = 0;
    function push() {
      var h = Math.ceil(banner.getBoundingClientRect().height);
      if (!h || h === was) return;   // 같은 값에서 멈춘다: 아래 resize가 이 함수를 다시 부른다
      was = h;
      document.documentElement.style.setProperty('--demo-banner', h + 'px');
      document.body.style.paddingTop = h + 'px';
      // 페이지는 제 껍데기의 높이를 "본문이 시작되는 자리"에서 잰다 — 방금 그 자리가 움직였다.
      window.dispatchEvent(new Event('resize'));
    }
    function place() {
      document.body.prepend(banner);
      if (typeof ResizeObserver === 'function') new ResizeObserver(push).observe(banner);
      requestAnimationFrame(push);
    }
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', place);
    else place();
  })();
</script>
`

// shimMark is where the shim stands: ahead of everything that opens a line.
//
// A marker rather than "before the first <script src=", which is what this used to look for — the
// bootstrap moved into JavaScript, that tag stopped existing, and the mock silently dropped out
// (every screen 404ed, and it took a while to notice). Missing, emitting FAILS rather than quietly
// producing an empty console.
const shimMark = "<!--DEMO-SHIM-->"

// emitDemo writes the assembled console as a self-answering static site into dir.
//
// tree is the console this binary would serve; mock is clients/web/ui/build/demo-mock/demo, which
// assembleConsole deliberately leaves out of the production bundle and only this emitter copies in.
//
// The page is not rewritten beyond its own asset prefixes: GitHub Pages serves a project site under
// /<repo>/, where a leading slash escapes to the domain root and 404s. The moment this function
// starts editing the page for any other reason, the demo stops being evidence about the page.
func emitDemo(dir string, tree fs.FS, mock string) error {
	pageBytes, err := fs.ReadFile(tree, "console.html")
	if err != nil {
		return fmt.Errorf("no console to make a demo of: %w — see clients/web/server/console/README.md", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The compiled modules and stylesheets, laid out under ui/ the way the server serves them.
	if err := copyFS(tree, filepath.Join(dir, "ui")); err != nil {
		return fmt.Errorf("ui assets: %w", err)
	}
	// The demo's mock, beside the screens it answers for. The page loads it first and waits.
	if !dirExists(mock) {
		return fmt.Errorf("demo mock not built: %s — run assembleDemoMock", mock)
	}
	if err := copyFS(os.DirFS(mock), filepath.Join(dir, "ui", "demo")); err != nil {
		return fmt.Errorf("demo mock: %w", err)
	}
	// Everything the page fetches in life, written where it fetches it from: the vendored bundles,
	// every language pack, every face of the typeface. Read out of the directories rather than from
	// a list kept in step — a hand-written list is one more thing to forget when a second bundle
	// arrives, which is exactly how the Material one went missing once.
	if err := copyFS(assetFS, dir); err != nil {
		return fmt.Errorf("vendored assets: %w", err)
	}
	fonts, ferr := fs.Sub(fontFS, "fonts")
	if ferr != nil {
		return ferr
	}
	if err := copyFS(fonts, filepath.Join(dir, "font")); err != nil {
		return fmt.Errorf("fonts: %w", err)
	}
	// 설치에 필요한 것들도 함께 나간다 — 데모도 홈 화면에 담을 수 있어야 그것이 이 콘솔의
	// 사본이다. .nojekyll은 밑줄로 시작하는 파일을 감추는 호스트가 사이트의 일부를 먹지 않게.
	for _, one := range []struct{ name, body string }{
		{".nojekyll", ""},
		{"manifest.webmanifest", webassets.Manifest},
		{"icon.svg", webassets.Icon},
		{"icon-maskable.svg", webassets.IconMaskable},
		{"sw.js", webassets.ServiceWorker},
	} {
		if err := os.WriteFile(filepath.Join(dir, one.name), []byte(one.body), 0o644); err != nil {
			return fmt.Errorf("%s: %w", one.name, err)
		}
	}
	// The stylesheet's own absolute paths. A CSS url() resolves against the STYLESHEET, and this
	// one lives a directory down at ui/console.css, so `url(/font/…)` answers 404 — measured, two faces
	// missing on every demo page.
	cssPath := filepath.Join(dir, "ui", "console.css")
	if css, rerr := os.ReadFile(cssPath); rerr == nil {
		fixed := strings.ReplaceAll(string(css), "url(/font/", "url(../font/")
		if werr := os.WriteFile(cssPath, []byte(fixed), 0o644); werr != nil {
			return werr
		}
	}
	// The page: root-relative prefixes become relative, and the shim goes in ahead of the shell so
	// the "this is a demo" flag is already set when the first module asks.
	page := withSprite(string(pageBytes))
	for _, prefix := range []string{"/ui/", "/vendor/", "/i18n/", "/manifest.webmanifest", "/icon.svg",
		"/icon-maskable.svg", "/sw.js"} {
		page = strings.ReplaceAll(page, `"`+prefix, `"./`+strings.TrimPrefix(prefix, "/"))
		page = strings.ReplaceAll(page, "'"+prefix, "'./"+strings.TrimPrefix(prefix, "/"))
	}
	if !strings.Contains(page, shimMark) {
		return fmt.Errorf("console.html: %s marker not found — the demo mock has nowhere to stand", shimMark)
	}
	page = strings.Replace(page, shimMark, demoShim, 1)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	// The compiled shell builds module paths as '/ui/'+name+… (ScriptModuleLoader) — absolute,
	// because in life a relative path would resolve against whatever route the reader is standing
	// on. Here there is no router and there IS a subpath, so the demo's copy says it relatively.
	// GWT's obfuscated output writes string literals with SINGLE quotes (measured), and the path
	// shows up two ways: as the loader's own '/ui/' piece and as whole literals like
	// '/ui/companion.css'. So the rewrite is by PREFIX — quote + the path — in both quote styles.
	//
	// `./`이지 `../`가 아니다: JS의 상대경로는 스크립트가 아니라 문서를 기준으로 푼다. 문서는
	// index.html이고 사이트 뿌리에 있다(실측: `../`로 고쳐 놓고 한 칸 위를 물었다).
	for _, q := range []string{`'`, `"`} {
		for _, prefix := range []string{`/ui/`, `/i18n/`, `/vendor/`} {
			if err := rewriteTree(filepath.Join(dir, "ui"), q+prefix, q+`.`+prefix); err != nil {
				return err
			}
		}
	}
	return nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// copyFS writes an fs.FS out to a directory. os.CopyFS would do this, and does not: it refuses to
// write into a directory that already has the file, which is precisely what the second and third
// calls above do.
func copyFS(from fs.FS, to string) error {
	return fs.WalkDir(from, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(to, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		b, rerr := fs.ReadFile(from, p)
		if rerr != nil {
			return rerr
		}
		if merr := os.MkdirAll(filepath.Dir(dest), 0o755); merr != nil {
			return merr
		}
		return os.WriteFile(dest, b, 0o644)
	})
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
