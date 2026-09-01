package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 헬퍼가 `tools/list` 로 올리는 목록(DESIGN.md §6).
//
// 도구는 Office.js 호출 하나에 대응하고, 실행하는 것은 **애드인**이다. 이 파일이 지는 것은
// 셋이다 — 스키마, 인자 검사, 그리고 결과 봉투(어느 문서를 손댔는지·무엇이 어떻게 바뀌었는지).
//
// # 여기 없는 것은 일부러 없다
//
// 차트·SmartArt·애니메이션·발표자 노트 편집, 이미 있는 표의 구조·서식 변경, 마스터·레이아웃
// 편집, OOXML 직접 쓰기. 전부 §6 의 「의도적으로 선언하지 않는 것」이다. 못 하는 것을 도구로
// 광고하면 §2.3 이 최악이라고 적은 실패가 난다 — 「고쳤습니다」 하고 아무것도 안 바뀌는 것.
// magi 에는 광고와 실행이 어긋났을 때 모델이 그 도구를 부르고, 거부당하고, 다시 부르다 루프
// 가드에 죽은 기록이 있다.

// Revision 은 슬라이드 하나의 개정 쌍이다(§5.6·§6). `Known` 이 거짓이면 「안 바뀌었다」가
// 아니라 **「모른다」**다 — 헬퍼가 재시작하면 그 사이를 아무도 못 봤고, 둘을 같은 값으로
// 접으면 화면이 없던 보장을 하게 된다.
type Revision struct {
	Known bool `json:"known"`
	Epoch int  `json:"epoch,omitempty"`
	Count int  `json:"count,omitempty"`
}

// HandResult 는 애드인이 조작 하나를 마치고 돌려주는 것.
type HandResult struct {
	// Document 는 **실제로 손댄 문서**다. 받은 인자를 되받아 적는 것이 아니다(§6) — 생략했을
	// 때 답이 되는 쪽이 그것이고, 되받아 적기만 하면 생략한 호출은 여전히 아무것도 안 말한다.
	Document string `json:"document"`
	// Label 은 사람이 부르는 이름(대개 파일 이름). **키가 아니다**(§5.6 — 경로는 키가 아니다).
	Label    string         `json:"label,omitempty"`
	Result   map[string]any `json:"result,omitempty"`
	Changed  []string       `json:"changed,omitempty"`
	Revision *Revision      `json:"revision,omitempty"`
}

// Hand 는 덱에 닿는 유일한 구멍. 구현은 붙어 있는 애드인이고, 시험에서는 가짜 손이다.
type Hand interface {
	// Attached 는 지금 손이 있는가. 없을 때 도구는 **실패해야 하고, 사유가 「PowerPoint 에
	// 붙어 있지 않다」여야 한다**(§5.4) — 조용히 빈 결과를 주면 에이전트가 덱이 비어 있다고
	// 읽는다.
	Attached() bool
	// Call 은 조작 하나를 넘긴다. document 가 빈 문자열이면 활성 문서다(§4.4 ④).
	Call(ctx context.Context, document, op string, args map[string]any) (HandResult, error)
}

// property 는 스키마 한 칸. 순서를 지키려고 슬라이스로 든다 — 맵으로 두면 `tools/list` 의
// 출력이 런마다 흔들리고, 골든으로 물기가 어려워진다.
type property struct {
	Name string
	Type string // "string" | "integer" | "number" | "boolean" | "array" | "object"
	Desc string
	// Items 는 array 일 때의 원소 타입. 비면 제약을 안 적는다.
	Items string
}

// tool 은 도구 하나.
type tool struct {
	Name string
	Desc string
	// Props 는 `document` 를 **뺀** 나머지다. 그 칸은 모든 도구가 같이 받으므로 한 자리에서
	// 붙인다(§6: "모든 도구가 `document` 를 옵션으로 받는다").
	Props    []property
	Required []string
	// ReadOnly 는 **덱을 고치지 않는가**다. 허용 규칙의 기준이 이것이지 읽기/쓰기라는 제목이
	// 아니다(§6) — `advise` 는 읽기 표에 없지만 덱을 안 고치고, `snapshot_slide` 는 되돌리기
	// 짝의 절반이지만 덱을 읽기만 한다.
	ReadOnly bool
}

