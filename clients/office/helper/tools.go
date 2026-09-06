package office

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 헬퍼가 `tools/list` 로 올리는 목록 — 엑셀 판.
//
// 도구는 Excel.js 호출 한 묶음에 대응하고, 실행하는 것은 **애드인(작업창)**이다. 이 파일이 지는 것은
// 셋이다 — 스키마, 인자 검사, 그리고 결과 봉투(어느 통합 문서를 손댔는지·무엇이 어떻게 바뀌었는지).
// 파워포인트 판(clients/powerpoint/helper/tools.go)과 같은 구조이고, 거기서 배운 것을 그대로 지킨다:
// 예시 값은 계약이다(모델이 그대로 쓴다), 열거형은 광고하고 거절문에 대안을 적는다, 못 하는 것은
// 광고하지 않는다(「고쳤습니다」하고 아무것도 안 바뀌는 것이 최악이다).
//
// # 주소 어휘
//
// 파워포인트의 `slide`/`slide_id`/`shape_id` 자리에 엑셀은 `sheet`(이름, 생략 = 사람이 보고 있는 시트)와
// `address`(A1 표기, "B2:E9"·"C3"·"A:A")가 선다. 시트 이름은 사람의 어휘 그대로다 — 「2분기」시트는
// "2분기"다. 표·차트·이름은 제 이름으로 부른다(`table`·`chart`·`name`).
//
// # 여기 없는 것은 일부러 없다
//
// 매크로(VBA)·외부 데이터 연결·파워 쿼리·통합 문서 열기/저장/닫기(Office.js 가 못 한다 — 사람이 하는 일),
// 셀 하나하나의 리치 텍스트 부분 서식(1.18), 노트(1.18 — 2021 에 없다). 2021(ExcelApi 1.14)에서 도는 것만
// 광고한다.

// Revision 은 통합 문서 하나의 개정 쌍(§5.6·§6). `Known` 이 거짓이면 「안 바뀌었다」가 아니라 **「모른다」**다.
type Revision struct {
	Known bool `json:"known"`
	Epoch int  `json:"epoch,omitempty"`
	Count int  `json:"count,omitempty"`
}

// HandResult 는 애드인이 조작 하나를 마치고 돌려주는 것.
type HandResult struct {
	// Document 는 **실제로 손댄 통합 문서**다. 받은 인자를 되받아 적는 것이 아니다(§6).
	Document string `json:"document"`
	// Label 은 사람이 부르는 이름(대개 파일 이름). **키가 아니다.**
	Label    string         `json:"label,omitempty"`
	Result   map[string]any `json:"result,omitempty"`
	Changed  []string       `json:"changed,omitempty"`
	Revision *Revision      `json:"revision,omitempty"`
}

// Hand 는 통합 문서에 닿는 유일한 구멍. 구현은 붙어 있는 애드인이고, 시험에서는 가짜 손이다.
type Hand interface {
	// Attached 는 지금 손이 있는가. 없을 때 도구는 **실패해야 하고, 사유가 「Excel 에 붙어 있지 않다」여야
	// 한다** — 조용히 빈 결과를 주면 에이전트가 통합 문서가 비어 있다고 읽는다.
	Attached() bool
	// Call 은 조작 하나를 넘긴다. document 가 빈 문자열이면 활성 문서다.
	Call(ctx context.Context, document, op string, args map[string]any) (HandResult, error)
}

// property 는 스키마 한 칸. 순서를 지키려고 슬라이스로 든다.
type property struct {
	Name string
	Type string // "string" | "integer" | "number" | "boolean" | "array" | "object"
	Desc string
	// Items 는 배열 항목의 타입. 비면 배열이 아니다.
	Items string
	// Also 는 이 칸의 별칭 — 모델이 흔히 쓰는 다른 이름. 스키마에 같이 광고한다(파워포인트 판이
	// 겪은 `[ignored arguments]` 거짓 경고의 교훈).
	Also []string
	// Enum 은 값의 열거. 있으면 스키마에 광고하고, 거절문이 이것을 적는다.
	Enum []string
}

// tool 은 목록의 한 줄.
type tool struct {
	Name string
	Desc string
	// Props 는 `document` 를 **뺀** 나머지다. 그 칸은 모든 도구가 같이 받으므로 한 자리에서 붙인다.
	Props    []property
	Required []string
	// ReadOnly 는 **통합 문서를 고치지 않는가**다. 허용 규칙의 기준이 이것이지 읽기/쓰기라는 제목이
	// 아니다 — `advise` 는 읽기 표에 없지만 문서를 안 고치고, `snapshot_range` 는 되돌리기 짝의
	// 절반이지만 읽기만 한다.
	ReadOnly bool
}

// sheetProp 는 시트를 고르는 칸. 생략 = 사람이 보고 있는 시트(activeWorksheet).

// 문단은 **1부터 세는 번호**로 가리킨다 — 본문(body)의 문단 순서다. Word.js 에는 문단의 안정된 id 가 없으므로
// list_paragraphs 가 준 번호가 손잡이이고, 고친 뒤에는 번호가 밀릴 수 있어 답에 `now` 가 실린다.

// schemaOf 는 도구 하나의 `inputSchema` 를 짓는다. `properties` 와 `required` 를 반드시 적는다 —
// magi 는 디스패치 직전에 보낸 키를 스키마와 맞춰 보는데, `properties` 를 못 읽으면 그 검사가 아무
// 의견도 안 낸다(파워포인트 판 §4.3).
func schemaOf(app *App, t tool) json.RawMessage {
	props := map[string]any{}
	for _, p := range append([]property{app.DocumentProp}, t.Props...) {
		entry := map[string]any{"type": p.Type, "description": p.Desc}
		if p.Items != "" {
			entry["items"] = map[string]any{"type": p.Items}
		}
		if len(p.Enum) > 0 {
			entry["enum"] = p.Enum
		}
		props[p.Name] = entry
		for _, alias := range p.Also {
			props[alias] = map[string]any{"type": p.Type, "description": "Same as " + p.Name + " — prefer " + p.Name + "."}
		}
	}
	required := t.Required
	if required == nil {
		required = []string{}
	}
	b, err := json.Marshal(map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	})
	if err != nil {
		panic("schemaOf: " + err.Error())
	}
	return b
}

// allowRules 는 오퍼레이터가 config 에 적을 허용 규칙이다. 기준은 「통합 문서를 고치는가」.
func (a *App) AllowRules() []string {
	var out []string
	for _, t := range a.Catalogue(false) {
		if t.ReadOnly {
			out = append(out, fmt.Sprintf("mcp__%s__%s(**)", a.Key, t.Name))
		}
	}
	sort.Strings(out)
	return out
}

// AllowRulesTOML 은 사람에게 그대로 붙여 넣으라고 내주는 모양이다.
func (a *App) AllowRulesTOML() string {
	var b strings.Builder
	b.WriteString("# magi office (" + a.Key + "): " + a.NounKo + "를 고치지 않는 도구만 허용한다.\n")
	b.WriteString("# 쓰기 도구는 일부러 빠져 있다 — " + a.NounKo + "를 고치는 것은 물어야 하는 일이 맞다.\n")
	b.WriteString("allow = [\n")
	for _, r := range a.AllowRules() {
		b.WriteString("  \"" + r + "\",\n")
	}
	b.WriteString("]\n")
	return b.String()
}
