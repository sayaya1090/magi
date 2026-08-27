// web/server — 이행기 개발 서버. 새 콘솔(web/ui)의 컴파일 산출물을 서빙하고, 그 밖의 모든
// 요청은 기존 magi-web(정본)으로 리버스 프록시한다. 새 프론트가 기존 BFF의 실데이터로
// 렌더되므로, 개발 루프가 곧 신구 대조다. 컷오버(대조표 100%) 때 산출물은 go:embed로
// 옮겨 실리고 이 서버는 성장하거나 은퇴한다 — web/README.md 참조.
package main

import (
	"flag"
	"fmt"
	"github.com/sayaya1090/magi/internal/webassets"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7778", "listen address (loopback only — the console rule)")
	ui := flag.String("ui", "web/ui/build/console", "directory holding the assembled UI (gradle assembleConsole)")
	bff := flag.String("bff", "http://127.0.0.1:7777", "the running magi-web to proxy everything else to")
	demo := flag.String("emit-demo", "", "write the console as a static demo into this directory, then exit")
	oldConsole := flag.String("old-console", "cmd/magi-web", "the old console's sources, for the single-source assets the demo carries")
	flag.Parse()

	if *demo != "" {
		if err := emitDemo(*demo, *ui, *oldConsole); err != nil {
			fmt.Fprintln(os.Stderr, "web/server: emit-demo:", err)
			os.Exit(1)
		}
		fmt.Println("web/server: demo written to", *demo)
		return
	}

	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		// 기존 콘솔의 listenLoopback 규칙을 그대로: 루프백이 아니면 거절.
		fmt.Fprintln(os.Stderr, "web/server: refusing non-loopback addr", *addr)
		os.Exit(2)
	}

	target, err := url.Parse(*bff)
	if err != nil {
		fmt.Fprintln(os.Stderr, "web/server: bad -bff:", err)
		os.Exit(2)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	base := proxy.Director
	proxy.Director = func(r *http.Request) {
		base(r)
		// 기존 콘솔은 same-site 가드가 Origin을 본다. 프록시 오리진(7778)이 그대로 가면
		// 교차 출처 POST로 읽혀 거절되므로, BFF 자신의 오리진으로 고쳐 보낸다.
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", target.Scheme+"://"+target.Host)
		}
		r.Header.Del("Referer")
		r.Host = target.Host
	}

	// GWT의 캐시 규약: <name>.nocache.js 는 절대 캐시하면 안 되고(재컴파일마다 내용이
	// 바뀌는 선택자다), <hash>.cache.js 는 영원히 캐시해도 된다(이름이 내용이다). 헤더 없이
	// 내보내면 브라우저 휴리스틱이 nocache까지 캐싱해, 재컴파일 뒤 낡은 선택자가 새 산출물
	// 짝을 못 찾는 간헐 공백이 된다 — 실측으로 되밟은 결함. html·css도 개발 서버에선 no-store.
	rawFiles := http.FileServer(http.Dir(*ui))
	files := http.StripPrefix("/ui/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, ".cache."):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "no-store")
		}
		rawFiles.ServeHTTP(w, r)
	}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ui/") {
			files.ServeHTTP(w, r)
			return
		}
		// 설치에 필요한 것들은 이 콘솔이 제 것으로 낸다 — 남의 서버를 부르지 않는다는 규칙이
		// 여기까지다. 개발 서버에서도 같은 바이트가 나가야 홈 화면에 담아 보고 확인할 수 있다.
		switch r.URL.Path {
		case "/manifest.webmanifest":
			w.Header().Set("Content-Type", "application/manifest+json")
			say(w, webassets.Manifest)
			return
		case "/icon.svg", "/icon-maskable.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			if r.URL.Path == "/icon.svg" {
				say(w, webassets.Icon)
			} else {
				say(w, webassets.IconMaskable)
			}
			return
		case "/sw.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			say(w, webassets.ServiceWorker)
			return
		}
		if r.URL.Path == "/next" { // 새 셸 진입점 (기존 / 는 프록시로 옛 콘솔)
			w.Header().Set("Cache-Control", "no-store")
			page, err := os.ReadFile(*ui + "/console.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			say(w, withSprite(string(page)))
			return
		}
		proxy.ServeHTTP(w, r)
	})
	log.Printf("web/server: new UI at http://%s/next (assets /ui/*), everything else → %s", *addr, *bff)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// withSprite puts the baked icons where the page's marker is, or takes the marker out.
//
// 이 콘솔이 제 그림을 낸다 — 예전에는 옛 콘솔의 페이지에서 빌려 왔고(Icons.borrow), 그것은
// 옛 콘솔이 사라지는 날 그림도 함께 사라진다는 뜻이었다. 라이선스가 없는 빌드에서는 표식만
// 사라지고 화면들은 늘 그리던 도형을 그린다.
func withSprite(page string) string {
	return strings.Replace(page, "<!--ICON-SPRITE-->", webassets.Sprite, 1)
}

// say writes a body and reports what went wrong instead of dropping it.
//
// 개발 서버라고 조용히 삼키지 않는다: 여기서 삼킨 오류는 "브라우저가 왜 빈 파일을 받았나"를
// 알아낼 유일한 단서다(저장소의 게이트가 그 규칙을 지킨다).
func say(w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		log.Printf("web/server: writing the response: %v", err)
	}
}
