package office

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── 가짜 COM ──

type fakeWordExtra struct {
	paras    int
	saved    string
	exported []string
	proof    []wordProof
	compared []string
}

func (f *fakeWordExtra) Paragraphs() (int, error)  { return f.paras, nil }
func (f *fakeWordExtra) FilePath() (string, error) { return f.saved, nil }
func (f *fakeWordExtra) ExportPDF(p string) error {
	f.exported = append(f.exported, p)
	return os.WriteFile(p, []byte("%PDF-1.7"), 0o600)
}
func (f *fakeWordExtra) Proof(from, to, limit int) ([]wordProof, bool, error) {
	var out []wordProof
	for _, p := range f.proof {
		if p.Paragraph >= from && p.Paragraph <= to {
			out = append(out, p)
		}
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
func (f *fakeWordExtra) Stats() (wordStats, error) {
	return wordStats{Pages: 3, Words: 120, Characters: 400, CharactersWithSpaces: 480, Paragraphs: f.paras, Lines: 30}, nil
}
func (f *fakeWordExtra) Compare(other string, rev bool) (string, int, error) {
	f.compared = append(f.compared, other)
	return "비교 결과 1", 7, nil
}
func (f *fakeWordExtra) Close() {}

type fakeXLExtra struct {
	saved      string
	exported   [][2]string
	goal       xlGoal
	sparkCells int
	slicers    map[string]bool
}

func (f *fakeXLExtra) FilePath(string) (string, error) { return f.saved, nil }
func (f *fakeXLExtra) ExportPDF(_, sheet, p string) error {
	f.exported = append(f.exported, [2]string{sheet, p})
	return os.WriteFile(p, []byte("%PDF-1.7"), 0o600)
}
func (f *fakeXLExtra) GoalSeek(_, _, _ string, _ float64, _ string) (xlGoal, error) {
	return f.goal, nil
}
func (f *fakeXLExtra) AddSparklines(_, _, _, _, _, _ string, _ bool) (int, error) {
	return f.sparkCells, nil
}
func (f *fakeXLExtra) RemoveSparklines(_, _, _ string) (int, error) { return f.sparkCells, nil }
func (f *fakeXLExtra) AddSlicer(_, source, field, sheet, name string, _, _, _, _ float64) (xlSlicer, error) {
	if name == "" {
		name = "슬라이서_" + field
	}
	f.slicers[name] = true
	return xlSlicer{Name: name, Caption: field, Sheet: sheet, Kind: "table"}, nil
}
func (f *fakeXLExtra) RemoveSlicer(_, name string) error {
	if !f.slicers[name] {
		return errors.New("슬라이서 " + name + " 이 없습니다")
	}
	delete(f.slicers, name)
	return nil
}
func (f *fakeXLExtra) Close() {}

// ── PDF 자리 ──

func TestPDFGoesWhereItShouldAndNeverOverwritesSilently(t *testing.T) {
	dir := t.TempDir()
	was := userDocumentsDir
	t.Cleanup(func() { userDocumentsDir = was })
	userDocumentsDir = func() (string, error) { return dir, nil }

	if p, err := pdfTarget(filepath.Join(dir, "보고.docx"), "보고", "", false); err != nil || p != filepath.Join(dir, "보고.pdf") {
		t.Errorf("저장된 문서 옆이 아니다: %q %v", p, err)
	}
	if p, err := pdfTarget("", "통합 문서1", "", false); err != nil || p != filepath.Join(dir, "통합 문서1.pdf") {
		t.Errorf("저장 안 한 문서가 「문서」 폴더로 안 갔다: %q %v", p, err)
	}
	if p, _ := pdfTarget("", `a/b:c`, "", false); filepath.Base(p) != "a_b_c.pdf" {
		t.Errorf("파일 이름에 못 쓰는 글자를 안 걸렀다: %q", p)
	}
	for asked, want := range map[string]string{"rel.pdf": "전체 경로", filepath.Join(dir, "x.txt"): ".pdf 로 끝나야", filepath.Join(dir, "없는폴더", "x.pdf"): "폴더가 없습니다"} {
		if _, err := pdfTarget("", "", asked, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: 기대 %q, 받은 %v", asked, want, err)
		}
	}
	there := filepath.Join(dir, "있음.pdf")
	_ = os.WriteFile(there, []byte("x"), 0o600)
	if _, err := pdfTarget("", "", there, false); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Errorf("있는 파일을 조용히 덮으려 했다: %v", err)
	}
	if _, err := pdfTarget("", "", there, true); err != nil {
		t.Errorf("overwrite 인데 거절했다: %v", err)
	}
}

// ── Word ──

func TestWordComToolsCheckTheirArgumentsAndSayWhatHappened(t *testing.T) {
	dir := t.TempDir()
	x := &fakeWordExtra{paras: 10, saved: filepath.Join(dir, "계약.docx"), proof: []wordProof{
		{Kind: "spelling", Text: "계약서을", Paragraph: 2, Suggestions: []string{"계약서를"}}, {Kind: "grammar", Text: "문장", Paragraph: 5}, {Kind: "spelling", Text: "틀림", Paragraph: 9},
	}}

	res, changed, err := wordExtraRun(x, "계약", "export_pdf", map[string]any{})
	if err != nil || res["path"] != filepath.Join(dir, "계약.pdf") || !strings.Contains(changed[0], "PDF 로 내보냈습니다") {
		t.Fatalf("PDF: %v %v %v", res, changed, err)
	}

	res, changed, err = wordExtraRun(x, "", "proofread", map[string]any{})
	if err != nil || res["count"] != 3 || !strings.Contains(changed[0], "맞춤법 2 · 문법 1") {
		t.Fatalf("전체 교정: %v %v %v", res, changed, err)
	}
	// 범위는 다른 Word 도구와 같은 규칙 — to 를 빼면 from 한 문단
	if res, _, _ = wordExtraRun(x, "", "proofread", map[string]any{"from": json.Number("2")}); res["count"] != 1 {
		t.Errorf("from 만 주면 그 문단 하나여야 한다: %v", res)
	}
	if res, changed, _ = wordExtraRun(x, "", "proofread", map[string]any{"limit": json.Number("1")}); res["more"] != true || !strings.Contains(changed[0], "더 있음") {
		t.Errorf("잘랐다는 것을 안 말했다: %v %v", res, changed)
	}
	if _, _, err = wordExtraRun(x, "", "proofread", map[string]any{"from": json.Number("11")}); err == nil {
		t.Error("없는 문단을 검사했다")
	}
	if _, changed, _ = wordExtraRun(&fakeWordExtra{paras: 1}, "", "proofread", map[string]any{}); !strings.Contains(strings.Join(changed, " "), "교정 도구") {
		t.Errorf("빈 결과가 무엇을 뜻할 수 있는지 안 말했다: %v", changed)
	}

	if res, changed, err = wordExtraRun(x, "", "document_stats", map[string]any{}); err != nil || res["pages"] != 3 || !strings.HasPrefix(changed[0], "3쪽") {
		t.Errorf("통계: %v %v %v", res, changed, err)
	}

	other := filepath.Join(dir, "계약_수정.docx")
	_ = os.WriteFile(other, []byte("x"), 0o600)
	if res, changed, err = wordExtraRun(x, "", "compare_documents", map[string]any{"path": other}); err != nil || res["revisions"] != 7 || !strings.Contains(changed[1], "작업창이 안 붙어") {
		t.Errorf("비교: %v %v %v", res, changed, err)
	}
	for args, want := range map[string]string{
		`{}`:                 "path 가 없습니다",
		`{"path":"상대.docx"}`: "전체 경로",
		`{"path":"` + jsonPath(filepath.Join(dir, "a.xlsx")) + `"}`:  "Word 문서가 아닙니다",
		`{"path":"` + jsonPath(filepath.Join(dir, "없음.docx")) + `"}`: "파일이 없습니다",
		`{"path":"` + jsonPath(x.saved) + `"}`:                       "파일이 없습니다", // 저장 경로의 파일이 실제로 없다 — 존재 검사가 먼저 문다
		`{"path":"` + jsonPath(other) + `","as":"sideways"}`:         "revised",
	} {
		var a map[string]any
		_ = json.Unmarshal([]byte(args), &a)
		if _, _, err := wordExtraRun(x, "", "compare_documents", a); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: 기대 %q, 받은 %v", args, want, err)
		}
	}
	_ = os.WriteFile(x.saved, []byte("x"), 0o600)
	if _, _, err := wordExtraRun(x, "", "compare_documents", map[string]any{"path": x.saved}); err == nil || !strings.Contains(err.Error(), "자신") {
		t.Errorf("자기 자신과 비교하려 했다: %v", err)
	}
}

