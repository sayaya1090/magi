package office

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

// 오피스 웹 작업창 애드인 정적 에셋 서빙 및 부트스트랩 주입 모듈(DESIGN.md §5.5, §12 #7).
//
// # 헬퍼 직접 호스팅 구조
// 헬퍼가 애드인 웹 에셋을 직접 HTTPS로 제공함으로써, 세션 인증 토큰을 HTML 생성 시점에 안전하게 주입(`window.MAGI = {...}`)합니다.
// 이를 통해 사용자 수동 토큰 복사/붙여넣기나 `<SourceLocation>` URL 인자 노출 등의 보안 및 편의성 문제를 배제합니다(§5.5).
//
// # 아키텍처적 전제 조건 3가지
// 1) 자체 서명 TLS 인증서 생성 및 신뢰 등록 필수(§5.5, certs.go).
// 2) 매니페스트 고정 포트 바인딩 준수(claim.go).
// 3) LNA(Local Network Access) 및 혼합 콘텐츠 보안 정책 준수(로컬 HTTPS 오리진 기반 통신).

// tokenMarker 는 애드인 HTML 내 부팅 스크립트가 주입될 위치를 나타내는 주석 태그입니다.
const tokenMarker = "<!--magi:boot-->"

// Pages 는 애드인 정적 리소스를 제공하는 HTTP 핸들러 구조체입니다.
type Pages struct {
	// Root 는 애드인 웹 소스 루트 디렉터리 경로입니다.
	Root string
	// Token 은 페이지에 주입될 세션 인증 토큰입니다. 빈 문자열인 경우 주입을 생략합니다(개발용 브라우저 직접 조회 지원).
	Token string
	// Boot 는 애드인 초기화 시 전달할 설정 메타데이터 맵입니다.
	Boot map[string]any
	// Base 는 라우팅 접두 URL 경로(예: `/word`, `/ppt`, `/xl`)입니다. 절대 경로 리소스 매핑에 사용됩니다.
	Base string
}

// Handler 는 정적 에셋 서빙 및 `taskpane.html` 동적 토큰 주입을 수행하는 HTTP 핸들러를 반환합니다.
func (p *Pages) Handler() http.Handler {
	files := http.FileServer(http.Dir(p.Root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackOnly(w, r) {
			return
		}
		// 로컬 개발 및 실시간 갱신 환경에서 구버전 JS/CSS 혼합 렌더링 결함을 방지하기 위해 캐시를 비활성화합니다.
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		// 동적 아이콘 생성 요청을 우선 처리하여 정적 디스크 파일과의 충돌을 방지합니다.
		if serveIcon(w, r) {
			return
		}
		clean := r.URL.Path
		if clean == "" || clean == "/" {
			// 애드인의 진입점은 하나다. `/` 로 들어온 사람을 404 로 보내지 않는다.
			http.Redirect(w, r, p.Base+"/taskpane.html", http.StatusFound)
			return
		}
		if strings.HasSuffix(clean, "taskpane.html") {
			p.serveTaskpane(w, r)
			return
		}
		// 빌드 식별자 접두사(`/v/<buildID>/`)를 제거하여 실제 디스크 경로 파일로 라우팅합니다.
		// 이전 빌드 ID를 포함한 요청이 유입되더라도 404를 반환하지 않고 현재 파일을 정상 제공합니다.
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

// buildID 는 애드인 소스 트리의 파일 크기 및 수정 시각 기반 해시값(12자)을 계산합니다.
//
// # 도입 배경 (WebView2 캐시 무효화)
// Office WebView2 런타임은 작업창 에셋을 강력하게 캐싱하며 `Cache-Control` 헤더를 무시하는 경우가 있습니다(TESTING §5.1.3).
// 단순 쿼리 파라미터(`?v=`) 방식은 진입점(`main.js`)만 변경되고 내부 상대 `import` 모듈들에는 적용되지 않으므로,
// URL 경로 자체에 버전 식별자(`/v/<id>/`)를 부여하여 상대 경로 임포트 모듈까지 일괄적으로 캐시가 무효화되도록 처리합니다.
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
func versionAssets(body []byte, base, id string) []byte {
	for _, pair := range [][2]string{
		{`href="taskpane.css"`, `href="` + base + `/v/` + id + `/taskpane.css"`},
		{`src="src/main.js"`, `src="` + base + `/v/` + id + `/src/main.js"`},
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
		_, _ = w.Write(versionAssets(body, p.Base, buildID(p.Root)))
		return
	}
	boot := map[string]any{}
	for k, v := range p.Boot {
		boot[k] = v
	}
	boot["token"] = p.Token
	// 안전한 직렬화를 위해 JSON 인코딩을 거쳐 script 태그에 주입합니다.
	var buf bytes.Buffer
	if err := bootTemplate.Execute(&buf, template.JS(mustJSON(boot))); err != nil {
		http.Error(w, "부팅 값을 못 심었습니다: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := bytes.Replace(body, []byte(tokenMarker), buf.Bytes(), 1)
	out = versionAssets(out, p.Base, buildID(p.Root))
	if !bytes.Contains(body, []byte(tokenMarker)) {
		// 마커 누락으로 인해 토큰이 주입되지 않은 경우 경고 헤더를 설정하여 진단 가능하도록 합니다.
		w.Header().Set("X-Magi-Warning", "taskpane.html has no "+tokenMarker+" marker; the page was served without a token")
	}
	_, _ = w.Write(out)
}

var bootTemplate = template.Must(template.New("boot").Parse(
	`<script>window.MAGI = {{.}};</script>`))

// mustJSON 은 내부 데이터 직렬화용 헬퍼입니다. 직렬화 오류 발생 시 빈 객체("{}")를 반환합니다.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
