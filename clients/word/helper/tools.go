package main

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
var fromProp = property{Name: "from", Type: "integer", Desc: "First paragraph, 1-based (from list_paragraphs). Omit for the whole body."}
var toProp = property{Name: "to", Type: "integer", Desc: "Last paragraph, inclusive. Omit for just `from`."}
var paraProp = property{Name: "paragraph", Type: "integer", Desc: "A paragraph number, 1-based (from list_paragraphs).", Also: []string{"para"}}
var tableProp = property{Name: "table", Type: "integer", Desc: "Table number, 1-based in body order (from list_paragraphs / read_document)."}

func withFromTo(rest ...property) []property {
	return append([]property{fromProp, toProp}, rest...)
}

func catalogue(hasCouncil bool) []tool {
	declare := ""
	if hasCouncil {
		declare = " A turn that called any tool must end by declaring it finished with " +
			"council{complete:true}, even a read-only one: otherwise the turn lands UNVERIFIED."
	}
	return []tool{
		{
			Name: "list_paragraphs",
			Desc: "A DOCUMENT IS ALREADY OPEN IN WORD AND THESE TOOLS ARE ATTACHED TO IT. You do not " +
				"create, open or upload a document, and there is no tool that does — the person is looking at " +
				"theirs right now and every tool here edits that one. Never ask them to provide, upload or " +
				"attach a file. If a call fails, read the error and RETRY IT. Do not fall back to building a " +
				"document any other way: a .docx you write with a shell, python-docx or COM automation is a " +
				"FILE NOBODY IS LOOKING AT, and the person ends up with an unchanged document on screen plus " +
				"scripts they did not ask for. If these tools truly cannot do it, say so and stop. " +
				"THE DOCUMENT'S OUTLINE: every body paragraph with its 1-based number, style (Heading 1, Normal, " +
				"List Paragraph…), list level, whether it sits in a table (and which), and the first ~80 " +
				"characters. Read this first; it is one call and it tells you where everything is. Long " +
				"documents are paged (from/to/max)." + declare,
			Props:    withFromTo(property{Name: "max", Type: "integer", Desc: "Rows to return (default 200)."}),
			ReadOnly: true,
		},
		{
			Name: "read_paragraphs",
			Desc: "Full text of paragraphs from..to, one entry per paragraph, with style, alignment, list level and " +
				"the font of the first run (name, size, bold, italic, color). Use after list_paragraphs to read " +
				"the passage you are about to change. Cheap for a page, expensive for a book — page it." + declare,
			Props:    withFromTo(property{Name: "max_chars", Type: "integer", Desc: "Cap per paragraph (default 4000)."}),
			ReadOnly: true,
		},
		{
			Name: "read_document",
			Desc: "The document as a whole: title/subject/author/keywords, counts (paragraphs, tables, sections, " +
				"inline pictures, comments where the host can count them), each section's header and footer text, " +
				"the change-tracking mode, and the requirement sets this host supports (WordApi 1.3 is Word " +
				"2019/2021; comments, bookmarks and tracked changes need 1.4+, i.e. Microsoft 365 / 2024)." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "find",
			Desc: "Search the body for text. Answers paragraph numbers and the matched text with a little context, " +
				"so you can address the passage with from/to or replace it with replace_all. Case-insensitive " +
				"unless match_case; whole-word optional. Wildcards are OFF." + declare,
			Props: []property{
				property{Name: "text", Type: "string", Desc: "What to look for. Required."},
				property{Name: "match_case", Type: "boolean"}, property{Name: "whole_word", Type: "boolean"},
				property{Name: "limit", Type: "integer", Desc: "Max hits (default 50)."},
			},
			Required: []string{"text"},
			ReadOnly: true,
		},
		{
			Name: "read_table",
			Desc: "One table: its number, size, header row flag, style, and every cell's text as a 2-D array " +
				"(rows of cells). Cells that were merged read as empty in the covered positions." + declare,
			Props:    []property{tableProp, property{Name: "max_rows", Type: "integer", Desc: "Rows to return (default 200)."}},
			Required: []string{"table"},
			ReadOnly: true,
		},
		{
			Name: "read_html",
			Desc: "The passage from..to as HTML, as Word renders it. This is the LOOK — the nearest thing to seeing " +
				"the page: it carries bold, sizes, colors, lists and tables where read_paragraphs gives you " +
				"numbers. Word cannot render a page to an image, so this is what you check formatting with. " +
				"Big — ask for a few paragraphs at a time." + declare,
			Props:    withFromTo(property{Name: "max_chars", Type: "integer", Desc: "Cap on the HTML (default 20000)."}),
			ReadOnly: true,
		},
		{
			Name: "read_comments",
			Desc: "Every comment thread on the body: id, author, date, the commented text, the comment, its " +
				"replies, and whether it is resolved. Needs WordApi 1.4 (Microsoft 365 / 2024) — on 2019/2021 " +
				"this refuses by name." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "read_tracked_changes",
			Desc: "Pending tracked changes (insertions, deletions, formatting) with author, date and text, plus the " +
				"tracking mode. Needs WordApi 1.6 (Microsoft 365 / 2024)." + declare,
			Props:    withFromTo(property{Name: "limit", Type: "integer", Desc: "Max changes (default 100)."}),
			ReadOnly: true,
		},
		{
			Name: "describe_style",
			Desc: "How this document is dressed: the paragraph styles in use with counts, the body font (name, size), " +
				"heading fonts, and the first few heading texts. Read it before adding to a document you did not " +
				"write, so new text matches — use the SAME style names, not ad-hoc bold." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "snapshot_paragraphs",
			Desc: "Take a snapshot of paragraphs from..to (their OOXML) and get an id, so restore_paragraphs can put " +
				"them back exactly. Take one before a replace_all or delete you are not sure of. Snapshots live in " +
				"the task pane's memory only — they do not survive the pane closing." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "read_tags",
			Desc: "Notes earlier turns left in the document (custom document properties under MAGI.*): what was " +
				"decided, what was left. Read them before continuing someone else's work." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "read_suggestions",
			Desc: "Suggestions pinned to this document by suggest (see it) — what they propose, where, and whether " +
				"the pane can apply them. Needs WordApi 1.4 (settings)." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "advise",
			Desc: "Pin advice to the task pane for the person — things you noticed but did NOT change (an " +
				"inconsistent heading, a table without a header row). Each item names a paragraph so the " +
				"pane can jump to it. Changes nothing in the document." + declare,
			Props: []property{property{Name: "items", Type: "array", Items: "object",
				Desc: "[{message, why?, paragraph?}] — paragraph is 1-based; omit it if the advice is about the whole document."}},
			Required: []string{"items"},
			ReadOnly: true,
		},
		{
			Name:     "clear_advice",
			Desc:     "Remove the pinned advice from the task pane." + declare,
			Props:    []property{},
			ReadOnly: true,
		},

		// ── 쓰기 ──
		{
			Name: "insert_paragraphs",
			Desc: "Insert paragraphs. `lines` is an array of strings, one paragraph each (an empty string is a " +
				"blank paragraph); `style` applies to all of them (\"Heading 1\", \"Normal\", \"List Paragraph\" — " +
				"the document's own names, see describe_style). Where: after/before a paragraph number, or at the " +
				"start/end of the body. Answers the new paragraphs' numbers. This is also how you write a whole " +
				"section: headings and body in one call, in order.",
			Props: []property{
				property{Name: "lines", Type: "array", Items: "string", Desc: "Paragraph texts in order. Required."},
				property{Name: "after", Type: "integer", Desc: "Insert after this paragraph number (default: end of body)."},
				property{Name: "before", Type: "integer", Desc: "Insert before this paragraph number."},
				property{Name: "at", Type: "string", Desc: "\"start\" or \"end\" of the body when neither after nor before is given.", Enum: atWhere},
				property{Name: "style", Type: "string", Desc: "Style for all lines: a built-in name (\"Heading2\", \"Normal\", \"ListParagraph\" — language-independent) or the document's own style name."},
			},
			Required: []string{"lines"},
		},
		{
			Name: "replace_paragraph",
			Desc: "Replace the text of one paragraph, keeping its style and paragraph formatting. Character " +
				"formatting inside it (a bold word) is lost — use format_text afterwards if you need it back.",
			Props:    []property{paraProp, property{Name: "text", Type: "string", Desc: "The new text. Required.", Also: []string{"content"}}},
			Required: []string{"paragraph", "text"},
		},
		{
			Name: "delete_paragraphs",
			Desc: "Delete paragraphs from..to. There is no undo except a snapshot you took first — snapshot_paragraphs " +
				"if you are not sure. Refuses to delete the only paragraph in the body.",
			Props:    withFromTo(),
			Required: []string{"from"},
		},
		{
			Name: "set_style",
			Desc: "Apply a paragraph style to paragraphs from..to: the document's own style names (\"Heading 1\", " +
				"\"Title\", \"Quote\", \"List Paragraph\", \"Normal\") or a built-in name from `builtin`. Styles " +
				"are how headings become headings — the navigation pane, the table of contents and the outline " +
				"all read them; bold 16pt text is not a heading.",
			Props: withFromTo(
				property{Name: "style", Type: "string", Desc: "Style name as the document shows it (localized names work); a built-in name like \"Heading2\" is taken as builtin."},
				property{Name: "builtin", Type: "string", Desc: "Built-in style, language-independent.", Enum: builtinStyles},
			),
			Required: []string{"from"},
		},
		{
			Name: "format_text",
			Desc: "Character formatting for a passage: bold, italic, underline, strike, size (pt), color (#RRGGBB), " +
				"highlight (color name or \"none\"), font name. Give paragraphs from..to, or `text` to format only " +
				"the matches of that text inside them. Only the fields you pass change. Prefer set_style for " +
				"whole-paragraph looks.",
			Props: withFromTo(
				property{Name: "text", Type: "string", Desc: "Format only this text where it occurs in from..to (case-sensitive)."},
				property{Name: "bold", Type: "boolean"}, property{Name: "italic", Type: "boolean"}, property{Name: "strike", Type: "boolean"},
				property{Name: "underline", Type: "string", Desc: "None, Single, Double, Dotted, Dashed, Wave, Thick.", Enum: underlines},
				property{Name: "size", Type: "number", Desc: "Points."},
				property{Name: "color", Type: "string", Desc: "#RRGGBB.", Also: []string{"font_color"}},
				property{Name: "highlight", Type: "string", Desc: "Yellow, BrightGreen, Turquoise, Pink, Blue, Red, DarkBlue, Teal, Green, Violet, DarkRed, DarkYellow, Gray50, Gray25, Black, or none.", Enum: highlights},
				property{Name: "font", Type: "string", Desc: "Font name, e.g. \"맑은 고딕\"."},
				property{Name: "clear", Type: "boolean", Desc: "Remove direct character formatting first."},
			),
			Required: []string{"from"},
		},
		{
			Name: "format_paragraph",
			Desc: "Paragraph formatting for from..to: alignment, space before/after (pt), line spacing (pt, e.g. 18), " +
				"first-line and left indents (pt). Only the fields you pass change.",
			Props: withFromTo(
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right, Justified.", Enum: aligns},
				property{Name: "space_before", Type: "number"}, property{Name: "space_after", Type: "number"},
				property{Name: "line_spacing", Type: "number", Desc: "Points between lines (12 = single for 12pt text; 18 = 1.5)."},
				property{Name: "first_line_indent", Type: "number"}, property{Name: "left_indent", Type: "number"}, property{Name: "right_indent", Type: "number"},
			),
			Required: []string{"from"},
		},
		{
			Name: "insert_table",
			Desc: "Insert a table after a paragraph (or at the end): `values` is a 2-D array (rows of cells, strings); " +
				"the first row is the header when has_header (default true). Optional built-in style. Answers the " +
				"table number. Put a caption paragraph before it yourself if the document uses them.",
			Props: []property{
				property{Name: "values", Type: "array", Items: "array", Desc: "Rows of cell texts, e.g. [[\"분기\",\"매출\"],[\"1분기\",\"12,000\"]]. Required."},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "has_header", Type: "boolean", Desc: "First row is a header (default true)."},
				property{Name: "table_style", Type: "string", Desc: "Built-in table style, e.g. GridTable4_Accent1, PlainTable1, TableGrid.", Enum: tableStyles},
			},
			Required: []string{"values"},
		},
		{
			Name: "set_table_cells",
			Desc: "Write individual cells of a table by 0-based row and column: [{row, column, value}]. Rows and " +
				"columns are counted including the header row.",
			Props:    []property{tableProp, property{Name: "cells", Type: "array", Items: "object", Desc: "[{row, column, value}] — 0-based. Required."}},
			Required: []string{"table", "cells"},
		},
		{
			Name: "add_table_rows",
			Desc: "Append rows to a table (at the end unless at=\"start\"): `rows` is a 2-D array with one entry per " +
				"column. Shorter rows are padded with empty cells.",
			Props: []property{tableProp,
				property{Name: "rows", Type: "array", Items: "array", Desc: "Rows of cell texts. Required."},
				property{Name: "at", Type: "string", Desc: "\"end\" (default) or \"start\".", Enum: atWhere}},
			Required: []string{"table", "rows"},
		},
		{
			Name:     "delete_table",
			Desc:     "Delete a whole table. No undo — snapshot the paragraphs around it first if unsure.",
			Props:    []property{tableProp},
			Required: []string{"table"},
		},
		{
			Name: "format_table",
			Desc: "Table looks: built-in style, header row on/off, banded rows/columns, alignment of the table on the " +
				"page, and optional column widths (pt). Only the fields you pass change.",
			Props: []property{tableProp,
				property{Name: "table_style", Type: "string", Enum: tableStyles},
				property{Name: "header_row", Type: "boolean"}, property{Name: "banded_rows", Type: "boolean"}, property{Name: "banded_columns", Type: "boolean"},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right.", Enum: aligns},
				property{Name: "widths", Type: "array", Items: "number", Desc: "Column widths in points, left to right."}},
			Required: []string{"table"},
		},
		{
			Name: "insert_list",
			Desc: "Insert a bulleted or numbered list after a paragraph (or at the end): one item per entry of " +
				"`items`; `levels` (same length, 0-based) nests. Answers the new paragraphs' numbers. To turn " +
				"existing paragraphs into a list use set_list.",
			Props: []property{
				property{Name: "items", Type: "array", Items: "string", Desc: "Item texts in order. Required."},
				property{Name: "kind", Type: "string", Desc: "bulleted (default) or numbered.", Enum: listKinds},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "levels", Type: "array", Items: "integer", Desc: "Nesting level per item, 0-based."},
			},
			Required: []string{"items"},
		},
		{
			Name: "set_list",
			Desc: "Make paragraphs from..to a list (kind bulleted/numbered), change their level, or take them out of " +
				"their list (detach). Paragraphs already in a list keep it unless kind is given.",
			Props: withFromTo(
				property{Name: "kind", Type: "string", Enum: listKinds},
				property{Name: "level", Type: "integer", Desc: "0-based list level."},
				property{Name: "detach", Type: "boolean", Desc: "Remove from the list."}),
			Required: []string{"from"},
		},
		{
			Name: "insert_image",
			Desc: "Put a picture from the person's own computer into the document, inline, after a paragraph. Give " +
				"the FILE PATH — never base64: the helper reads the file itself and refuses anything that is " +
				"not a real PNG/JPEG/GIF/BMP.",
			Props: []property{
				property{Name: "path", Type: "string", Desc: "Where the picture is on this machine. Required."},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "width", Type: "number", Desc: "Points. Omit to keep the natural size (capped to the page width)."},
				property{Name: "alt", Type: "string", Desc: "Alt text. Set it."},
			},
			Required: []string{"path"},
		},
		{
			Name:     "insert_break",
			Desc:     "Insert a page break, a section break (next page) or a line break after a paragraph.",
			Props:    []property{paraProp, property{Name: "kind", Type: "string", Desc: "page (default), section, line.", Enum: breakKinds}},
			Required: []string{"paragraph"},
		},
		{
			Name: "set_header_footer",
			Desc: "Set the header or footer text of a section (default: first section, primary). Replaces what is " +
				"there. Word keeps page-number fields; to keep one, say so in `keep_fields`.",
			Props: []property{
				property{Name: "which", Type: "string", Desc: "header or footer. Required.", Enum: headerFooter},
				property{Name: "text", Type: "string", Desc: "The new text. Empty string clears it. Required."},
				property{Name: "section", Type: "integer", Desc: "Section number, 1-based (default 1)."},
				property{Name: "kind", Type: "string", Desc: "Primary (default), FirstPage, EvenPages.", Enum: headerKinds},
				property{Name: "align", Type: "string", Enum: aligns},
			},
			Required: []string{"which", "text"},
		},
		{
			Name: "set_hyperlink",
			Desc: "Put a hyperlink on a passage: the matches of `text` inside paragraphs from..to (or the whole " +
				"paragraphs when text is omitted). Omit url to remove the link.",
			Props:    withFromTo(property{Name: "text", Type: "string"}, property{Name: "url", Type: "string", Desc: "Omit to remove."}),
			Required: []string{"from"},
		},
		{
			Name: "replace_all",
			Desc: "Find and replace across the body (or from..to): every match of `find` becomes `replace`. " +
				"Case-insensitive unless match_case; whole-word optional. Answers how many changed and where. " +
				"Take a snapshot first if the words are common.",
			Props: withFromTo(
				property{Name: "find", Type: "string", Desc: "Required."}, property{Name: "replace", Type: "string", Desc: "Required (empty string deletes the matches)."},
				property{Name: "match_case", Type: "boolean"}, property{Name: "whole_word", Type: "boolean"},
				property{Name: "limit", Type: "integer", Desc: "Stop after this many (default: all)."}),
			Required: []string{"find", "replace"},
		},
		{
			Name: "add_comment",
			Desc: "Add a comment on a passage: the first match of `text` inside paragraphs from..to, or the whole " +
				"paragraph `from` when text is omitted. Needs WordApi 1.4 (Microsoft 365 / 2024).",
			Props:    withFromTo(property{Name: "text", Type: "string", Desc: "Anchor text inside the paragraphs."}, property{Name: "comment", Type: "string", Desc: "The comment. Required."}),
			Required: []string{"from", "comment"},
		},
		{
			Name:     "reply_comment",
			Desc:     "Reply to a comment thread by id (from read_comments). Needs WordApi 1.4.",
			Props:    []property{property{Name: "id", Type: "string", Desc: "Comment id. Required."}, property{Name: "text", Type: "string", Desc: "Reply. Required."}},
			Required: []string{"id", "text"},
		},
		{
			Name:     "resolve_comment",
			Desc:     "Mark a comment thread resolved (or reopen it), or delete it with delete:true. Needs WordApi 1.4.",
			Props:    []property{property{Name: "id", Type: "string", Desc: "Comment id. Required."}, property{Name: "resolved", Type: "boolean", Desc: "Default true."}, property{Name: "delete", Type: "boolean"}},
			Required: []string{"id"},
		},
		{
			Name:     "add_bookmark",
			Desc:     "Put a named bookmark on paragraphs from..to (a hidden anchor you or a hyperlink can jump to). Needs WordApi 1.4.",
			Props:    withFromTo(property{Name: "name", Type: "string", Desc: "Bookmark name — letters, digits, underscore; starts with a letter. Required."}),
			Required: []string{"from", "name"},
		},
		{
			Name:     "delete_bookmark",
			Desc:     "Remove a bookmark by name. The text stays. Needs WordApi 1.4.",
			Props:    []property{property{Name: "name", Type: "string", Desc: "Required."}},
			Required: []string{"name"},
		},
		{
			Name: "set_track_changes",
			Desc: "Turn change tracking Off, TrackAll, or TrackMineOnly. With tracking on, every edit these tools " +
				"make shows as a tracked change the person can accept or reject — the right mode when editing " +
				"someone else's document. Needs WordApi 1.4.",
			Props:    []property{property{Name: "mode", Type: "string", Desc: "Off, TrackAll, TrackMineOnly. Required.", Enum: trackModes}},
			Required: []string{"mode"},
		},
		{
			Name:     "review_changes",
			Desc:     "Accept or reject tracked changes — all of them, or only those in paragraphs from..to. Needs WordApi 1.6.",
			Props:    withFromTo(property{Name: "what", Type: "string", Desc: "accept or reject. Required.", Enum: reviewWhats}),
			Required: []string{"what"},
		},
		{
			Name: "set_properties",
			Desc: "Document properties: title, subject, author, keywords, comments, category. Only the fields you " +
				"pass change.",
			Props: []property{
				property{Name: "title", Type: "string"}, property{Name: "subject", Type: "string"}, property{Name: "author", Type: "string"},
				property{Name: "keywords", Type: "string"}, property{Name: "comments", Type: "string"}, property{Name: "category", Type: "string"}},
		},
		{
			Name:     "restore_paragraphs",
			Desc:     "Put back a snapshot from snapshot_paragraphs, replacing the paragraphs at the snapshot's from..to as they are NOW. If paragraphs were inserted or deleted above it since, re-check the numbers first.",
			Props:    []property{property{Name: "snapshot", Type: "string", Desc: "Snapshot id from snapshot_paragraphs. Required."}},
			Required: []string{"snapshot"},
		},
		{
			Name: "set_tag",
			Desc: "Leave a note in the document for later turns (a custom document property under MAGI.*): a decision, " +
				"what is left. Values are capped at 255 characters by Word. Keys starting with MAGI.FIX. are " +
				"reserved for suggestions.",
			Props:    []property{property{Name: "key", Type: "string", Desc: "Required."}, property{Name: "value", Type: "string", Desc: "Required; empty string deletes the note."}},
			Required: []string{"key", "value"},
		},
		{
			Name: "suggest",
			Desc: "Propose a change WITHOUT making it — the person sees a card in the task pane and applies it with one " +
				"click. `fix` is the exact call to make: {tool, args}; only replace_paragraph, format_text, " +
				"format_paragraph, set_style, replace_all and insert_paragraphs can be applied from a card. Needs " +
				"WordApi 1.4 (settings).",
			Props: []property{
				property{Name: "what", Type: "string", Desc: "The suggestion, one sentence. Required."},
				property{Name: "why", Type: "string", Desc: "Why it would be better."},
				property{Name: "paragraph", Type: "integer", Desc: "Where it applies, 1-based."},
				property{Name: "fix", Type: "object", Desc: "{tool, args} — the call Apply should make."},
			},
			Required: []string{"what"},
		},
		{
			Name:     "drop_suggestion",
			Desc:     "Remove a pinned suggestion by key (from read_suggestions) without applying it. Needs WordApi 1.4.",
			Props:    []property{property{Name: "key", Type: "string", Desc: "Required."}},
			Required: []string{"key"},
		},
	}
}

