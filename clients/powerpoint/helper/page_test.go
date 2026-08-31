package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	return &Pages{Root: dir, Token: "tok", Boot: map[string]any{"origin": "https://127.0.0.1:3000"}}, dir
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
	if !strings.Contains(got, `"origin":"https://127.0.0.1:3000"`) {
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
