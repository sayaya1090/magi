package event

import (
	"encoding/json"
	"strings"
	"testing"
)

// 승인 화면이 사람에게 보여 주는 것은 **선 위의 모양**이다. Go 안에서만 맞으면 소용이 없다:
// `[]byte` 는 Go 끼리는 정상 왕복하지만(encoding/json 이 base64 로 싸고 다시 풀어 준다) 선 위에서는
// base64 문자열이라, Go 밖 클라이언트(VS Code·JetBrains)는 그 문자열을 그대로 받는다.
//
// 실물에서 그 대가를 봤다(2026-09-20, VS Code 1.137.0): 「이 편집을 허용하겠냐」는 카드에
// `eyJwYXRoIjoiU2FtcGxlLmt0Ii…` 184자가 그려졌고, **사람은 바뀔 내용을 못 봤다.** 게다가 앵커 편집
// ({at})은 `change.EditDiff` 가 정직한 diff 를 못 만들어 일부러 빈 값을 주므로, 그때 물러설 자리가
// 바로 이 인자 표시다 — 그 자리가 읽을 수 없으면 승인 화면에 읽을 것이 하나도 남지 않는다.
//
// 그래서 이 시험은 구조체를 채워 **실제로 json.Marshal 한 결과**를 잰다. 손으로 지은 「정상」 객체를
// 넣고 도는 시험은 이 결함을 영영 못 잡는다 — 결함이 사는 곳이 바로 그 직렬화이기 때문이다.
func TestPermissionArgsRideTheWireAsReadableJSON(t *testing.T) {
	for _, c := range []struct {
		name string
		args string
	}{
		{"한글과 따옴표", `{"path":"Sample.kt","at":"2","new":"    return \"반갑습니다, \" + who","old":"return \"안녕하세요, \" + who","replaceAll":false}`},
		{"빈 객체", `{}`},
		{"중첩과 배열", `{"path":"a.go","edits":[{"old":"x","new":"y"}],"n":3}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(PermissionRequestedData{CallID: "c1", Name: "edit", Args: []byte(c.args)})
			if err != nil {
				t.Fatalf("승인 사건을 못 지었다: %v", err)
			}
			// 선 위에서 args 가 무엇으로 보이는가. 클라이언트는 이 값을 그대로 화면에 쓴다.
			var wire struct {
				Args json.RawMessage `json:"args"`
			}
			if err := json.Unmarshal(b, &wire); err != nil {
				t.Fatalf("사건을 못 읽었다: %v", err)
			}
			got := strings.TrimSpace(string(wire.Args))
			if strings.HasPrefix(got, `"`) {
				t.Fatalf("인자가 선 위에서 **문자열 한 덩이**로 간다 — 클라이언트가 그대로 그리면 사람은 무엇을 승인하는지 못 읽는다.\n받은 것: %s\n바라는 것: %s", got, c.args)
			}
			// 내용도 같아야 한다: 읽히기만 하고 다른 것이 담기면 더 나쁘다.
			var a, b2 any
			if json.Unmarshal([]byte(got), &a) != nil || json.Unmarshal([]byte(c.args), &b2) != nil {
				t.Fatalf("선 위 인자가 JSON 이 아니다: %s", got)
			}
			ga, _ := json.Marshal(a)
			gb, _ := json.Marshal(b2)
			if string(ga) != string(gb) {
				t.Fatalf("선 위 인자가 원본과 다르다\n받은 것: %s\n원본: %s", ga, gb)
			}
		})
	}
}

// 모델이 JSON 이 아닌 것을 인자로 낼 수 있다. 그때 **빈 인자로 바꿔치지 않는다**: 인자가 있던 호출에
// 조용히 `{}` 를 그리면 사람은 「인자가 없는 호출」과 구별할 수 없고, 그건 못 읽는 것보다 나쁘다.
// 원문을 JSON 문자열로 싸서 보낸다 — 읽히고, 파싱 안 된 것이라는 사실도 남는다.
func TestToolArgsThatAreNotJSONStayVisible(t *testing.T) {
	raw := []byte(`path=Sample.kt (모델이 JSON 을 안 냈다)`)
	w := ToolArgsJSON(raw)
	if !json.Valid(w) {
		t.Fatalf("선 위 값이 JSON 이 아니다 — 사건 전체가 직렬화에서 깨진다: %s", w)
	}
	var got string
	if err := json.Unmarshal(w, &got); err != nil {
		t.Fatalf("문자열로 안 싸였다: %v (%s)", err, w)
	}
	if got != string(raw) {
		t.Fatalf("원문이 바뀌었다\n받은 것: %q\n원문: %q", got, raw)
	}
	if string(w) == "{}" || string(w) == "null" {
		t.Fatalf("인자가 있던 호출이 빈 인자로 보인다 — 없는 호출과 구별이 안 된다")
	}
	// 인자가 아예 없으면 없는 대로 둔다.
	if ToolArgsJSON(nil) != nil {
		t.Fatalf("인자 없는 호출에 없던 값을 지어냈다")
	}
}

// 고치기 전에 기록된 로그에는 base64 문자열이 들어 있다. 그 로그를 다시 재생해도 사람이 읽을 수
// 있어야 한다 — 고침이 과거를 못 읽게 만들면 그건 다른 결함이다. 다만 **추측으로 디코딩하지
// 않는다**: base64 처럼 생겼다는 이유로 사람의 글자를 알아볼 수 없는 것으로 바꾸지 않는다.
func TestToolArgsTextReadsOldAndNew(t *testing.T) {
	args := `{"path":"Sample.kt","new":"반갑습니다"}`
	for _, c := range []struct{ name, wire, want string }{
		{"지금 모양(인라인)", args, args},
		{"옛 모양(base64 문자열)", `"eyJwYXRoIjoiU2FtcGxlLmt0IiwibmV3Ijoi67CY6rCR7Iq164uI64ukIn0="`, args},
		{"JSON 아닌 원문", `"path=Sample.kt"`, "path=Sample.kt"},
		{"base64 처럼 생긴 사람 글자", `"deadbeef"`, "deadbeef"},
		{"없음", `null`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ToolArgsText(json.RawMessage(c.wire)); got != c.want {
				t.Fatalf("잘못 읽었다\n받은 것: %q\n바라는 것: %q", got, c.want)
			}
		})
	}
}
