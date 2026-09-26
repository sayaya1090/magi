package office

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── 셋째 묶음의 가짜: 도형 · 표 · 필드 ──

func (f *fakeWordDoc) Shapes() ([]wordShape, error) { return f.shapes, nil }
func (f *fakeWordDoc) AddShape(para int, s wordShapeSpec) (int, string, error) {
	id := 100 + len(f.shapes)
	name := s.Name
	if name == "" {
		name = fmt.Sprintf("도형 %d", id)
	}
	typ := "GeometricShape"
	if s.Mso == 0 {
		typ = "TextBox"
	}
	f.shapes = append(f.shapes, wordShape{ID: id, Name: name, Type: typ, Text: s.Text, Left: s.Left, Top: s.Top, Width: s.Width, Height: s.Height})
	f.lastMso = s.Mso
	return id, name, nil
}
func (f *fakeWordDoc) pick(id int, name string) (int, error) {
	for i, s := range f.shapes {
		if (id != 0 && s.ID == id) || (id == 0 && s.Name == name) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("그런 도형이 없습니다")
}
func (f *fakeWordDoc) EditShape(id int, name string, e wordShapeEdit) (int, string, error) {
	i, err := f.pick(id, name)
	if err != nil {
		return 0, "", err
	}
	if e.Text != nil {
		f.shapes[i].Text = *e.Text
	}
	if e.NewName != "" {
		f.shapes[i].Name = e.NewName
	}
	return f.shapes[i].ID, f.shapes[i].Name, nil
}
func (f *fakeWordDoc) DeleteShape(id int, name string) (int, string, error) {
	i, err := f.pick(id, name)
	if err != nil {
		return 0, "", err
	}
	s := f.shapes[i]
	f.shapes = append(f.shapes[:i], f.shapes[i+1:]...)
	return s.ID, s.Name, nil
}
func (f *fakeWordDoc) Tables() (int, error) { return f.tables, nil }
func (f *fakeWordDoc) EditTable(n int, e wordTableEdit) (int, int, []string, error) {
	f.tableEdits = append(f.tableEdits, e)
	rows, cols := 3, 3
	var done []string
	if e.Merge != nil {
		done = append(done, fmt.Sprintf("(%d,%d)–(%d,%d) 병합", e.Merge[0], e.Merge[1], e.Merge[2], e.Merge[3]))
	}
	if e.AddAt != "" {
		cols += e.AddCount
		done = append(done, fmt.Sprintf("열 %d개 추가(%s)", e.AddCount, e.AddAt))
	}
	return rows, cols, done, nil
}
func (f *fakeWordDoc) InsertFields(w wordFieldWhere, pieces []wordFieldPiece, align string) (int, error) {
	f.fieldCalls = append(f.fieldCalls, w)
	n := 0
	for _, p := range pieces {
		if p.Text == nil {
			n++
		}
	}
	return n, nil
}

// 셋째 묶음도 인자 검사와 답의 모양이 창과 같다 — 숫자는 와이어의 모양(json.Number)으로.
func TestWordComRunMatchesThePaneOnShapesTablesAndFields(t *testing.T) {
	d := &fakeWordDoc{paras: 3, tables: 1}

	// 도형: 모르는 모양 · 없는 문단 · 넣기(기본값과 숫자) · 목록 · 고치기 · 지우기
	if _, _, _, err := wordComRunMore(d, "insert_shape", map[string]any{"shape": "spaceship"}); err == nil || !strings.Contains(err.Error(), "rectangle") {
		t.Errorf("모르는 모양을 받았거나 아는 것을 안 댔다: %v", err)
	}
	if _, _, _, err := wordComRunMore(d, "insert_shape", map[string]any{"paragraph": json.Number("9")}); err == nil {
		t.Error("없는 문단에 도형을 걸었다")
	}
	if _, _, _, err := wordComRunMore(d, "insert_shape", map[string]any{"fill": "blue"}); err == nil {
		t.Error("#RRGGBB 가 아닌 채우기를 받았다")
	}
	res, changed, ok, err := wordComRunMore(d, "insert_shape", map[string]any{"shape": "star", "text": "주의", "name": "sweepbox", "left": json.Number("100"), "width": json.Number("80")})
	if !ok || err != nil || d.lastMso != 92 || res["id"] != 100 || !strings.Contains(changed[0], "도형(star)를 넣었습니다 — id 100 「sweepbox」, (100, 72) 80×60") {
		t.Fatalf("별 넣기: mso=%d %v %v %v", d.lastMso, res, changed, err)
	}
	if _, changed, _, _ = wordComRunMore(d, "insert_shape", map[string]any{}); !strings.HasPrefix(changed[0], "글 상자를 넣었습니다") || d.lastMso != 0 {
		t.Errorf("기본은 글 상자다: %v mso=%d", changed, d.lastMso)
	}
	res, _, _, _ = wordComRunMore(d, "list_shapes", map[string]any{})
	if res["count"] != 2 {
		t.Fatalf("목록: %v", res)
	}
	if _, _, _, err := wordComRunMore(d, "format_shape", map[string]any{"name": "sweepbox"}); err == nil {
		t.Error("바꿀 것 없이 도형을 고쳤다")
	}
	if _, _, _, err := wordComRunMore(d, "format_shape", map[string]any{"text": "x"}); err == nil {
		t.Error("id·name 없이 도형을 골랐다")
	}
	if _, changed, _, err = wordComRunMore(d, "format_shape", map[string]any{"name": "sweepbox", "text": "주의!", "new_name": "box2", "top": json.Number("10")}); err != nil ||
		changed[0] != "도형 「box2」: 글 「주의!」, top 10, 이름 「box2」" {
		t.Fatalf("도형 고치기: %v %v", changed, err)
	}
	if _, changed, _, err = wordComRunMore(d, "delete_shape", map[string]any{"name": "box2"}); err != nil || changed[0] != "도형 「box2」 를 지웠습니다" {
		t.Fatalf("도형 지우기: %v %v", changed, err)
	}

	// 표: 할 일 없음 · 없는 표 · 병합(to 를 빠뜨리면 from 과 같다) + 열 추가
	if _, _, _, err := wordComRunMore(d, "edit_table", map[string]any{"table": json.Number("1")}); err == nil {
		t.Error("할 일 없이 표를 고쳤다")
	}
	if _, _, _, err := wordComRunMore(d, "edit_table", map[string]any{"table": json.Number("2"), "delete_rows": []any{json.Number("0")}}); err == nil || !strings.Contains(err.Error(), "표 1개") {
		t.Errorf("없는 표를 고쳤거나 표 수를 안 댔다: %v", err)
	}
	_, changed, _, err = wordComRunMore(d, "edit_table", map[string]any{"table": json.Number("1"),
		"merge":       map[string]any{"from_row": json.Number("0"), "from_column": json.Number("0"), "to_column": json.Number("1")},
		"add_columns": map[string]any{"at": "end", "values": []any{[]any{"합계", "1", "2"}}}})
	if err != nil || changed[0] != "표 1: (0,0)–(0,1) 병합, 열 1개 추가(end) — 이제 3×4" {
		t.Fatalf("병합+열: %v %v", changed, err)
	}
	if e := d.tableEdits[len(d.tableEdits)-1]; e.AddCount != 1 || len(e.AddValues) != 1 || e.AddValues[0][0] != "합계" {
		t.Errorf("열 값이 창처럼 열마다 위→아래로 안 왔다: %+v", e)
	}

	// 필드: 조각 · 자리 · 거절
	if _, _, _, err := wordComRunMore(d, "insert_field", map[string]any{}); err == nil {
		t.Error("field·template 없이 필드를 넣었다")
	}
	if _, _, _, err := wordComRunMore(d, "insert_field", map[string]any{"template": "쪽 없음"}); err == nil {
		t.Error("필드 자리가 없는 template 을 받았다")
	}
	if _, _, _, err := wordComRunMore(d, "insert_field", map[string]any{"which": "footer", "section": json.Number("4"), "field": "page"}); err == nil || !strings.Contains(err.Error(), "구역 1개") {
		t.Errorf("없는 구역 바닥글에 넣었다: %v", err)
	}
	res, changed, _, err = wordComRunMore(d, "insert_field", map[string]any{"which": "footer", "template": "{page} / {pages}", "align": "Centered"})
	if err != nil || res["fields"] != 2 || changed[0] != "구역 1 바닥글(Primary)에 쪽 번호·전체 쪽수 필드를 넣었습니다 — 「{page} / {pages}」" {
		t.Fatalf("바닥글 쪽 번호: %v %v %v", res, changed, err)
	}
	if _, changed, _, err = wordComRunMore(d, "insert_field", map[string]any{"field": "toc", "after": json.Number("1")}); err != nil || changed[0] != "문단 1 뒤에 목차 필드를 넣었습니다" {
		t.Fatalf("목차: %v %v", changed, err)
	}
	if w := d.fieldCalls[len(d.fieldCalls)-1]; w.After != 1 || w.Which != "" {
		t.Errorf("자리가 안 실렸다: %+v", w)
	}
}

// 목차 필드의 스위치는 창과 같다 — 단계를 안 주면 1-3.
func TestWordFieldPiecesMatchThePane(t *testing.T) {
	p, said, err := wordFieldPieces(map[string]any{"field": "toc", "levels": "1-2"})
	if err != nil || len(p) != 1 || p[0].Type != 13 || p[0].Code != ` \o "1-2" \h \z \u ` || said != "목차 필드를 넣었습니다" {
		t.Fatalf("목차: %+v %q %v", p, said, err)
	}
	p, _, _ = wordFieldPieces(map[string]any{"template": "- {page} -"})
	if len(p) != 3 || *p[0].Text != "- " || p[1].Type != 33 || *p[2].Text != " -" {
		t.Fatalf("글과 필드가 번갈아 와야 한다: %+v", p)
	}
}

// 두 벌(창의 표와 이 길의 표)이 같은 이름을 알아야 한다 — 한쪽에만 있으면 365 와 2021 이 다른 것을 한다.
func TestWordComKnowsThePanesShapesAndFields(t *testing.T) {
	src, err := os.ReadFile("../../word/addin/src/adapter/WordHand.js")
	if err != nil {
		t.Fatalf("창의 소스를 못 읽었다(%v)", err)
	}
	m := regexp.MustCompile(`static #GEOMETRY = \{([^}]*)\}`).FindSubmatch(src)
	if m == nil {
		t.Fatal("창의 #GEOMETRY 를 못 찾았다")
	}
	var pane []string
	for _, k := range regexp.MustCompile(`([a-z_]+):`).FindAllSubmatch(m[1], -1) {
		pane = append(pane, string(k[1]))
	}
	var mine []string
	for k := range wordGeometry {
		mine = append(mine, k)
	}
	sort.Strings(pane)
	sort.Strings(mine)
	if strings.Join(pane, ",") != strings.Join(mine, ",") {
		t.Errorf("도형 이름이 갈렸다\n  창: %v\n  COM: %v", pane, mine)
	}
	for _, f := range wordFieldKinds {
		if _, ok := wordFieldTypes[f]; !ok {
			t.Errorf("광고된 필드 %s 를 COM 길이 모른다", f)
		}
	}
}

// 그릴 도구(pdftoppm·sips)가 없는 Windows 에서 render_page 는 Word 가 제 손으로 그린 쪽 그림을 쓴다 — 그리고 그 사실을 답에 싣는다.
// 그 밖의 경우(도구가 있다, Word 가 아니다, COM 이 없다)에는 이 길을 안 탄다.
func TestRenderPageFallsBackToWordsOwnDrawingOnlyWhenNothingCanDrawThePDF(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n1 0 obj << /Type /Page >> endobj\n%%EOF"))
	t.Setenv("PATH", t.TempDir()) // pdftoppm·sips 가 없는 세상
	wasR, wasOS := renderWordPage, wordComOnThisOS
	t.Cleanup(func() { renderWordPage, wordComOnThisOS = wasR, wasOS })
	var asked []string
	renderWordPage = func(doc string, page, width int) ([]byte, error) {
		asked = append(asked, fmt.Sprintf("%s/%d/%d", doc, page, width))
		return base64.StdEncoding.DecodeString(png)
	}
	wordComOnThisOS = true
	hand := &fakeHand{attached: true, answer: HandResult{Document: "wd-doc-9", Result: map[string]any{"pdf_base64": pdf, "page": 1, "max_width": 400}}}
	out := (&MCPServer{App: Word, Hand: hand}).call(httptest.NewRequest("POST", "/mcp", nil), "render_page", json.RawMessage(`{"page":1}`))
	blocks, _ := out["content"].([]map[string]any)
	if out["isError"] == true || len(blocks) != 2 || blocks[1]["type"] != "image" {
		t.Fatalf("Word 가 그린 그림이 안 왔다: %v", out)
	}
	if len(asked) != 1 || asked[0] != "wd-doc-9/1/400" {
		t.Errorf("표식의 문서·쪽·폭으로 안 물었다: %v", asked)
	}
	if text, _ := blocks[0]["text"].(string); !strings.Contains(text, `"via": "com"`) {
		t.Errorf("Word 로 그렸다는 사실을 숨겼다: %s", text)
	}

	// COM 이 없으면 원래의 이유가 그대로 간다. (답의 맵은 헬퍼가 고쳐 쓰므로 — pdf_base64 를 지운다 — 새로 준다.)
	wordComOnThisOS = false
	hand.answer = HandResult{Document: "wd-doc-9", Result: map[string]any{"pdf_base64": pdf, "page": 1, "max_width": 400}}
	out = (&MCPServer{App: Word, Hand: hand}).call(httptest.NewRequest("POST", "/mcp", nil), "render_page", json.RawMessage(`{"page":1}`))
	if out["isError"] != true || !strings.Contains(firstText(t, out), "pdftoppm") {
		t.Errorf("COM 이 없는데 다른 길을 탔거나 이유를 바꿨다: %v", out)
	}
	if len(asked) != 1 {
		t.Errorf("COM 이 없는데 Word 에 물었다: %v", asked)
	}
}

// ── 제안의 가짜: 문서 변수 ──

func (f *fakeWordDoc) Variables(prefix string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.vars {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}
func (f *fakeWordDoc) SetVariable(name, value string) error {
	if f.vars == nil {
		f.vars = map[string]string{}
	}
	f.vars[name] = value
	return nil
}
func (f *fakeWordDoc) DeleteVariable(name string) (bool, error) {
	_, had := f.vars[name]
	delete(f.vars, name)
	return had, nil
}

// 제안은 창과 같은 열쇠·같은 모양으로 적히고, 창과 같은 규칙으로 읽힌다 — 누를 수 있는 손만 appliable 이다.
func TestWordComSuggestionsMatchThePane(t *testing.T) {
	d := &fakeWordDoc{paras: 3}
	if _, _, _, err := wordComRunMore(d, "suggest", map[string]any{"what": "굵게", "fix": map[string]any{"tool": "delete_paragraphs"}}); err == nil || !strings.Contains(err.Error(), "format_text") {
		t.Errorf("누를 수 없는 손을 받았거나 누를 수 있는 것을 안 댔다: %v", err)
	}
	res, changed, _, err := wordComRunMore(d, "suggest", map[string]any{"what": "제목을 굵게", "why": "눈에 띄게", "paragraph": json.Number("1"),
		"fix": map[string]any{"tool": "format_text", "args": map[string]any{"from": json.Number("1"), "bold": true}}})
	if err != nil || !strings.HasPrefix(fmt.Sprint(res["suggestion"]), "MAGI.FIX.") || !strings.Contains(changed[0], "문단 1 에 제안을 붙였습니다 — 제목을 굵게. **이건 아직 안 고친 것입니다**") {
		t.Fatalf("제안: %v %v %v", res, changed, err)
	}
	key := res["suggestion"].(string)
	d.vars["MAGI.FIX.BROKEN"] = "{not json"
	d.vars["다른변수"] = "x"
	res, _, _, _ = wordComRunMore(d, "read_suggestions", map[string]any{})
	if res["count"] != 2 {
		t.Fatalf("제안이 아닌 변수까지 읽었거나 빠뜨렸다: %v", res)
	}
	var mine, broken map[string]any
	for _, r := range res["suggestions"].([]any) {
		m := r.(map[string]any)
		if m["key"] == key {
			mine = m
		} else {
			broken = m
		}
	}
	if mine["appliable"] != true || mine["what"] != "제목을 굵게" || mine["why"] != "눈에 띄게" {
		t.Errorf("제안이 창의 모양으로 안 읽혔다: %v", mine)
	}
	if broken["broken"] != true || broken["appliable"] != false {
		t.Errorf("못 읽는 제안을 누를 수 있게 뒀다: %v", broken)
	}
	if _, _, _, err := wordComRunMore(d, "drop_suggestion", map[string]any{"key": "다른변수"}); err == nil {
		t.Error("제안이 아닌 변수를 뗐다")
	}
	if _, changed, _, err = wordComRunMore(d, "drop_suggestion", map[string]any{"key": key}); err != nil || !strings.Contains(changed[0], "고치지는 않았습니다") {
		t.Fatalf("떼기: %v %v", changed, err)
	}
	if _, _, _, err := wordComRunMore(d, "drop_suggestion", map[string]any{"key": key}); err == nil {
		t.Error("없는 제안을 뗐다고 했다")
	}
}

// 누를 수 있는 손의 목록은 창의 FIX_TOOLS 와 같다.
func TestWordComFixToolsAreThePanes(t *testing.T) {
	src, err := os.ReadFile("../../word/addin/src/adapter/handCore.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`FIX_TOOLS = Object\.freeze\(\[([^\]]*)\]\)`).FindSubmatch(src)
	if m == nil {
		t.Fatal("창의 FIX_TOOLS 를 못 찾았다")
	}
	var pane []string
	for _, k := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(m[1], -1) {
		pane = append(pane, string(k[1]))
	}
	if strings.Join(pane, ",") != strings.Join(wordFixToolList, ",") {
		t.Errorf("누를 수 있는 손이 갈렸다\n  창: %v\n  COM: %v", pane, wordFixToolList)
	}
}
