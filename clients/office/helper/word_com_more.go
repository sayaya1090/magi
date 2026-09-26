package office

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 셋째 묶음: 도형 넷 · 필드 · 표 병합. 창(WordHand.js)의 인자·답과 같게 둔다 — word_com.go 머리 주석.

// wordShape 는 떠 있는 도형 하나 — list_shapes 가 창과 같은 모양으로 낸다.
type wordShape struct {
	ID                       int
	Name, Type, Text         string
	Left, Top, Width, Height float64
}

// wordShapeSpec 은 넣을 도형. Mso 가 0 이면 글 상자다.
type wordShapeSpec struct {
	Mso                      int
	Text, Name, Fill, Line   string
	Left, Top, Width, Height float64
}

// wordShapeEdit 은 고칠 것 — 비어 있는 칸은 안 건드린다.
type wordShapeEdit struct {
	Text                     *string
	Fill, Line, NewName      string
	Left, Top, Width, Height *float64
}

// wordTableEdit 는 표 하나에 할 일. 순서는 창과 같다: 병합 → 열 추가 → 열 삭제 → 행 삭제.
type wordTableEdit struct {
	Merge     *[4]int // from_row, from_column, to_row, to_column (0부터, 끝 포함)
	AddAt     string  // "" 이면 안 넣는다 · end · start · 뒤에 넣을 열 번호(0부터)
	AddCount  int
	AddValues [][]string // 새 열마다 위→아래
	DelCols   []int
	DelRows   []int
}

// wordFieldPiece 는 필드 줄의 조각 — 글이거나 필드다.
type wordFieldPiece struct {
	Text  *string
	Type  int // WdFieldType
	Code  string
	Name  string
	Ko    string
	Given string
}

// wordFieldWhere 는 필드 줄이 설 자리 — Which 가 있으면 머리글/바닥글, 없으면 본문.
type wordFieldWhere struct {
	Which, Kind   string
	Section       int
	After, Before int
	At            string
}

// wordGeometry 는 창의 도형 이름(WordHand.js #GEOMETRY) → MsoAutoShapeType.
var wordGeometry = map[string]int{
	"rectangle": 1, "rounded_rectangle": 5, "ellipse": 9, "triangle": 7, "diamond": 4, "hexagon": 10,
	"star": 92, "right_arrow": 33, "left_arrow": 34, "up_arrow": 35, "down_arrow": 36,
}

// wordFieldTypes 는 창의 필드 이름(handCore.js FIELD_TYPES) → WdFieldType 과 사람 말.
var wordFieldTypes = map[string]struct {
	id int
	ko string
}{
	"toc": {13, "목차"}, "page": {33, "쪽 번호"}, "num_pages": {26, "전체 쪽수"}, "pages": {26, "전체 쪽수"},
	"date": {31, "날짜"}, "time": {32, "시각"}, "title": {15, "제목"}, "author": {17, "작성자"},
	"file_name": {29, "파일 이름"}, "file": {29, "파일 이름"},
}

var wordFieldSlot = regexp.MustCompile(`\{(page|pages|num_pages|date|time|title|author|file|file_name|toc)\}`)

// wordFieldPieces 는 창의 fieldPieces(handCore.js) 와 같은 규칙으로 조각을 낸다.
func wordFieldPieces(args map[string]any) ([]wordFieldPiece, string, error) {
	field, template := wcStr(args, "field"), wcStr(args, "template")
	if field == "" && template == "" {
		return nil, "", fmt.Errorf(`field 나 template 이 있어야 합니다 — 예: field "toc", template "{page} / {pages}"`)
	}
	codeOf := func(name string) string {
		if name != "toc" {
			return ""
		}
		levels := wcStr(args, "levels")
		if levels == "" {
			levels = "1-3"
		}
		return fmt.Sprintf(` \o "%s" \h \z \u `, levels)
	}
	var pieces []wordFieldPiece
	if template != "" {
		last := 0
		for _, m := range wordFieldSlot.FindAllStringSubmatchIndex(template, -1) {
			if m[0] > last {
				t := template[last:m[0]]
				pieces = append(pieces, wordFieldPiece{Text: &t})
			}
			name := template[m[2]:m[3]]
			ft := wordFieldTypes[name]
			pieces = append(pieces, wordFieldPiece{Type: ft.id, Code: codeOf(name), Name: name, Ko: ft.ko})
			last = m[1]
		}
		if last < len(template) {
			t := template[last:]
			pieces = append(pieces, wordFieldPiece{Text: &t})
		}
		has := false
		for _, p := range pieces {
			if p.Text == nil {
				has = true
			}
		}
		if !has {
			return nil, "", fmt.Errorf("template 에 필드 자리가 없습니다 — {page} {pages} {date} {time} {title} {author} {file} 중 하나를 넣으세요: %s", template)
		}
	} else {
		ft, ok := wordFieldTypes[field]
		if !ok {
			return nil, "", fmt.Errorf("모르는 필드입니다: %s — toc, page, num_pages, date, time, title, author, file_name", field)
		}
		pieces = append(pieces, wordFieldPiece{Type: ft.id, Code: codeOf(field), Name: field, Ko: ft.ko})
	}
	var names []string
	for _, p := range pieces {
		if p.Text == nil {
			names = append(names, p.Ko)
		}
	}
	said := strings.Join(names, "·") + " 필드를 넣었습니다"
	if template != "" {
		said += " — 「" + template + "」"
	}
	return pieces, said, nil
}