// 슬라이드를 가리키는 두 칸. 어느 도구든 같은 뜻이어야 해서 한 자리에 적는다.
//
// 모델은 **번호로** 말한다(CAPABILITIES.md §10.4 — 사람도 모델도 "3번 슬라이드"라고 하지
// id=257 이라고 하지 않는다). 다만 그 번호는 슬라이드 하나만 넣어도 뒤가 전부 밀리는 값이라,
// 읽은 결과에 실려 온 `slide_id` 를 그대로 되쓰는 쪽이 정확하다. 둘 다 받고, 있으면 id 가 이긴다.
// **생략했을 때의 뜻을 두 칸이 다 적는다.** 손은 진작부터 「사람이 보고 있는 장」으로 떨어지는데
// (`OfficeHand.#slide`, `officehand.mjs` 가 그것을 잰다) 스키마가 그 말을 안 했다 — 모델이 읽는
// 것은 스키마뿐이라, 사람이 "3행 5열 테이블 만들어 줘"라고 했을 때 **모델은 어느 슬라이드인지
// 되물었다.** 실물에서 그 왕복을 봤다(2026-09-01): 도구는 그냥 됐을 텐데 화면에는 되묻는 말만
// 섰고, 사람 눈에는 요청이 씹힌 것으로 보였다. 있는 능력을 안 광고하면 없는 것과 같다.
var slideProps = []property{
	{Name: "slide", Type: "integer", Desc: "1-based position of the slide, as a person would say it (\"slide 3\"). Positions shift when slides are added, removed or reordered, so prefer slide_id when you have one. Omit both this and slide_id for the slide the person is looking at now — do not ask which slide when they did not name one."},
	{Name: "slide_id", Type: "string", Desc: "Exact slide id, as returned by list_slides or read_slide. Wins over slide when both are given. Omit both this and slide for the slide the person is looking at now."},
}

func withSlide(rest ...property) []property {
	return append(append([]property{}, slideProps...), rest...)
}

