package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 인자 검사. **모르는 키는 서버가 거절한다**(DESIGN.md §4.3).
//
// magi 는 디스패치 직전에 키를 스키마와 맞춰 보되 둘로 갈라 다르게 답한다: 선언된 키의 흔한
// 오타면 안 돌리고 진짜 이름을 알려 주고, **그 밖의 모르는 키는 그대로 돈다** — 결과 뒤에
// `[ignored arguments]` 한 줄이 붙을 뿐이다. 그래서 우리가 선언한 적 없는 `keep_formatting:true`
// 가 `set_text` 에 실려 오면 그건 magi 를 통과해 여기 도착하고, 조용히 무시하면 §2.3 이 최악이라고
// 적은 실패(「고쳤습니다」 하고 안 바뀌는 것)가 인자 한 칸에서 난다.
//
// 겹쳐 말하는 것이 아니라 **magi 가 안 막기로 정한 자리를 우리가 막는 것**이다.

// argError 는 사람이 아니라 모델이 읽는다. 그래서 무엇이 틀렸는지와 **무엇을 쓸 수 있는지**를
// 같이 적는다 — 받아들일 수 있는 이름을 안 적으면 모델은 다음 시도에서 또 지어낸다.
type argError struct{ msg string }

func (e argError) Error() string { return e.msg }

func validateArgs(t tool, raw json.RawMessage) (map[string]any, error) {
	args := map[string]any{}
	// **비어 있다고 말하는 방식이 셋이다: 안 보냄 · `null` · `[]`.** 셋 다 뜻이 같다 —
	// 「이 호출에 인자가 없다」.
	//
	// 앞 판본은 앞의 둘만 받았다. 실물에서 그 화면을 봤다(2026-09-04, Mac): 첫 호출이
	// `mcp__ppt__list_slides []` 였고 —— 인자가 하나도 필요 없는 도구다 —— 답이
	// `arguments must be a JSON object: json: cannot unmarshal array into Go value of type
	// map[string]interface {}` 였다. 사람이 본 것은 대화 첫 줄의 **✗ 실패했습니다**이고, 모델은
	// 아무것도 안 틀렸다.
	//
	// **빈 배열만 받는다.** 값이 든 배열은 진짜로 틀린 것이라 그대로 거절하되, 아래 문구가
	// 무엇을 보내야 하는지 말한다 — 모델이 읽는 글이라 사유만 적으면 다음 시도에서 또 지어낸다.
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "[]" {
		trimmed = "{}"
	}
	if trimmed != "" && trimmed != "null" {
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber() // 정수를 float 로 바꾸면 슬라이드 번호가 3 이 아니라 3.0000000001 로 간다
		if err := dec.Decode(&args); err != nil {
			return nil, argError{fmt.Sprintf(
				"arguments must be a JSON object like {\"address\": \"B2\"} — an empty call is {} or [] (%v)", err)}
		}
	}

	// **안 준 것과 `null` 은 같은 뜻이다.**
	//
	// JSON 을 짓는 모델은 선택 인자를 생략하는 대신 `null` 로 채우는 일이 잦다 — 스키마의 칸을
	// 하나씩 훑으며 값을 넣기 때문이다. 그리고 `document: null` 은 `document` 를 안 준 것과
	// **뜻이 정확히 같다**: 어느 쪽이든 「붙어 있는 그 판」이다.
	//
	// 앞 판본은 그것을 型 오류로 되받았다. 실물에서 그 화면을 봤다(2026-09-02): 사람이 「4분기
	// 계획이라는 제목으로 새 장 하나 만들어 줘」라고 했고 모델은 전부 옳게 했는데 —
	// `add_slide{document:null, layout:null, at:null, title:"[4분기 계획]"}` — 호출이
	// `"document" must be a string (got null)` 로 튕겼고, 장은 안 생겼다. 사람은 아무 일도 안
	// 일어난 화면을 봤다.
	//
	// **필수 칸의 `null` 은 그대로 거절한다.** 아래 필수 검사가 「없음」으로 읽고 이름을 대어
	// 말한다 — 그쪽은 진짜로 빠뜨린 것이고, 지어내 주면 안 되는 자리다.
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}

	known := map[string]property{documentProp.Name: documentProp}
	for _, p := range t.Props {
		known[p.Name] = p
	}

	// **다른 이름으로 온 것을 제자리에 옮긴다.** 별칭은 스키마에 없으므로 모델이 볼 이름은
	// 여전히 하나고, 여기서 조용히 받아 준다 — 조용히 **버리는** 것이 아니라 **옮기는** 것이다.
	// 정본과 별칭이 같이 오면 둘 중 어느 쪽이 뜻인지 우리가 못 정하니 거절한다.
	for _, p := range t.Props {
		for _, other := range p.Also {
			v, ok := args[other]
			if !ok {
				continue
			}
			if _, taken := args[p.Name]; taken {
				return nil, argError{fmt.Sprintf(
					"%s: %q and %q are the same thing — send only %q. Nothing was changed — this call did not run.",
					t.Name, p.Name, other, p.Name)}
			}
			args[p.Name] = v
			delete(args, other)
		}
	}

	var unknown []string
	for k := range args {
		if _, ok := known[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, argError{fmt.Sprintf(
			"%s does not take %s. It takes: %s. Nothing was changed — this call did not run.",
			t.Name, strings.Join(quoted(unknown), ", "), strings.Join(quoted(names(known)), ", "))}
	}

	for _, r := range t.Required {
		v, ok := args[r]
		if !ok || v == nil {
			return nil, argError{fmt.Sprintf("%s needs %q. Nothing was changed — this call did not run.", t.Name, r)}
		}
	}

	for k, v := range args {
		if err := checkType(t.Name, known[k], v); err != nil {
			return nil, err
		}
	}

	// 슬라이드를 가리키는 두 칸 중 하나는 있어야 하는 도구가 있다 — 그런데 **없는 것이 답인
	// 도구도 있다**(`list_slides`·`advise`·`clear_advice`·`find_shapes` 는 덱 전체가 대상이다).
	// 그래서 여기서 강제하지 않고, 슬라이드가 필요한 도구는 손이 자기 말로 거절한다. 이 층에서
	// 「슬라이드를 안 줬다」를 지어내면 덱 전체가 대상인 호출이 못 돌게 된다.
	if s, ok := args["to"]; ok && t.Name == "move_sheet" {
		if n, err := asInt(s); err != nil || n < 1 {
			return nil, argError{fmt.Sprintf("%s: to is a 1-based tab position, so it starts at 1 (got %v)", t.Name, s)}
		}
	}

	// **세는 칸이 0 이면 거절한다.** 모델은 `count: 0` 을 「제한 없음」으로 쓰는데, 그것을 그대로
	// 받으면 「0 장을 달라」가 되어 **빈 목록과 total: 2 가 한 답에 같이 실린다.** 실측이다
	// (2026-09-01, 첫 라이브 턴): 모델은 그 답을 「덱을 못 읽었다」로 읽고 아무것도 안 고쳤다.
	//
	// 조용히 「전부」로 바꿔 주는 길도 있는데 안 고른다 — 그러면 다음에도 0 을 보내고, 이 도구가
	// 아닌 다른 도구에서 같은 낱말이 다른 뜻이 된다. **묻지 않은 질문에 답을 주지 않는 것**이
	// 이 층의 규칙이다(§4.3).
	for _, key := range []string{"count", "limit", "from", "rows", "columns"} {
		v, ok := args[key]
		if !ok {
			continue
		}
		// 엑셀에서 rows·columns 는 **목록**이기도 하다 — add_table_rows 의 행 배열, add_pivot 의 필드 이름.
		// 그 자리에서 이 검사가 「1부터」라고 거절했다(실물 2026-09-06). 수가 아닌 것은 이 검사의 것이 아니다.
		switch v.(type) {
		case []any, map[string]any:
			continue
		}
		n, err := asInt(v)
		if err != nil || n < 1 {
			return nil, argError{fmt.Sprintf(
				"%s: %q starts at 1 (got %v). Omit it rather than passing 0 — 0 would mean \"none\", and this call did not run",
				t.Name, key, v)}
		}
	}
	// 열거형 칸은 어느 깊이에든 오므로 나무를 걸어 거른다(enums.go · bullets.go).
	if err := checkEnums(t.Name, args); err != nil {
		return nil, err
	}
	return args, nil
}

