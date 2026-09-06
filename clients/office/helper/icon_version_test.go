package office

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// **주소에 판 번호 한 칸.** Office 는 애드인 자원을 주소로 캐시하고, 같은 주소면 서버가 새 그림을 내도
// 영영 옛 그림을 그린다 — 실물 LTSC 2021 에서 9월 1일의 82바이트 주황 네모가 그대로 남아 있었다
// (2026-09-06). 쿼리(?v=2)를 붙이니 이번에는 리본이 **기본 자리표시자**를 그렸다 — Office 가 그 주소를
// 안 받는다. 그래서 번호를 경로에 둔다: /assets/v2/icon-32.png. 그림을 바꾸면 매니페스트의 번호를 올린다.
func TestAVersionedPathDrawsTheSameMark(t *testing.T) {
	p, _ := pagesAt(t)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	fetch := func(path string) (int, []byte) {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}

	plain, mark := fetch("/assets/icon-32.png")
	if plain != http.StatusOK {
		t.Fatalf("맨 주소가 %d", plain)
	}
	for _, path := range []string{"/assets/v2/icon-32.png", "/assets/v17/icon-32.png", "/assets/icon-32.png?v=2"} {
		code, body := fetch(path)
		if code != http.StatusOK {
			t.Errorf("%s: 상태 %d — 판 번호가 붙어도 같은 그림을 내야 한다", path, code)
			continue
		}
		if !bytes.Equal(body, mark) {
			t.Errorf("%s: 맨 주소와 다른 바이트를 냈다 — 그림은 하나뿐이어야 한다", path)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil || img.Bounds().Dx() != 32 {
			t.Errorf("%s: 32px PNG 가 아니다(%v)", path, err)
		}
	}

	// 판 번호는 v 뒤에 숫자만이다. 아무 칸이나 삼키면 낯선 주소가 그림으로 답한다.
	for _, path := range []string{"/assets/vx/icon-32.png", "/assets/v/icon-32.png", "/assets/2/icon-32.png", "/assets/v2/v3/icon-32.png"} {
		if code, _ := fetch(path); code == http.StatusOK {
			t.Errorf("%s: 200 — 판 번호 칸이 아닌 것을 삼켰다", path)
		}
	}
}
