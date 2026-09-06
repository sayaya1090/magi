package office

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// 가짜 노트 창고 — COM 대신. 통장 하나, 시트 이름별 주소별 글.
type fakeNoter struct {
	notes  map[string]map[string]string // sheet → address → text
	opened int
	closed int
	book   string
}

func (f *fakeNoter) Notes(book, sheet string) ([]xlNote, error) {
	f.book = book
	var out []xlNote
	for s, m := range f.notes {
		if sheet != "" && s != sheet {
			continue
		}
		for a, t := range m {
			out = append(out, xlNote{Sheet: s, Address: a, Author: "김나리", Text: t})
		}
	}
	return out, nil
}
func (f *fakeNoter) Add(book, sheet, address, text string) (xlNote, bool, error) {
	f.book = book
	if f.notes[sheet] == nil {
		f.notes[sheet] = map[string]string{}
	}
	if old, ok := f.notes[sheet][address]; ok {
		f.notes[sheet][address] = old + "\n" + text
		return xlNote{Sheet: sheet, Address: address, Author: "김나리", Text: f.notes[sheet][address]}, true, nil
	}
	f.notes[sheet][address] = text
	return xlNote{Sheet: sheet, Address: address, Author: "김나리", Text: text}, false, nil
}
func (f *fakeNoter) Delete(book, sheet, address string) (bool, error) {
	if _, ok := f.notes[sheet][address]; !ok {
		return false, nil
	}
	delete(f.notes[sheet], address)
	return true, nil
}
func (f *fakeNoter) Close() { f.closed++ }

// 창의 손 흉내 — 메모 셋은 2021 처럼 NotImplemented, list_sheets 는 통장 이름과 활성 시트를 준다.
type notesHand struct{ calls []string }

func (h *notesHand) Attached() bool { return true }
func (h *notesHand) Call(_ context.Context, document, op string, _ map[string]any) (HandResult, error) {
	h.calls = append(h.calls, op)
	switch op {
	case "list_sheets":
		return HandResult{Document: "wb-book-1", Label: "ltsc.xlsx", Result: map[string]any{"workbook": "ltsc.xlsx", "active": "데이터", "count": 1}}, nil
	case "add_comment", "read_comments", "resolve_comment":
		return HandResult{}, errors.New("NotImplemented — 이 작업이 실행되지 않았습니다. — CommentCollection._OnAccess")
	}
	return HandResult{Document: "wb-book-1", Result: map[string]any{}}, nil
}

func withFakeNoter(t *testing.T) *fakeNoter {
	t.Helper()
	f := &fakeNoter{notes: map[string]map[string]string{}}
	was := openXLNoter
	openXLNoter = func() (xlNoter, error) { f.opened++; return f, nil }
	t.Cleanup(func() { openXLNoter = was })
	return f
}

func TestNotesFallbackTakesOnlyTheCommentToolsRefusedAsNotImplemented(t *testing.T) {
	f := withFakeNoter(t)
	hand := &notesHand{}
	ctx := context.Background()
	// 다른 도구·다른 오류에는 손을 안 댄다 — 그 오류가 그대로 간다.
	if _, handled, _ := xlNotesFallback(ctx, hand, "wb-book-1", "write_range", map[string]any{}, "NotImplemented"); handled {
		t.Error("메모 도구가 아닌 것을 대신했다")
	}
	if _, handled, _ := xlNotesFallback(ctx, hand, "wb-book-1", "add_comment", map[string]any{"address": "A2", "text": "x"}, "InvalidArgument — Range"); handled {
		t.Error("NotImplemented 가 아닌 오류를 대신했다")
	}
	if f.opened != 0 {
		t.Errorf("길이 아닌데 COM 을 열었다: %d", f.opened)
	}
}