func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

// 헬퍼가 답하는 길: 창에게 표식을 묻고, 표식이 없으면 이유를 대고 멈춘다. COM 이 없는 기계에서는 부르지도 못한다.
func TestWordComToolsAskThePaneWhichDocument(t *testing.T) {
	wasOS, wasOpen := comLocalOnThisOS, openWordExtra
	t.Cleanup(func() { comLocalOnThisOS, openWordExtra = wasOS, wasOpen })
	comLocalOnThisOS = true
	var opened string
	openWordExtra = func(id string) (wordExtra, error) { opened = id; return &fakeWordExtra{paras: 4}, nil }

	h := &fakeHand{answer: HandResult{Document: "wd-abc", Label: "계약.docx"}}
	res, err := wordLocal("document_stats")(h, "wd-abc", map[string]any{})
	if err != nil || opened != "abc" || res.Result["via"] != "com" || res.Document != "wd-abc" {
		t.Fatalf("표식으로 안 잡았다: %v %v opened=%q", res, err, opened)
	}
	if len(h.calls) != 1 || h.calls[0].Op != "list_paragraphs" {
		t.Errorf("창에게 문서를 안 물었다: %v", h.calls)
	}
	if _, err := wordLocal("document_stats")(&fakeHand{answer: HandResult{Document: "doc-x"}}, "", map[string]any{}); err == nil || !strings.Contains(err.Error(), "MAGI.DOC") {
		t.Errorf("표식 없는 문서를 짐작으로 잡았다: %v", err)
	}
	comLocalOnThisOS = false
	if _, err := wordLocal("document_stats")(h, "", map[string]any{}); err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Errorf("COM 이 없는 기계에서 불렀다: %v", err)
	}
}