var documentProp = property{
	Name: "document",
	Type: "string",
	Desc: "Omit it. This conversation is bound to one document and every call goes to that document. Only a hub-wide conversation (no document of its own) names one here, with a key from an earlier answer.",
	Also: []string{"doc"},
}

// schemaOf 는 도구 하나의 `inputSchema` 를 짓는다. `properties` 와 `required` 를 반드시 적는다 —
// magi 는 디스패치 직전에 보낸 키를 스키마와 맞춰 보는데, `properties` 를 못 읽으면 그 검사가 아무
// 의견도 안 낸다(파워포인트 판 §4.3).
func schemaOf(t tool) json.RawMessage {
	props := map[string]any{}
	for _, p := range append([]property{documentProp}, t.Props...) {
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
func allowRules() []string {
	var out []string
	for _, t := range catalogue(false) {
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
	b.WriteString("# magi-word: 통합 문서를 고치지 않는 도구만 허용한다.\n")
	b.WriteString("# 쓰기 도구는 일부러 빠져 있다 — 통합 문서를 고치는 것은 물어야 하는 일이 맞다.\n")
	b.WriteString("allow = [\n")
	for _, r := range allowRules() {
		b.WriteString("  \"" + r + "\",\n")
	}
	b.WriteString("]\n")
	return b.String()
}
