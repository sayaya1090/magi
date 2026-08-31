package main

import (
	"bytes"
	"encoding/json"
	"html/template"
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
		files.ServeHTTP(w, r)
	})
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
		_, _ = w.Write(body)
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