// ── Excel ──

func TestExcelComToolsCheckTheirArgumentsAndSayWhatHappened(t *testing.T) {
	dir := t.TempDir()
	x := &fakeXLExtra{saved: filepath.Join(dir, "매출.xlsx"), sparkCells: 4, slicers: map[string]bool{}}

	if res, _, err := xlExtraRun(x, "매출.xlsx", "Sheet1", "매출.xlsx", "export_pdf", map[string]any{"sheet": "2분기"}); err != nil || res["path"] != filepath.Join(dir, "매출 - 2분기.pdf") || res["what"] != "시트 2분기" {
		t.Errorf("시트 PDF 가 통장 PDF 자리를 덮으려 했거나 틀렸다: %v %v", res, err)
	}
	if res, _, err := xlExtraRun(x, "매출.xlsx", "Sheet1", "매출.xlsx", "export_pdf", map[string]any{}); err != nil || res["path"] != filepath.Join(dir, "매출.pdf") {
		t.Errorf("통장 PDF: %v %v", res, err)
	}

	x.goal = xlGoal{Found: true, Value: 1000.0, ValueWas: 700.0, Changing: 1300.0, ChangingWas: 1000.0}
	res, changed, err := xlExtraRun(x, "b", "Sheet1", "", "goal_seek", map[string]any{"cell": "B10", "goal": json.Number("1000"), "changing": "B2"})
	if err != nil || res["found"] != true || !strings.Contains(changed[0], "B2 = 1300 (원래 1000)") {
		t.Errorf("목표값: %v %v %v", res, changed, err)
	}
	x.goal = xlGoal{Found: true, Value: 998.0, ChangingWas: 1.0, Changing: 9.0} // Excel 이 「찾았다」 해도 값이 멀면 못 찾은 것이다
	if res, changed, _ = xlExtraRun(x, "b", "Sheet1", "", "goal_seek", map[string]any{"cell": "B10", "goal": 1000.0, "changing": "B2"}); res["found"] != false || !strings.Contains(changed[0], "되돌리려면") {
		t.Errorf("못 맞춘 것을 맞췄다고 했다: %v %v", res, changed)
	}
	for _, a := range []map[string]any{{"cell": "B10", "changing": "B2"}, {"cell": "B1:B3", "goal": 1.0, "changing": "B2"}, {"cell": "B2", "goal": 1.0, "changing": "$B$2"}} {
		if _, _, err := xlExtraRun(x, "b", "Sheet1", "", "goal_seek", a); err == nil {
			t.Errorf("틀린 인자를 받았다: %v", a)
		}
	}

	if res, changed, err = xlExtraRun(x, "b", "Sheet1", "", "add_sparklines", map[string]any{"address": "F2:F5", "source": "B2:E5", "kind": "column", "color": "#1F4E79"}); err != nil || res["cells"] != 4 || !strings.Contains(changed[0], "열 스파크라인 4개") {
		t.Errorf("스파크라인: %v %v %v", res, changed, err)
	}
	for a, want := range map[string]map[string]any{
		"kind 는":         {"address": "F2", "source": "B2:E2", "kind": "pie"},
		"#RRGGBB":        {"address": "F2", "source": "B2:E2", "color": "blue"},
		"line 에만":        {"address": "F2", "source": "B2:E2", "kind": "column", "markers": true},
		"address(그릴 칸들)": {"source": "B2:E2"},
	} {
		if _, _, err := xlExtraRun(x, "b", "Sheet1", "", "add_sparklines", want); err == nil || !strings.Contains(err.Error(), a) {
			t.Errorf("기대 %q, 받은 %v", a, err)
		}
	}
	x.sparkCells = 0
	if _, _, err := xlExtraRun(x, "b", "Sheet1", "", "remove_sparklines", map[string]any{"address": "F2:F5"}); err == nil || !strings.Contains(err.Error(), "없습니다") {
		t.Errorf("없는 스파크라인을 지웠다고 했다: %v", err)
	}

	if res, _, err = xlExtraRun(x, "b", "Sheet1", "", "add_slicer", map[string]any{"source": "매출표", "field": "지역"}); err != nil || res["slicer"] != "슬라이서_지역" || res["sheet"] != "Sheet1" {
		t.Errorf("슬라이서: %v %v", res, err)
	}
	if _, _, err = xlExtraRun(x, "b", "Sheet1", "", "remove_slicer", map[string]any{"name": "슬라이서_지역"}); err != nil || len(x.slicers) != 0 {
		t.Errorf("슬라이서 지우기: %v %v", x.slicers, err)
	}
	if _, _, err = xlExtraRun(x, "b", "Sheet1", "", "remove_slicer", map[string]any{"name": "없음"}); err == nil {
		t.Error("없는 슬라이서를 지웠다고 했다")
	}
}

