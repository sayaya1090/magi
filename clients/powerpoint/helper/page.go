package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// 애드인 페이지를 **헬퍼가 직접 내준다**(DESIGN.md §5.5, §12 #7).
//
// 그러면 토큰은 애드인에게 **전달할 것이 아니라 페이지에 박혀 나오는 것**이고, 주소는 그 페이지의
// 자기 오리진이라 「어떻게 도달하는가」가 통째로 사라진다. 기각된 둘 — 사람이 코드를 한 번 붙여
// 넣기, `<SourceLocation>` 에 실어 보내기 — 과 그 사유는 §5.5 에 있다.
//
// 값이 셋 붙는다: 인증서가 필수가 되고(§5.5 값 1 · certs.go), 포트가 고정이어야 하고(값 2 ·
// claim.go), LNA·혼합 콘텐츠의 모양이 달라진다(값 3 — 요청하는 쪽이 공개 오리진이 아니게 된다).

// tokenMarker 는 페이지에 토큰을 심는 자리. 애드인 쪽 HTML 이 이 주석을 들고 있고, 여기서
// 스크립트 한 줄로 바뀐다. **애드인이 주소를 안 적는 것과 같은 이유로 토큰도 안 적는다** —
// 소스에 있는 값은 넷을 다섯으로 만든다.
const tokenMarker = "<!--magi:boot-->"

// Pages 는 애드인 트리를 내주는 쪽.
type Pages struct {
	// Root 는 애드인 소스 디렉토리.
	Root string
	// Token 은 페이지에 박아 보낼 값. 비면 안 박는다(브라우저에서 맨몸으로 열어 보는 길).
	Token string
	// Boot 는 페이지가 부팅할 때 알아야 하는 것들. 토큰과 같이 실린다.
	Boot map[string]any
}