// checkType 은 값의 모양만 본다. 뜻은 손이 본다 — 여기서 도형 id 가 존재하는지까지 물으면
// 왕복이 하나 더 들고, 그 답은 어차피 손이 실행할 때 다시 봐야 한다.
func checkType(toolName string, p property, v any) error {
	bad := func(want string) error {
		return argError{fmt.Sprintf("%s: %q must be %s (got %s)", toolName, p.Name, want, kindOf(v))}
	}
	switch p.Type {
	case "string":
		if _, ok := v.(string); !ok {
			return bad("a string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad("true or false")
		}
	case "integer":
		if _, err := asInt(v); err != nil {
			return bad("a whole number")
		}
	case "number":
		if _, err := asFloat(v); err != nil {
			return bad("a number")
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return bad("an array")
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return bad("an object")
		}
	}
	return nil
}

func asInt(v any) (int64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("not a number")
	}
	return n.Int64()
}

func asFloat(v any) (float64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("not a number")
	}
	return n.Float64()
}

func kindOf(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case bool:
		return "true or false"
	case json.Number:
		return "a number"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	case nil:
		return "null"
	}
	return "something else"
}

func names(m map[string]property) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func quoted(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, `"`+s+`"`)
	}
	return out
}

// documentOf 는 인자에서 문서를 꺼낸다. 빈 문자열이면 활성 문서다(§4.4 ④).
func documentOf(args map[string]any) string {
	if s, ok := args["document"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
