package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// 스키마가 검사를 켜 두는가(DESIGN.md §4.3).
//
// magi 는 디스패치 직전에 보낸 키를 스키마와 맞춰 보는데, `properties` 를 못 읽으면 **아무
// 의견도 안 낸다.** 그리고 빈 `inputSchema` 는 매니저가 `{"type":"object"}` 로 채워 넣어서
// **모양은 멀쩡한데 검사만 꺼진 스키마**가 된다. 그러니 한 도구라도 빠지면 그 도구만 조용해진다.
func TestEverySchemaKeepsTheArgumentCheckOn(t *testing.T) {
	tools := catalogue()
	if len(tools) == 0 {
		t.Fatal("도구가 하나도 없다 — 볼 것이 없었다")
	}
	for _, tl := range tools {
		var body struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Additional *bool                      `json:"additionalProperties"`
		}
		if err := json.Unmarshal(schemaOf(tl), &body); err != nil {
			t.Fatalf("%s: 스키마가 JSON 이 아니다: %v", tl.Name, err)
		}
		if body.Type != "object" {
			t.Errorf("%s: type 이 %q 다", tl.Name, body.Type)
		}
		if len(body.Properties) == 0 {
			t.Errorf("%s: properties 가 비었다 — 이 도구만 인자 검사가 꺼진다", tl.Name)
		}
		if body.Required == nil {
			t.Errorf("%s: required 가 없다", tl.Name)
		}
		if body.Additional == nil || *body.Additional {
			t.Errorf("%s: additionalProperties 가 false 가 아니다 — 모르는 키가 그냥 지나간다", tl.Name)
		}
		// 필수 칸은 반드시 properties 안에 있어야 한다. 이름을 고치다 한쪽만 고치면
		// 모델은 채울 수 없는 것을 요구받는다.
		for _, r := range body.Required {
			if _, ok := body.Properties[r]; !ok {
				t.Errorf("%s: required 의 %q 가 properties 에 없다", tl.Name, r)
			}
		}
	}
	t.Logf("도구 %d 개의 스키마를 봤다", len(tools))
}

// 모든 도구가 `document` 를 받는다(§4.4 ④ — MCP 에 scope 개념이 없어서 인자로 받는다).
// 하나라도 빠지면 그 도구만 언제나 활성 문서를 향하고, 창이 둘일 때 목표가 사람이 마지막에
// 누른 창을 조용히 따라간다.
func TestEveryToolTakesTheDocument(t *testing.T) {
	for _, tl := range catalogue() {
		var body struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		_ = json.Unmarshal(schemaOf(tl), &body)
		if _, ok := body.Properties["document"]; !ok {
			t.Errorf("%s: document 칸이 없다", tl.Name)
		}
		for _, r := range body.Required {
			if r == "document" {
				t.Errorf("%s: document 를 필수로 걸었다 — 생략하면 활성 문서다", tl.Name)
			}
		}
	}
}

// 도구 이름이 다듬기를 지나도 그대로여야 한다(§5.0.6 의 이유가 서버 이름에만 걸리는 게 아니다).
// 이름이 다듬어지면 allow 룰에 적어야 하는 이름과 목록에서 보는 이름이 갈린다.
func TestToolNamesSurviveSanitizing(t *testing.T) {
	safe := regexp.MustCompile(`^[a-z0-9_]+$`)
	seen := map[string]bool{}
	for _, tl := range catalogue() {
		if !safe.MatchString(tl.Name) {
			t.Errorf("도구 이름 %q 에 다듬기가 손댈 문자가 있다", tl.Name)
		}
		if seen[tl.Name] {
			t.Errorf("도구 이름 %q 가 둘이다 — 레지스트리는 이름으로 덮어쓴다", tl.Name)
		}
		seen[tl.Name] = true
	}
}

