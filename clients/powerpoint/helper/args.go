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
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber() // 정수를 float 로 바꾸면 슬라이드 번호가 3 이 아니라 3.0000000001 로 간다
		if err := dec.Decode(&args); err != nil {
			return nil, argError{fmt.Sprintf("arguments must be a JSON object: %v", err)}
		}
	}

	known := map[string]property{documentProp.Name: documentProp}
	for _, p := range t.Props {
		known[p.Name] = p
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
	if s, ok := args["slide"]; ok {
		if n, err := asInt(s); err != nil || n < 1 {
			return nil, argError{fmt.Sprintf("%s: slide is a 1-based position, so it starts at 1 (got %v)", t.Name, s)}
		}
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