// Windows 가 아닌 기계에서는 COM 도구 열이 목록에서 빠지고, Windows 에서는 창이 뭘 쟀든 보인다.
func TestComLocalToolsShowOnlyWhereComIs(t *testing.T) {
	was := comLocalOnThisOS
	t.Cleanup(func() { comLocalOnThisOS = was })
	for _, app := range []*App{Word, XL} {
		comLocalOnThisOS = true
		on := listed(&MCPServer{App: app})
		comLocalOnThisOS = false
		off := listed(&MCPServer{App: app})
		for n := range comLocalTools[app.Key] {
			if !on[n] || off[n] {
				t.Errorf("%s %s: Windows 에서 보임 %v, 아닌 곳에서 보임 %v", app.Key, n, on[n], off[n])
			}
		}
		for _, tl := range app.Catalogue(false) {
			if comLocalTools[app.Key][tl.Name] && tl.Local == nil {
				t.Errorf("%s %s: COM 도구인데 헬퍼가 답하지 않는다(Local 없음) — 창의 손에게 가면 「모른다」다", app.Key, tl.Name)
			}
		}
	}
}

// 헬퍼가 COM 으로 답한 도구도 손의 답처럼 「무엇을 했는가」(changed)와 문서 이름을 싣는다 — 앞 판은 둘을 버렸다(실물 2026-09-27).
func TestComLocalAnswersCarryWhatTheyDid(t *testing.T) {
	wasOS, wasOpen := comLocalOnThisOS, openWordExtra
	t.Cleanup(func() { comLocalOnThisOS, openWordExtra = wasOS, wasOpen })
	comLocalOnThisOS = true
	openWordExtra = func(string) (wordExtra, error) { return &fakeWordExtra{paras: 4}, nil }
	srv := &MCPServer{App: Word, Hand: &fakeHand{attached: true, answer: HandResult{Document: "wd-abc", Label: "계약.docx"}}}
	got := srv.call(httptest.NewRequest("POST", "/mcp", nil), "document_stats", json.RawMessage(`{}`))
	text := firstText(t, got)
	if got["isError"] == true || !strings.Contains(text, `"changed"`) || !strings.Contains(text, "3쪽") || !strings.Contains(text, "계약.docx") {
		t.Fatalf("답에 한 일이나 문서 이름이 없다: %s", text)
	}
}

// COM 의 정수는 int32(VT_I4)로 온다 — 그것을 0 으로 읽어 스파크라인 셋을 「0칸」, 있는 표를 「없음」이라 했다(실물 2026-09-27).
func TestComIntegersAreRead(t *testing.T) {
	for _, v := range []any{int32(7), int16(7), uint8(7), uint32(7), int64(7), 7.0, json.Number("7")} {
		if intOf(v) != 7 {
			t.Errorf("%T 를 %d 로 읽었다", v, intOf(v))
		}
	}
}