// 허용 규칙의 기준은 「덱을 고치는가」이지 표의 제목이 아니다(§6).
//
// 앞 판본의 설계가 이걸 "읽기에만 준다"로 적었고, 그렇게 가르면 도구 둘이 틀린 쪽에 선다 —
// `advise` 는 읽기 표에 없지만 덱을 안 고치고, `restore_slide` 는 되돌리기 짝이지만 덱을 쓴다.
func TestAllowRulesCoverExactlyWhatDoesNotChangeTheDeck(t *testing.T) {
	rules := allowRules()
	if len(rules) == 0 {
		t.Fatal("허용 규칙이 하나도 없다 — 그러면 읽기마다 사람에게 물음이 뜬다")
	}
	has := func(name string) bool {
		for _, r := range rules {
			if r == "mcp__"+ServerName+"__"+name+"(**)" {
				return true
			}
		}
		return false
	}
	for _, name := range []string{
		"list_slides", "read_slide", "render_slide", "export_slide_ooxml", "find_shapes",
		"snapshot_slide", "advise", "clear_advice",
	} {
		if !has(name) {
			t.Errorf("%s 가 허용 규칙에 없다 — 이건 덱을 안 고친다", name)
		}
	}
	for _, name := range []string{
		"set_text", "format_shape", "move_shape", "add_shape", "delete_shape",
		"apply_layout", "reorder_slide", "set_hyperlink", "add_table", "set_table_cells",
		"restore_slide",
	} {
		if has(name) {
			t.Errorf("%s 가 허용 규칙에 있다 — 덱을 고치는 것은 물어야 하는 일이다", name)
		}
	}
	// 규칙의 도구 자리에는 와일드카드가 없다(§4.4 ②). `list_*` 같은 줄은 `parseRule` 을
	// 통과하고 **어떤 호출에도 안 걸린다** — 그 실패의 증상이 「규칙을 아직 안 썼다」와 같다.
	for _, r := range rules {
		if strings.Contains(r, "*") && !strings.HasSuffix(r, "(**)") {
			t.Errorf("규칙 %q 의 도구 자리에 글롭이 있다 — 그 줄은 아무것도 안 건다", r)
		}
		if strings.HasPrefix(r, "*") {
			t.Errorf("규칙 %q 는 모든 도구를 여는 규칙이다", r)
		}
	}
	t.Logf("허용 규칙 %d 줄을 봤다", len(rules))
}

// 읽기 도구의 설명문이 선언을 안내한다(§7).
//
// 서버가 적어 보내는 instructions 는 모델에 **도달하지 않는다** — magi 의 MCP 클라이언트가
// 핸드셰이크 응답을 통째로 버린다. 남는 자리가 `tools/list` 의 설명문뿐이라, 이 문장이 빠지면
// 조회만 한 턴이 조르기 세 번을 지나 UNVERIFIED 로 착지한다. 아무것도 안 고친 조회에.
func TestReadToolsSayHowToDeclareFinished(t *testing.T) {
	checked := 0
	for _, tl := range catalogue() {
		if tl.Name == "advise" || tl.Name == "clear_advice" || tl.Name == "snapshot_slide" {
			continue // 덱을 안 고치지만 조회 도구는 아니다
		}
		if !tl.ReadOnly {
			continue
		}
		checked++
		if !strings.Contains(tl.Desc, "council{complete:true}") {
			t.Errorf("%s: 설명문이 선언을 안 안내한다", tl.Name)
		}
	}
	if checked == 0 {
		t.Fatal("조회 도구를 하나도 못 찾았다 — 볼 것이 없었다")
	}
	t.Logf("조회 도구 %d 개의 설명문을 봤다", checked)
}

// 못 하는 것을 인자 자리로 광고하지 않는다(§2.3·§6).
//
// 1.8 이 주는 표 쓰기는 정확히 둘이다 — 만들면서 서식까지 주는 `addTable`, 그리고 만든 뒤에
// 고칠 수 있는 유일한 것인 `TableCell.text`. 그래서 `set_table_cells` 에 서식 인자가 있으면
// 그건 「고쳤습니다」 하고 안 바뀌는 실패를 인자 한 칸에 심어 두는 것이다.
func TestSetTableCellsAdvertisesNoFormatting(t *testing.T) {
	var found *tool
	for _, tl := range catalogue() {
		if tl.Name == "set_table_cells" {
			c := tl
			found = &c
		}
	}
	if found == nil {
		t.Fatal("set_table_cells 가 없다")
	}
	banned := []string{"font", "size", "bold", "italic", "color", "fill", "border", "merge", "style", "align", "rows", "columns"}
	for _, p := range found.Props {
		for _, b := range banned {
			if strings.Contains(p.Name, b) {
				t.Errorf("set_table_cells 가 %q 를 받는다 — 이미 있는 표의 그것은 1.9 라 이 바닥에서 못 한다", p.Name)
			}
		}
	}
}