func TestNotesFallbackAddsReadsAppendsAndDeletes(t *testing.T) {
	f := withFakeNoter(t)
	hand := &notesHand{}
	ctx := context.Background()

	res, handled, err := xlNotesFallback(ctx, hand, "wb-book-1", "add_comment", map[string]any{"address": "A2", "text": "근거는?"}, "NotImplemented — CommentCollection._OnAccess")
	if !handled || err != nil {
		t.Fatalf("노트로 대신해야 한다: handled=%v err=%v", handled, err)
	}
	if f.book != "ltsc.xlsx" {
		t.Errorf("통장 이름을 창의 list_sheets 에서 받아 COM 에 줘야 한다: %q", f.book)
	}
	if res.Document != "wb-book-1" || res.Label != "ltsc.xlsx" {
		t.Errorf("손댄 문서와 이름을 싣는다: %+v", res)
	}
	if res.Result["kind"] != "note" || res.Result["sheet"] != "데이터" || res.Result["replied"] != false {
		t.Errorf("노트라는 사실과 활성 시트가 실린다: %v", res.Result)
	}
	if len(res.Changed) != 1 || !strings.Contains(res.Changed[0], "노트를 넣었습니다") || !strings.Contains(res.Changed[0], "메모 API 가 없습니다") {
		t.Errorf("바뀐 것이 노트임을 말한다: %v", res.Changed)
	}
	if f.closed != 1 {
		t.Errorf("COM 을 닫아야 한다: %d", f.closed)
	}

	// 같은 셀에 한 번 더 — 답글 대신 덧붙인다.
	res, _, err = xlNotesFallback(ctx, hand, "wb-book-1", "add_comment", map[string]any{"address": "A2", "text": "표 3 참고"}, "NotImplemented")
	if err != nil || res.Result["replied"] != true || !strings.Contains(res.Changed[0], "덧붙였습니다") {
		t.Errorf("있는 노트에는 덧붙인다: %v %v", res.Result, err)
	}
	if f.notes["데이터"]["A2"] != "근거는?\n표 3 참고" {
		t.Errorf("글이 줄로 이어진다: %q", f.notes["데이터"]["A2"])
	}

	// 읽기 — 시트를 안 주면 통장 전부.
	res, _, err = xlNotesFallback(ctx, hand, "wb-book-1", "read_comments", map[string]any{}, "NotImplemented")
	if err != nil || res.Result["count"] != 1 {
		t.Fatalf("노트를 읽는다: %v %v", res.Result, err)
	}
	row := res.Result["comments"].([]any)[0].(map[string]any)
	if row["address"] != "A2" || row["kind"] != "note" || row["resolved"] != nil || row["text"] != "근거는?\n표 3 참고" || row["content"] != nil {
		t.Errorf("노트 한 줄의 모양: %v", row)
	}

	// 해결 표시는 없다 — 지우기만.
	if _, _, err = xlNotesFallback(ctx, hand, "wb-book-1", "resolve_comment", map[string]any{"address": "A2"}, "NotImplemented"); err == nil || !strings.Contains(err.Error(), "해결 표시가 없습니다") {
		t.Errorf("해결 표시는 이름을 대고 거절한다: %v", err)
	}
	res, _, err = xlNotesFallback(ctx, hand, "wb-book-1", "resolve_comment", map[string]any{"address": "A2", "delete": true}, "NotImplemented")
	if err != nil || res.Result["deleted"] != true || len(f.notes["데이터"]) != 0 {
		t.Errorf("delete:true 는 노트를 지운다: %v %v", res.Result, err)
	}
	if _, _, err = xlNotesFallback(ctx, hand, "wb-book-1", "resolve_comment", map[string]any{"address": "A2", "delete": true}, "NotImplemented"); err == nil || !strings.Contains(err.Error(), "노트가 없습니다") {
		t.Errorf("없는 노트는 없다고 말한다: %v", err)
	}
}

// 통장 이름 없이는 COM 으로 안 간다 — 다른 Excel 인스턴스의 통장을 잡을 수 있다. 범위 주소도 노트가 아니다.
type namelessHand struct{ notesHand }