// catalogue 는 §6 의 목록 그대로다. 순서는 읽기 → 쓰기 → 되돌리기 → 안내.
func catalogue() []tool {
	// 읽기 도구의 설명문에 **선언 안내가 붙는다**(§7). 이유가 기전에 있다: magi 의 MCP
	// 클라이언트는 핸드셰이크 응답을 통째로 버려서 서버가 적어 보내는 instructions 가 모델에
	// 도달하지 않는다. 남는 자리가 `tools/list` 의 설명문뿐이라, 이 문장은 예의가 아니라
	// **도달 가능한 유일한 자리**다. 조회만 한 턴도 선언하게 두는 값이 그것이다 — 안 하면
	// 모델 왕복 세 번에 「확인되지 않음」 배너까지 붙는다.
	const declare = " A turn that called any tool must end by declaring it finished with council{complete:true}, even a read-only one: otherwise the turn lands UNVERIFIED."

	return []tool{
		{
			Name:     "list_slides",
			Desc:     "The deck's table of contents: for every slide, its 1-based position, id, layout name and shape count. Start here — the answer also names the document every later call should address." + declare,
			Props:    []property{{Name: "from", Type: "integer", Desc: "1-based position to start at (default 1). Use with count to page through a large deck."}, {Name: "count", Type: "integer", Desc: "How many slides to return (default: all of them from `from`)."}},
			ReadOnly: true,
		},
		{
			Name:     "read_slide",
			Desc:     "One slide, described the way PowerPoint models it: placeholders by role (title, body, …) with their text, and non-placeholder shapes with position and size in points. Says what it could NOT read rather than leaving it out." + declare,
			Props:    withSlide(),
			Required: []string{},
			ReadOnly: true,
		},
		{
			Name: "find_shapes",
			Desc: "Shapes across the whole deck matching a filter — the way into a bulk change (\"every shape whose font is X\"). Returns identity and the matched values, not full slide detail." + declare,
			Props: withSlide(
				property{Name: "text", Type: "string", Desc: "Substring to look for in shape text (case-insensitive)."},
				property{Name: "name", Type: "string", Desc: "Substring to look for in the shape's name."},
				property{Name: "type", Type: "string", Desc: "Shape type to keep, e.g. TextBox, Picture, Table, Chart."},
				property{Name: "font", Type: "string", Desc: "Font name to keep."},
				property{Name: "placeholder", Type: "string", Desc: "Placeholder role to keep, e.g. title, body, subtitle."},
				property{Name: "limit", Type: "integer", Desc: "Maximum matches to return (default 50)."},
			),
			ReadOnly: true,
		},
		{
			Name:     "render_slide",
			Desc:     "A PNG of one slide as PowerPoint draws it. Expensive, and the council never sees pictures — call it only for a defect that numbers cannot show (overflow, overlap, contrast), not as a routine check." + declare,
			Props:    withSlide(),
			ReadOnly: true,
		},
		{
			Name: "export_slide_ooxml",
			Desc: "The slide's own OOXML, narrowed to a part — for what the object model stays silent about (chart data, speaker notes, animation). Reading it does not make it writable: this host cannot edit those in place." + declare,
			Props: withSlide(
				property{Name: "part", Type: "string", Desc: "Which part to return: slide (default), notes, chart, or list to see what the slide contains."},
				property{Name: "shape_id", Type: "string", Desc: "Narrow a chart or picture part to one shape."},
			),
			ReadOnly: true,
		},

		{
			Name:     "list_layouts",
			Desc:     "The layouts this deck offers, by master, with the placeholder roles each one carries. Read this before add_slide: layout names come from the deck's theme, so a guessed name is refused." + declare,
			Props:    []property{},
			ReadOnly: true,
		},

		{
			Name: "add_slide",
			Desc: "Add a slide — and, in the same call, put it where it belongs and fill its title and body. Filling a layout's placeholders is not the same as dropping text boxes at coordinates: placeholders follow the theme, so they stay right when the design changes. Prefer this over add_shape for the words that carry a slide.",
			Props: []property{
				{Name: "layout", Type: "string", Desc: "Layout name from list_layouts. Omit for the deck's default layout."},
				{Name: "at", Type: "integer", Desc: "1-based position for the new slide. Omit to put it at the end."},
				{Name: "title", Type: "string", Desc: "Text for the title placeholder, if the layout has one."},
				{Name: "body", Type: "string", Desc: "Text for the body/subtitle placeholder. Use \\n between bullet lines."},
			},
		},
		{
			Name:  "delete_slide",
			Desc:  "Remove one slide. Every slide after it shifts down one, and the result says so. Snapshot first if the person might want it back — nothing else brings it back.",
			Props: withSlide(),
		},
		{
			Name:  "duplicate_slide",
			Desc:  "Copy one slide, formatting and all, and put the copy right after it. The way to build a deck that looks consistent: make one slide well, duplicate it, then change the words.",
			Props: withSlide(),
		},

		{
			Name:     "set_text",
			Desc:     "Replace the text of one shape. The result carries the old text and the new one, because that pair is the only record of the change that reaches the council.",
			Props:    withSlide(property{Name: "shape_id", Type: "string", Desc: "The shape to write, from read_slide or find_shapes."}, property{Name: "text", Type: "string", Desc: "The new text. Use \\n for a line break."}),
			Required: []string{"shape_id", "text"},
		},
		{
			Name: "format_shape",
			Desc: "Change the look of one shape: font family, size, bold, italic, colour, alignment, fill.",
			Props: withSlide(
				property{Name: "shape_id", Type: "string", Desc: "The shape to format."},
				property{Name: "font", Type: "string", Desc: "Font family name."},
				property{Name: "size", Type: "number", Desc: "Font size in points."},
				property{Name: "bold", Type: "boolean", Desc: "Bold on or off."},
				property{Name: "italic", Type: "boolean", Desc: "Italic on or off."},
				property{Name: "color", Type: "string", Desc: "Text colour as #RRGGBB."},
				property{Name: "fill", Type: "string", Desc: "Fill colour as #RRGGBB, or \"none\" to clear it."},
				property{Name: "align", Type: "string", Desc: "Paragraph alignment: left, center, right or justify."},
			),
			Required: []string{"shape_id"},
		},
		{
			Name: "move_shape",
			Desc: "Move or resize one shape. Units are points, the same units read_slide reports.",
			Props: withSlide(
				property{Name: "shape_id", Type: "string", Desc: "The shape to move."},
				property{Name: "left", Type: "number", Desc: "New left edge in points."},
				property{Name: "top", Type: "number", Desc: "New top edge in points."},
				property{Name: "width", Type: "number", Desc: "New width in points."},
				property{Name: "height", Type: "number", Desc: "New height in points."},
			),
			Required: []string{"shape_id"},
		},
		{
			Name: "add_shape",
			Desc: "Add a shape to a slide — a text box or a geometric shape — at a position given in points.",
			Props: withSlide(
				property{Name: "kind", Type: "string", Desc: "textbox (default), rectangle, ellipse, line or roundRectangle."},
				property{Name: "text", Type: "string", Desc: "Text to put in it."},
				property{Name: "left", Type: "number", Desc: "Left edge in points."},
				property{Name: "top", Type: "number", Desc: "Top edge in points."},
				property{Name: "width", Type: "number", Desc: "Width in points."},
				property{Name: "height", Type: "number", Desc: "Height in points."},
			),
		},
		{
			Name:     "delete_shape",
			Desc:     "Delete one shape. There is no undo for a delete: the tag journal cannot restore what it was written on, so the snapshot is the only way back.",
			Props:    withSlide(property{Name: "shape_id", Type: "string", Desc: "The shape to delete."}),
			Required: []string{"shape_id"},
		},
		{
			Name:     "apply_layout",
			Desc:     "Put one slide on a different layout. Layout names come from list_slides — they are the deck's own vocabulary. This CHOOSES a layout; it never edits one.",
			Props:    withSlide(property{Name: "layout", Type: "string", Desc: "Layout name to apply."}),
			Required: []string{"layout"},
		},
		{
			Name:     "reorder_slide",
			Desc:     "Move a slide to another position. Every position after the move is a different slide than it was, so re-read before addressing anything by position.",
			Props:    withSlide(property{Name: "to", Type: "integer", Desc: "1-based position to move it to."}),
			Required: []string{"to"},
		},
		{
			Name:     "set_hyperlink",
			Desc:     "Set or clear the hyperlink on one shape.",
			Props:    withSlide(property{Name: "shape_id", Type: "string", Desc: "The shape to link."}, property{Name: "url", Type: "string", Desc: "The address. An empty string removes the link."}),
			Required: []string{"shape_id", "url"},
		},
		{
			Name: "add_table",
			Desc: "Add a table, with its values and formatting given at creation. This host can create a formatted table but cannot restyle an existing one, so put the formatting you want in this call.",
			Props: withSlide(
				property{Name: "rows", Type: "integer", Desc: "Number of rows."},
				property{Name: "columns", Type: "integer", Desc: "Number of columns."},
				property{Name: "values", Type: "array", Items: "array", Desc: "Row-major cell text: an array of rows, each an array of strings."},
				property{Name: "left", Type: "number", Desc: "Left edge in points."},
				property{Name: "top", Type: "number", Desc: "Top edge in points."},
				property{Name: "width", Type: "number", Desc: "Width in points."},
				property{Name: "height", Type: "number", Desc: "Height in points."},
				property{Name: "header_bold", Type: "boolean", Desc: "Make the first row bold at creation."},
				property{Name: "font", Type: "string", Desc: "Font family for every cell."},
				property{Name: "size", Type: "number", Desc: "Font size in points for every cell."},
			),
			Required: []string{"rows", "columns"},
		},
		{
			Name: "set_table_cells",
			Desc: "Write text into cells of an existing table. Text only — this host cannot change an existing table's borders, fills, fonts, merges or row and column counts, so do not ask for them here.",
			Props: withSlide(
				property{Name: "shape_id", Type: "string", Desc: "The table shape."},
				property{Name: "cells", Type: "array", Items: "object", Desc: "Cells to write: [{row, column, text}], row and column 0-based."},
			),
			Required: []string{"shape_id", "cells"},
		},

		{
			Name:     "snapshot_slide",
			Desc:     "Take a snapshot of one slide before changing it — the .pptx bytes of that slide, kept by the helper. It reads the deck and changes nothing, so it costs one call and no approval. Restoring is a separate tool.",
			Props:    withSlide(),
			ReadOnly: true,
		},
		{
			Name:     "restore_slide",
			Desc:     "Put a snapshot back. The restored slide is INSERTED and the original removed, so it comes back with a NEW id: the result carries that id, and anything still addressing the old one is stale.",
			Props:    withSlide(property{Name: "snapshot", Type: "string", Desc: "Snapshot id from snapshot_slide."}),
			Required: []string{"snapshot"},
		},

		{
			Name: "advise",
			Desc: "Pin advice in the task pane without touching the deck: what to change and why, optionally pointing at a slide and shapes. Clicking an item takes the person there. Advice is what you would SAY, not what you did — it never counts as work finished.",
			Props: []property{
				{Name: "items", Type: "array", Items: "object", Desc: "[{message, why, slide_id, shape_ids}] — message and why are required, slide_id and shape_ids optional; an item with no slide is listed but cannot be pointed at."},
			},
			Required: []string{"items"},
			ReadOnly: true,
		},
		{
			Name:     "clear_advice",
			Desc:     "Take the pinned advice down. Nothing to undo: it was never in the deck.",
			Props:    []property{},
			ReadOnly: true,
		},
	}
}

