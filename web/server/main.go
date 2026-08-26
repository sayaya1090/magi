// web/server — 이행기 개발 서버. 새 콘솔(web/ui)의 컴파일 산출물을 서빙하고, 그 밖의 모든
// 요청은 기존 magi-web(정본)으로 리버스 프록시한다. 새 프론트가 기존 BFF의 실데이터로
// 렌더되므로, 개발 루프가 곧 신구 대조다. 컷오버(대조표 100%) 때 산출물은 go:embed로
// 옮겨 실리고 이 서버는 성장하거나 은퇴한다 — web/README.md 참조.
package main

import (
	"flag"
	"fmt"
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

	files := http.StripPrefix("/ui/", http.FileServer(http.Dir(*ui)))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ui/") {
			files.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/next" { // 새 셸 진입점 (기존 / 는 프록시로 옛 콘솔)
			http.ServeFile(w, r, *ui+"/console.html")
			return
		}
		proxy.ServeHTTP(w, r)
	})
	log.Printf("web/server: new UI at http://%s/next (assets /ui/*), everything else → %s", *addr, *bff)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