// Handler 는 정적 파일을 내주되 `taskpane.html` 만 손본다.
func (p *Pages) Handler() http.Handler {
	files := http.FileServer(http.Dir(p.Root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackOnly(w, r) {
			return
		}
		// **아무것도 캐시하지 않는다.** 페이지만 no-store 로 두고 스크립트와 스타일을 캐시에
		// 맡겼더니, 실물에서 **낡은 JS 와 새 CSS 가 한 화면에 섞여** 떴다 — 새 요약 칸은 비고
		// 카드에는 패딩이 없는, 어느 쪽 코드도 만든 적 없는 화면이었다. 웹뷰의 캐시는 우리가
		// 못 보는 자리라, 거기서 섞인 것을 화면만 보고 되짚는 데 값이 크게 든다.
		//
		// 캐시로 아낄 것이 없기도 하다 — 루프백이고, 파일은 한 줌이며, 창은 하루에 몇 번 뜬다.
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		// **마크는 그려서 낸다.** 파일서버보다 먼저 서는 것이 요점이다 — 뒤에 두면 디스크에
		// 남아 있는 옛 파일이 이긴다. 그 옛 파일이 단색 네모였고, 리본에 그게 떴다.
		if serveIcon(w, r) {
			return
		}
		clean := r.URL.Path
		if clean == "" || clean == "/" {
			// 애드인의 진입점은 하나다. `/` 로 들어온 사람을 404 로 보내지 않는다.
			http.Redirect(w, r, "/taskpane.html", http.StatusFound)
			return
		}
		if strings.HasSuffix(clean, "taskpane.html") {
			p.serveTaskpane(w, r)
			return
		}
		// **판본이 낀 주소를 벗겨서 같은 파일을 낸다**(`buildID`). Office 의 작업창 캐시는
		// 주소로 돌고 헤더로 못 끄므로, 파일이 바뀌면 주소가 바뀌게 해서 새로 받게 만든다.
		// 벗기기만 하고 판본은 안 따진다 — 낡은 판본으로 들어온 요청도 **지금 파일**을 받는
		// 것이 맞다. 우리는 옛 판본을 갖고 있지 않고, 없는 것을 404 로 답하면 열려 있던 창이
		// 그 자리에서 죽는다.
		if rest, ok := strings.CutPrefix(clean, "/v/"); ok {
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				r2 := *r
				u := *r.URL
				u.Path = rest[i:]
				r2.URL = &u
				files.ServeHTTP(w, &r2)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

// buildID 는 지금 디스크에 있는 애드인의 판본. 파일이 하나라도 바뀌면 달라진다.
//
// **왜 필요한가.** Office 는 작업창 자산을 **자기 캐시**에 물고, 그 캐시는 `Cache-Control` 로
// 못 끈다 — 이 저장소가 재 봤다(TESTING §5.1.3): `no-store` 를 줘도, 창을 껐다 열어도,
// PowerPoint 를 재시작해도, WebView2 캐시를 지워도 옛 화면이었다. 그래서 문서는 「화면은
// 목업에서 잰다」로 물러나 있었는데, 그건 대안이지 해결이 아니다 — **사람이 쓰는 화면은 고쳐도
// 안 바뀐다**는 뜻이기 때문이다.
//
// 캐시가 **주소로** 도는 이상 답은 주소를 바꾸는 것이다. 그런데 `?v=` 를 붙이는 흔한 수는 여기서
// 안 듣는다: 페이지가 부르는 것은 `src/main.js` 하나뿐이고 나머지는 그 안의 `import` 로 오므로,
// 진입점 주소만 바뀌면 **모듈 스무 개는 옛 주소 그대로** 온다. 그래서 판본을 **경로**에 넣는다 —
// `/v/<id>/src/main.js` 가 `./ui/view.js` 를 부르면 `/v/<id>/src/ui/view.js` 로 저절로 간다.
func buildID(root string) string {
	h := sha256.New()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// versionAssets 는 페이지가 부르는 **우리 자산**의 주소에 판본을 끼운다. 남의 주소(office.js
// 같은 절대 URL)는 안 건드린다 — 우리가 캐시를 논할 자리가 아니다.
func versionAssets(body []byte, id string) []byte {
	for _, pair := range [][2]string{
		{`href="taskpane.css"`, `href="/v/` + id + `/taskpane.css"`},
		{`src="src/main.js"`, `src="/v/` + id + `/src/main.js"`},
	} {
		body = bytes.Replace(body, []byte(pair[0]), []byte(pair[1]), 1)
	}
	return body
}

func (p *Pages) serveTaskpane(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(filepath.Join(p.Root, "taskpane.html"))
	if err != nil {
		http.Error(w, "이 헬퍼가 애드인 페이지를 못 찾았습니다: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 페이지는 **캐시하지 않는다.** 토큰이 기동마다 새로 나므로 캐시된 페이지는 지난 기동의
	// 토큰을 들고 오고, 증상은 「어제 열어 둔 창이 오늘 401 을 받는다」다.
	w.Header().Set("Cache-Control", "no-store")
	if p.Token == "" && p.Boot == nil {
		// 헤더는 이미 나갔다 — 최선 노력이다(icon.go 의 같은 자리와 같은 이유).
		_, _ = w.Write(versionAssets(body, buildID(p.Root)))
		return
	}
	boot := map[string]any{}
	for k, v := range p.Boot {
		boot[k] = v
	}
	boot["token"] = p.Token
	// `template.JSStr` 이 아니라 JSON 으로 심는다 — 값이 문자열이 아닌 것도 있고, 이스케이프를
	// 손으로 하는 자리를 만들지 않는다.
	var buf bytes.Buffer
	if err := bootTemplate.Execute(&buf, template.JS(mustJSON(boot))); err != nil {
		http.Error(w, "부팅 값을 못 심었습니다: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := bytes.Replace(body, []byte(tokenMarker), buf.Bytes(), 1)
	out = versionAssets(out, buildID(p.Root))
	if !bytes.Contains(body, []byte(tokenMarker)) {
		// **조용히 넘어가지 않는다.** 마커가 없으면 페이지는 토큰 없이 뜨고, 증상은 화면이
		// 아무 말 없이 비는 것이다 — 그 실패를 여기서 한 번 말한다.
		w.Header().Set("X-Magi-Warning", "taskpane.html has no "+tokenMarker+" marker; the page was served without a token")
	}
	_, _ = w.Write(out)
}

var bootTemplate = template.Must(template.New("boot").Parse(
	`<script>window.MAGI = {{.}};</script>`))

// mustJSON 은 우리가 만든 맵을 싣는 자리에서만 쓴다. 실패하면 그건 코드 결함이지 입력 문제가
// 아니라서, 빈 객체로 떨어뜨리고 페이지는 계속 뜬다 — 토큰 없는 페이지는 401 을 받고 **그 사유가
// 화면에 뜬다**(§5.3 「끝내 못 붙으면 말한다」).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