// documentProp 는 모든 도구가 같이 받는 칸이다(§4.4 ④ — MCP 에 scope 개념이 없으니 인자로 받는다).
var documentProp = property{
	Name: "document",
	Type: "string",
	Desc: "Which open deck to act on, as returned by an earlier call. Omit for the deck the person is looking at now.",
}

// schemaOf 는 도구 하나의 `inputSchema` 를 짓는다.
//
// **`properties` 와 `required` 를 반드시 적는다**(§4.3). magi 는 디스패치 직전에 보낸 키를
// 스키마와 맞춰 보는데, `properties` 를 못 읽으면 그 검사가 **아무 의견도 안 낸다.** 게다가
// 빈 스키마를 보내면 매니저가 `{"type":"object"}` 로 채워 넣으므로 **모양은 멀쩡한데 검사만
// 꺼진 스키마**가 만들어진다. 증상은 「스키마를 덜 적었다」가 아니라 「모델이 묻지 않은 질문에
// 답을 받았다」로 나타난다.
func schemaOf(t tool) json.RawMessage {
	props := map[string]any{}
	for _, p := range append([]property{documentProp}, t.Props...) {
		entry := map[string]any{"type": p.Type, "description": p.Desc}
		if p.Items != "" {
			entry["items"] = map[string]any{"type": p.Items}
		}
		props[p.Name] = entry
	}
	req := t.Required
	if req == nil {
		req = []string{}
	}
	body := map[string]any{
		"type":       "object",
		"properties": props,
		"required":   req,
		// 모르는 키는 **서버가 거절한다**(§4.3). magi 는 안 막기로 정했고 — 결과 뒤에
		// `[ignored arguments]` 한 줄이 붙을 뿐이다 — 그 자리를 우리가 막는 것이다. 조용히
		// 무시하면 §2.3 이 최악이라고 적은 실패가 인자 한 칸에서 난다.
		"additionalProperties": false,
	}
	b, err := json.Marshal(body)
	if err != nil { // 우리 손으로 지은 맵이라 여기 오면 코드 결함이다
		panic("ppt: schema marshal: " + err.Error())
	}
	return b
}

