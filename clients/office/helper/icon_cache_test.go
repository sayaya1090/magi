package office

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// **아이콘은 캐시할 수 있어야 한다.** 페이지 손잡이는 모든 것에 no-store 를 거는데(page.go — 낡은 JS 와 새
// CSS 가 섞이던 사고), Office 의 리본은 아이콘을 자기 캐시 파일에서 그린다. 캐시할 수 없는 그림은 기본
// 자리표시자로 떴다(2026-09-06, LTSC 2021). 주소에 판 번호가 있으니 오래 캐시해도 그림이 바뀌면 새 주소로
// 새로 받는다. 페이지 쪽 no-store 는 그대로다 — 그쪽 사고가 다시 나면 안 된다.
func TestTheIconIsCacheableWhileThePageIsNot(t *testing.T) {
	p, _ := pagesAt(t)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	icon, err := http.Get(srv.URL + "/assets/v2/icon-32.png")
	if err != nil {
		t.Fatal(err)
	}
	icon.Body.Close()
	cc := icon.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-store") || !strings.Contains(cc, "max-age=") {
		t.Fatalf("아이콘의 Cache-Control 이 %q — 리본이 캐시 못 하는 그림은 자리표시자가 된다", cc)
	}

	page, err := http.Get(srv.URL + "/taskpane.html")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if !strings.Contains(page.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("페이지의 Cache-Control 이 %q — 페이지는 여전히 no-store 여야 한다", page.Header.Get("Cache-Control"))
	}
}
