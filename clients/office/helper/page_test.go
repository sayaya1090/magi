package office

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pagesAt(t *testing.T) (*Pages, string) {
	t.Helper()
	dir := t.TempDir()
	body := "<!doctype html><head>" + tokenMarker + "</head><body>hi</body>"
	if err := os.WriteFile(filepath.Join(dir, "taskpane.html"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taskpane.css"), []byte(".x{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Pages{Root: dir, Token: "tok", Boot: map[string]any{"origin": "https://127.0.0.1:3001"}}, dir
}

// 토큰은 **페이지에 박혀 나간다**(DESIGN.md §5.5·§12 #7) — 애드인에 전달할 것이 아니라.
func TestThePageCarriesItsOwnToken(t *testing.T) {
	p, _ := pagesAt(t)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/taskpane.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, `"token":"tok"`) {
		t.Errorf("페이지가 토큰을 안 실었다:\n%s", got)
	}
	if !strings.Contains(got, `"origin":"https://127.0.0.1:3001"`) {
		t.Errorf("부팅 값이 안 실렸다:\n%s", got)
	}
	if strings.Contains(got, tokenMarker) {
		t.Error("마커가 그대로 남았다 — 심는 자리를 못 찾았다")
	}
}

// 마커가 없으면 **조용히 넘어가지 않는다.** 토큰 없는 페이지는 401 을 받고, 그 사유가 화면
// 어디에도 없으면 사람이 보는 것은 빈 창뿐이다.
func TestAPageWithNoMarkerSaysSo(t *testing.T) {
	p, dir := pagesAt(t)
	if err := os.WriteFile(filepath.Join(dir, "taskpane.html"), []byte("<html>no marker</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/taskpane.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Magi-Warning") == "" {
		t.Error("마커가 없는데 아무 말도 안 했다")
	}
}

// **아무것도 캐시하지 않는다.**
//
// 페이지만 no-store 로 두고 스크립트·스타일을 캐시에 맡겼더니 실물에서 낡은 JS 와 새 CSS 가
// 한 화면에 섞여 떴다 — 어느 쪽 코드도 만든 적 없는 화면이라 되짚는 데 값이 크게 들었다.
// 웹뷰의 캐시는 우리가 못 보는 자리다.
func TestNothingTheAddinLoadsIsCached(t *testing.T) {
	p, _ := pagesAt(t)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	checked := 0
	for _, path := range []string{"/taskpane.html", "/taskpane.css"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		checked++
		if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("%s 의 Cache-Control 이 %q 다", path, cc)
		}
	}
	if checked != 2 {
		t.Fatalf("본 것이 %d 개다 — 볼 것이 없었다", checked)
	}
}

// `/` 로 들어온 사람을 404 로 보내지 않는다. 애드인의 진입점은 하나다.
func TestTheRootGoesToTheTaskpane(t *testing.T) {
	p, _ := pagesAt(t)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "taskpane.html") {
		t.Fatalf("루트가 %d / %q 로 답했다", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestAssetsCarryABuildIDInTheirPath 는 **고친 화면이 사람에게 닿는가**를 잰다.
//
// Office 는 작업창 자산을 자기 캐시에 물고 그 캐시는 `Cache-Control` 로 못 끈다 — 이 저장소가
// 재 봤다(TESTING §5.1.3): no-store 를 줘도, 창을 껐다 열어도, PowerPoint 를 재시작해도,
// WebView2 캐시를 지워도 옛 화면이었다. 그래서 문서는 「화면은 목업에서 잰다」로 물러나 있었는데
// 그건 대안이지 해결이 아니다 — **사람이 쓰는 화면은 고쳐도 안 바뀐다**는 뜻이기 때문이다.
//
// `?v=` 로는 안 된다: 페이지가 부르는 것은 진입점 하나뿐이고 나머지는 그 안의 `import` 로 오므로
// 모듈 스무 개가 옛 주소 그대로 온다. 그래서 판본을 **경로**에 넣는다.
func TestAssetsCarryABuildIDInTheirPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "taskpane.html"),
		[]byte(`<link rel="stylesheet" href="taskpane.css" /><!--MAGI_BOOT--><script type="module" src="src/main.js"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.js"), []byte("export const a=1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Pages{Root: root, Token: "t"}
	page := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/taskpane.html", nil)
		req.RemoteAddr = "127.0.0.1:5555"
		p.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	first := page()
	if !strings.Contains(first, `src="/v/`) || !strings.Contains(first, `href="/v/`) {
		t.Fatalf("자산 주소에 판본이 안 꼈다:\n%s", first)
	}

	// **파일이 바뀌면 주소가 바뀐다** — 그것이 이 수의 전부다.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "src", "main.js"), []byte("export const a=2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := page(); second == first {
		t.Error("파일을 고쳤는데 주소가 그대로다 — 캐시가 옛것을 계속 준다")
	}

	// 그리고 **판본이 낀 주소로 지금 파일이 온다.** 옛 판본으로 들어와도 404 로 죽이지 않는다 —
	// 열려 있던 창이 그 자리에서 멎는다.
	rec := httptest.NewRecorder()
	vreq := httptest.NewRequest("GET", "/v/deadbeef0000/src/main.js", nil)
	vreq.RemoteAddr = "127.0.0.1:5555"
	p.Handler().ServeHTTP(rec, vreq)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "a=2") {
		t.Errorf("판본 낀 주소가 지금 파일을 안 준다: %d %q", rec.Code, rec.Body.String())
	}
}