func wcHexOrNone(args map[string]any, k string) (string, error) {
	v := wcStr(args, k)
	if v == "" || v == "none" || wordHexColor.MatchString(v) {
		return v, nil
	}
	return "", fmt.Errorf("%s 는 #RRGGBB 나 none 입니다 — %s", k, v)
}

func wcInts(v any) ([]int, bool) {
	xs, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(xs))
	for _, x := range xs {
		out = append(out, intOf(x))
	}
	return out, true
}

// wordComRunMore 는 셋째 묶음의 도구 — 모르는 이름이면 ok=false.
func wordComRunMore(d wordDoc, name string, args map[string]any) (res map[string]any, changed []string, ok bool, err error) {
	switch name {
	case "list_shapes":
		ss, err := d.Shapes()
		if err != nil {
			return nil, nil, true, err
		}
		rows := make([]any, 0, len(ss))
		for _, s := range ss {
			rows = append(rows, map[string]any{"id": s.ID, "name": s.Name, "type": s.Type, "left": int(s.Left + 0.5), "top": int(s.Top + 0.5),
				"width": int(s.Width + 0.5), "height": int(s.Height + 0.5), "text": wcClip(s.Text, 80)})
		}
		return map[string]any{"count": len(rows), "shapes": rows}, nil, true, nil

	case "insert_shape":
		kind := wcStr(args, "shape")
		if kind == "" {
			kind = "textbox"
		}
		spec := wordShapeSpec{Text: wcStr(args, "text"), Name: wcStr(args, "name"), Left: 72, Top: 72, Width: 200, Height: 60}
		if kind != "textbox" {
			mso, known := wordGeometry[kind]
			if !known {
				names := make([]string, 0, len(wordGeometry))
				for k := range wordGeometry {
					names = append(names, k)
				}
				sort.Strings(names)
				return nil, nil, true, fmt.Errorf("shape 는 textbox, %s 중 하나 — %s", strings.Join(names, ", "), kind)
			}
			spec.Mso = mso
		}
		for k, dst := range map[string]*float64{"left": &spec.Left, "top": &spec.Top, "width": &spec.Width, "height": &spec.Height} {
			if v, ok := wcNum(args, k); ok {
				*dst = v
			}
		}
		if spec.Fill, err = wcHexOrNone(args, "fill"); err != nil {
			return nil, nil, true, err
		}
		if spec.Line, err = wcHexOrNone(args, "line_color"); err != nil {
			return nil, nil, true, err
		}
		para := wcInt(args, "paragraph")
		if para == 0 {
			para = 1
		}
		if err := wcParaInRange(d, para); err != nil {
			return nil, nil, true, err
		}
		id, got, err := d.AddShape(para, spec)
		if err != nil {
			return nil, nil, true, err
		}
		what := "글 상자"
		if kind != "textbox" {
			what = "도형(" + kind + ")"
		}
		label := ""
		if got != "" {
			label = " 「" + got + "」"
		}
		return map[string]any{"id": id, "name": got, "shape": kind, "left": spec.Left, "top": spec.Top, "width": spec.Width, "height": spec.Height},
			[]string{fmt.Sprintf("%s를 넣었습니다 — id %d%s, (%g, %g) %g×%g", what, id, label, spec.Left, spec.Top, spec.Width, spec.Height)}, true, nil

	case "format_shape":
		var e wordShapeEdit
		var words []string
		if v, has := args["text"].(string); has {
			e.Text = &v
			words = append(words, "글 「"+wcClip(v, 30)+"」")
		}
		if e.Fill, err = wcHexOrNone(args, "fill"); err != nil {
			return nil, nil, true, err
		}
		if e.Fill != "" {
			words = append(words, "채우기 "+e.Fill)
		}
		if e.Line, err = wcHexOrNone(args, "line_color"); err != nil {
			return nil, nil, true, err
		}
		if e.Line != "" {
			words = append(words, "선 "+e.Line)
		}
		for _, k := range []string{"left", "top", "width", "height"} {
			if v, has := wcNum(args, k); has {
				v := v
				switch k {
				case "left":
					e.Left = &v
				case "top":
					e.Top = &v
				case "width":
					e.Width = &v
				case "height":
					e.Height = &v
				}
				words = append(words, fmt.Sprintf("%s %g", k, v))
			}
		}
		if e.NewName = wcStr(args, "new_name"); e.NewName != "" {
			words = append(words, "이름 「"+e.NewName+"」")
		}
		if len(words) == 0 {
			return nil, nil, true, fmt.Errorf("바꿀 것이 없습니다 — text·fill·line_color·left/top/width/height·new_name 중 하나")
		}
		id, nm := wcInt(args, "id"), wcStr(args, "name")
		if id == 0 && nm == "" {
			return nil, nil, true, fmt.Errorf("id 나 name 이 있어야 합니다 — list_shapes 가 둘 다 줍니다")
		}
		gotID, gotName, err := d.EditShape(id, nm, e)
		if err != nil {
			return nil, nil, true, err
		}
		// 창은 이름을 바꾼 **뒤의** 이름으로 말한다(Office.js 프록시의 name 은 대입하는 순간 바뀐다) — 같게 둔다.
		out := gotName
		if e.NewName != "" {
			out = e.NewName
		}
		who := out
		if who == "" {
			who = strconv.Itoa(gotID)
		}
		return map[string]any{"id": gotID, "name": out}, []string{fmt.Sprintf("도형 「%s」: %s", who, strings.Join(words, ", "))}, true, nil

	case "delete_shape":
		id, nm := wcInt(args, "id"), wcStr(args, "name")
		if id == 0 && nm == "" {
			return nil, nil, true, fmt.Errorf("id 나 name 이 있어야 합니다 — list_shapes 가 둘 다 줍니다")
		}
		gotID, gotName, err := d.DeleteShape(id, nm)
		if err != nil {
			return nil, nil, true, err
		}
		who := gotName
		if who == "" {
			who = strconv.Itoa(gotID)
		}
		return map[string]any{"id": gotID, "name": gotName}, []string{fmt.Sprintf("도형 「%s」 를 지웠습니다", who)}, true, nil

	case "edit_table":
		n := wcInt(args, "table")
		if n < 1 {
			return nil, nil, true, fmt.Errorf("table 이 없습니다 — 표 번호(1부터)")
		}
		var e wordTableEdit
		if m, has := args["merge"].(map[string]any); has {
			fr, fc := wcInt(m, "from_row"), wcInt(m, "from_column")
			tr, tc := fr, fc
			if _, h := m["to_row"]; h {
				tr = wcInt(m, "to_row")
			}
			if _, h := m["to_column"]; h {
				tc = wcInt(m, "to_column")
			}
			e.Merge = &[4]int{fr, fc, tr, tc}
		}
		if a, has := args["add_columns"].(map[string]any); has {
			e.AddAt = "end"
			switch v := a["at"].(type) {
			case string:
				if v != "" {
					e.AddAt = v
				}
			case nil:
			default:
				e.AddAt = strconv.Itoa(intOf(v))
			}
			if vs, has := a["values"].([]any); has {
				for _, col := range vs {
					var cells []string
					if cs, isArr := col.([]any); isArr {
						for _, c := range cs {
							cells = append(cells, fmt.Sprint(c))
						}
					} else {
						cells = []string{fmt.Sprint(col)}
					}
					e.AddValues = append(e.AddValues, cells)
				}
			}
			e.AddCount = wcInt(a, "count")
			if e.AddCount == 0 {
				e.AddCount = len(e.AddValues)
			}
			if e.AddCount == 0 {
				e.AddCount = 1
			}
		}
		e.DelCols, _ = wcInts(args["delete_columns"])
		e.DelRows, _ = wcInts(args["delete_rows"])
		if e.Merge == nil && e.AddAt == "" && len(e.DelCols) == 0 && len(e.DelRows) == 0 {
			return nil, nil, true, fmt.Errorf("할 일이 없습니다 — delete_rows·delete_columns·add_columns·merge 중 하나")
		}
		total, err := d.Tables()
		if err != nil {
			return nil, nil, true, err
		}
		if n > total {
			return nil, nil, true, fmt.Errorf("문서에 %d번 표가 없습니다 — 표 %d개", n, total)
		}
		rows, cols, done, err := d.EditTable(n, e)
		if err != nil {
			return nil, nil, true, err
		}
		return map[string]any{"table": n, "rows": rows, "columns": cols},
			[]string{fmt.Sprintf("표 %d: %s — 이제 %d×%d", n, strings.Join(done, ", "), rows, cols)}, true, nil

	case "insert_field":
		pieces, what, err := wordFieldPieces(args)
		if err != nil {
			return nil, nil, true, err
		}
		w := wordFieldWhere{Which: wcStr(args, "which"), Kind: wcStr(args, "kind"), Section: wcInt(args, "section"),
			After: wcInt(args, "after"), Before: wcInt(args, "before"), At: wcStr(args, "at")}
		align := wcStr(args, "align")
		if align != "" {
			if _, known := wordAlignCOM[align]; !known {
				return nil, nil, true, fmt.Errorf("align 은 Left, Centered, Right, Justified 중 하나입니다 — %s", align)
			}
		}
		var said string
		switch w.Which {
		case "header", "footer":
			if w.Section == 0 {
				w.Section = 1
			}
			if w.Kind == "" {
				w.Kind = "Primary"
			}
			if _, known := wordHeaderKindCOM[w.Kind]; !known {
				return nil, nil, true, fmt.Errorf("kind 는 Primary, FirstPage, EvenPages 중 하나입니다 — %s", w.Kind)
			}
			total, err := d.Sections()
			if err != nil {
				return nil, nil, true, err
			}
			if w.Section < 1 || w.Section > total {
				return nil, nil, true, fmt.Errorf("문서에 %d번 구역이 없습니다 — 구역 %d개", w.Section, total)
			}
			part := "머리글"
			if w.Which == "footer" {
				part = "바닥글"
			}
			said = fmt.Sprintf("구역 %d %s(%s)에", w.Section, part, w.Kind)
		case "":
			switch {
			case w.After != 0:
				if err := wcParaInRange(d, w.After); err != nil {
					return nil, nil, true, err
				}
				said = fmt.Sprintf("문단 %d 뒤에", w.After)
			case w.Before != 0:
				if err := wcParaInRange(d, w.Before); err != nil {
					return nil, nil, true, err
				}
				said = fmt.Sprintf("문단 %d 앞에", w.Before)
			case w.At == "start":
				said = "본문 처음에"
			default:
				w.At = "end"
				said = "본문 끝에"
			}
		default:
			return nil, nil, true, fmt.Errorf("which 는 header 나 footer 입니다 — %s", w.Which)
		}
		count, err := d.InsertFields(w, pieces, align)
		if err != nil {
			return nil, nil, true, err
		}
		var which any
		if w.Which != "" {
			which = w.Which
		}
		return map[string]any{"fields": count, "which": which}, []string{said + " " + what}, true, nil

	case "suggest":
		what := wcStr(args, "what")
		if what == "" {
			return nil, nil, true, fmt.Errorf("what 이 없습니다 — 제안 한 문장")
		}
		var fix map[string]any
		if f, has := args["fix"].(map[string]any); has {
			tool := wcStr(f, "tool")
			if !wordFixTools[tool] {
				return nil, nil, true, fmt.Errorf("제안으로 누를 수 있는 손은 %s 뿐입니다 — %s", strings.Join(wordFixToolList, ", "), tool)
			}
			fix = f
		}
		body := map[string]any{"what": what, "why": wcStr(args, "why"), "paragraph": nil, "fix": fix}
		para := wcInt(args, "paragraph")
		if para != 0 {
			body["paragraph"] = para
		}
		raw, _ := json.Marshal(body)
		key := wordFixPrefix + strings.ToUpper(strconv.FormatInt(wordNow().UnixMilli(), 36)) + wordRandTag()
		if err := d.SetVariable(key, string(raw)); err != nil {
			return nil, nil, true, err
		}
		where := "문서에"
		var p any
		if para != 0 {
			where = fmt.Sprintf("문단 %d 에", para)
			p = para
		}
		return map[string]any{"suggestion": key, "paragraph": p},
			[]string{fmt.Sprintf("%s 제안을 붙였습니다 — %s. **이건 아직 안 고친 것입니다** — 작업창의 「적용」을 누르기 전까지 문서는 그대로입니다", where, wcClip(what, 60))}, true, nil

	case "read_suggestions":
		vars, err := d.Variables(wordFixPrefix)
		if err != nil {
			return nil, nil, true, err
		}
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([]any, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, wordDecodeSuggestion(k, vars[k]))
		}
		return map[string]any{"scope": "document", "count": len(rows), "suggestions": rows}, nil, true, nil

	case "drop_suggestion":
		key := wcStr(args, "key")
		if key == "" {
			return nil, nil, true, fmt.Errorf("key 가 없습니다")
		}
		if !strings.HasPrefix(key, wordFixPrefix) {
			return nil, nil, true, fmt.Errorf("제안의 키가 아닙니다 — %s", key)
		}
		had, err := d.DeleteVariable(key)
		if err != nil {
			return nil, nil, true, err
		}
		if !had {
			return nil, nil, true, fmt.Errorf("그런 제안이 없습니다: %s", key)
		}
		return map[string]any{"suggestion": key, "dropped": true}, []string{fmt.Sprintf("제안 %s 를 뗐습니다 — 고치지는 않았습니다", key)}, true, nil
	}
	return nil, nil, false, nil
}