func (h *namelessHand) Call(ctx context.Context, document, op string, args map[string]any) (HandResult, error) {
	if op == "list_sheets" {
		return HandResult{Document: "wb-book-1", Result: map[string]any{"active": "데이터"}}, nil
	}
	return h.notesHand.Call(ctx, document, op, args)
}

func TestNotesFallbackRefusesWithoutAWorkbookNameOrOnARange(t *testing.T) {
	f := withFakeNoter(t)
	ctx := context.Background()
	_, handled, err := xlNotesFallback(ctx, &namelessHand{}, "wb-book-1", "add_comment", map[string]any{"address": "A2", "text": "x"}, "NotImplemented")
	if !handled || err == nil || !strings.Contains(err.Error(), "통장 이름") {
		t.Errorf("이름 없이는 거절: handled=%v err=%v", handled, err)
	}
	if _, _, err = xlNotesFallback(ctx, &notesHand{}, "wb-book-1", "add_comment", map[string]any{"address": "A2:B3", "text": "x"}, "NotImplemented"); err == nil || !strings.Contains(err.Error(), "셀 하나") {
		t.Errorf("범위에는 거절: %v", err)
	}
	if _, _, err = xlNotesFallback(ctx, &notesHand{}, "wb-book-1", "resolve_comment", map[string]any{"address": "A2:B3", "delete": true}, "NotImplemented"); err == nil || !strings.Contains(err.Error(), "셀 하나") {
		t.Errorf("범위 지우기에는 거절: %v", err)
	}
	if len(f.notes) != 0 {
		t.Errorf("거절한 호출은 노트를 안 만든다: %v", f.notes)
	}
}

func TestNotesFallbackSaysWhenThereIsNoComAtAll(t *testing.T) {
	was := openXLNoter
	openXLNoter = func() (xlNoter, error) { return nil, errors.New("COM 은 Windows 에만 있습니다") }
	t.Cleanup(func() { openXLNoter = was })
	_, handled, err := xlNotesFallback(context.Background(), &notesHand{}, "wb-book-1", "add_comment", map[string]any{"address": "A2", "text": "x"}, "NotImplemented")
	if !handled || err == nil || !strings.Contains(err.Error(), "노트로 대신할 길도 이 머신에는 없습니다") || !strings.Contains(err.Error(), "Windows") {
		t.Errorf("길이 없으면 둘 다 없다고 말한다: handled=%v err=%v", handled, err)
	}
}

// 헬퍼의 도구 길이 실제로 이 우회를 탄다 — 손이 NotImplemented 로 죽은 add_comment 가 노트 답으로 돌아온다.
func TestMCPCallFallsBackToNotesForExcelComments(t *testing.T) {
	withFakeNoter(t)
	srv := &MCPServer{App: XL, Hand: &notesHand{}}
	req := httptest.NewRequest("POST", "/xl/mcp?deck=wb-book-1", nil)
	out := srv.call(req, "add_comment", json.RawMessage(`{"address":"B3","text":"확인"}`))
	if out["isError"] == true {
		t.Fatalf("노트로 대신해야 하는데 오류다: %v", out)
	}
	text := out["content"].([]map[string]any)[0]["text"].(string)
	if !strings.Contains(text, `"kind": "note"`) || !strings.Contains(text, "노트를 넣었습니다") || !strings.Contains(text, `"document": "wb-book-1"`) {
		t.Errorf("답이 노트라고 말하고 문서를 싣는다: %s", text)
	}
	// 손이 다른 이유로 죽으면 그 말이 그대로 간다.
	bad := &fakeHand{attached: true, err: errors.New("InvalidArgument — Range.address")}
	out = (&MCPServer{App: XL, Hand: bad}).call(req, "add_comment", json.RawMessage(`{"address":"B3","text":"확인"}`))
	if out["isError"] != true || !strings.Contains(out["content"].([]map[string]any)[0]["text"].(string), "InvalidArgument") {
		t.Errorf("다른 오류는 손의 말 그대로: %v", out)
	}
}