// allowRules 는 오퍼레이터가 config 에 적을 허용 규칙이다(§4.4 ②·§6).
//
// 기준은 「덱을 고치는가」이지 읽기/쓰기라는 제목이 아니다. 규칙의 **도구 자리에는 와일드카드가
// 없어서**(`policy.go` 의 `matches` 가 문자열 비교다) 한 줄에 한 도구고, 그래서 이 목록은 도구를
// 하나 더할 때마다 같이 자란다. 산문으로 두면 안 자란다 — 그래서 코드가 만든다.
//
// 괄호 안은 `(**)` 다. 우리 도구에는 `path` 인자가 없어서 `subjectOf` 가 **빈 문자열**을 주고,
// 빈 주어에 걸리는 글롭이라야 한다.
func allowRules() []string {
	var out []string
	for _, t := range catalogue() {
		if t.ReadOnly {
			out = append(out, fmt.Sprintf("mcp__%s__%s(**)", ServerName, t.Name))
		}
	}
	sort.Strings(out)
	return out
}

// AllowRulesTOML 은 사람에게 그대로 붙여 넣으라고 내주는 모양이다.
func AllowRulesTOML() string {
	var b strings.Builder
	b.WriteString("# magi-ppt: 덱을 고치지 않는 도구만 허용한다(DESIGN.md §6).\n")
	b.WriteString("# 쓰기 도구는 일부러 빠져 있다 — 덱을 고치는 것은 물어야 하는 일이 맞다.\n")
	b.WriteString("allow = [\n")
	for _, r := range allowRules() {
		b.WriteString("  \"" + r + "\",\n")
	}
	b.WriteString("]\n")
	return b.String()
}