// wordHeaderKindCOM 은 머리글(word_enums.go 의 wordHeaderKinds)·바닥글의 종류 → WdHeaderFooterIndex.
var wordHeaderKindCOM = map[string]int{"Primary": 1, "FirstPage": 2, "EvenPages": 3}

// ── 제안 ──
//
// 창은 제안을 settings(WordApi 1.4)에 적는데 2021 에는 그 자리가 없다. COM 에서는 문서 변수(Document.Variables)에 **같은 열쇠
// 같은 모양**(MAGI.FIX.… → JSON)으로 적는다 — 파일과 함께 다니고, 사람 눈에는 안 보이고, 창이 헬퍼를 거쳐 읽는다(View.#suggestionRun).

// wordFixPrefix 는 제안 열쇠의 머리 — handCore.js FIX_PREFIX.
const wordFixPrefix = "MAGI.FIX."

// wordFixToolList 는 제안으로 누를 수 있는 손 — handCore.js FIX_TOOLS. 모두 1.3 에서 창이 제 손으로 한다(적용은 창이 누른다).
var wordFixToolList = []string{"replace_paragraph", "format_text", "format_paragraph", "set_style", "replace_all", "insert_paragraphs"}

var wordFixTools = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range wordFixToolList {
		m[t] = true
	}
	return m
}()

// wordNow 는 시험이 바꿔 낀다.
var wordNow = time.Now

func wordRandTag() string {
	const al = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = al[int(b[i])%len(al)]
	}
	return string(b)
}

// wordDecodeSuggestion 은 창의 WordHand.decodeSuggestion 과 같다 — 못 읽는 것은 「읽을 수 없는 제안」으로, 누를 수 없게.
func wordDecodeSuggestion(key, value string) map[string]any {
	var body map[string]any
	if json.Unmarshal([]byte(value), &body) != nil || body == nil {
		return map[string]any{"key": key, "what": "읽을 수 없는 제안입니다", "broken": true, "appliable": false}
	}
	what, ok := body["what"].(string)
	if !ok {
		return map[string]any{"key": key, "what": "읽을 수 없는 제안입니다", "broken": true, "appliable": false}
	}
	var fix any
	appliable := false
	if f, isMap := body["fix"].(map[string]any); isMap {
		if tool, _ := f["tool"].(string); tool != "" {
			fix = f
			appliable = wordFixTools[tool]
		}
	}
	why, _ := body["why"].(string)
	return map[string]any{"key": key, "what": what, "why": why, "paragraph": body["paragraph"], "fix": fix, "appliable": appliable}
}
