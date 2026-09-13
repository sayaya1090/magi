package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// **환경 때문에 빨간 수트는 사람이 무시하는 법을 배운다** — 그래서 이 문의 규칙을 못 박는다.
//
// `serves` 는 셋(`internal/app`·`internal/eval`·`internal/adapter/llm/openai`)에 같은 글로 복사되어
// 있다. 시험 파일은 패키지를 넘어 못 빌려 쓰고, 이 한 함수를 위해 배포되는 패키지를 새로 내는 것은
// 반대 방향의 값이라 판단했다 — 복사본을 **글자까지 같게** 두고, 규칙은 여기서 한 번 잰다.
// (앞의 `reachable` 도 셋이었고, 그쪽은 시험이 아예 없었다.)
func TestTheE2EGateSkipsOnlyWhenItKnows(t *testing.T) {
	for _, c := range []struct {
		name, body string
		status     int
		want       bool
	}{
		// 서버가 그 판을 준다고 말한다 → 돈다.
		{"lists the model", `{"object":"list","data":[{"id":"qwen3-coder:30b"}]}`, 200, true},
		{"lists it among others", `{"object":"list","data":[{"id":"a"},{"id":"qwen3-coder:30b"}]}`, 200, true},
		// 목록이 있는데 그 판이 없다 → 요청은 404 로 끝난다. 그것이 「모델이 없다」다.
		{"lists other models", `{"object":"list","data":[{"id":"llama3"}]}`, 200, false},
		// ⚠ 빈 목록은 **모르겠다가 아니라 없다**다. 이 기계의 ollama 가 실제로 주는 답이
		// (`{"object":"list","data":null}`, 실측) 이 모양이고, 그것을 「모르겠다」로 읽은 것이
		// 이 문의 첫 판을 그냥 통과시켰다.
		{"an empty but well-formed listing", `{"object":"list","data":null}`, 200, false},
		{"an empty array", `{"object":"list","data":[]}`, 200, false},
		// 목록이 아닌 것은 거절이 아니다. 열거를 안 하는 호환 서버가 있고, 거기서 건너뛰면 **되던
		// 시험이 조용히 안 돌게** 된다 — 그쪽은 요청이 답하게 둔다.
		{"not a listing", `{"error":"nope"}`, 200, true},
		{"not even JSON", `<html>no</html>`, 200, true},
		{"a 404 on /models", `not found`, 404, true},
		// 서버가 고장이면 아무것도 모른다 → 건너뛴다.
		{"a broken server", `boom`, 500, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models" {
					t.Errorf("문이 %q 를 물었다 — /models 여야 한다", r.URL.Path)
				}
				w.WriteHeader(c.status)
				fmt.Fprint(w, c.body)
			}))
			defer srv.Close()
			if got := serves(srv.URL, "qwen3-coder:30b"); got != c.want {
				t.Errorf("serves(%s) = %v, want %v — 본문: %s", c.name, got, c.want, c.body)
			}
		})
	}
	// 아무도 안 듣는 주소: 건너뛴다(그것이 원래 하던 일이고, 그대로여야 한다).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if serves(url, "anything") {
		t.Error("아무도 안 듣는데 돈다고 답했다")
	}
}
