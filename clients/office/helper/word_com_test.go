package office

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeWordDoc 는 COM 없이 재는 문서 — 문단 수와 메모·책갈피·각주를 메모리에 든다.
type fakeWordDoc struct {
	id        string
	paras     int
	comments  []wordComment
	bookmarks map[string][2]int
	notes     []wordNote
	closed    int

	tracking   bool
	revisions  []wordRevision
	sections   int
	pageSetups []wordPageSetup
	styleCalls []string

	shapes     []wordShape
	lastMso    int
	tables     int
	tableEdits []wordTableEdit
	fieldCalls []wordFieldWhere
	vars       map[string]string
}

func (f *fakeWordDoc) Paragraphs() (int, error) { return f.paras, nil }
func (f *fakeWordDoc) AddComment(from, to int, anchor, text string) (string, error) {
	if anchor == "없는글" {
		return "", fmt.Errorf("문단 %d–%d 에 「%s」 가 없습니다", from, to, anchor)
	}
	id := fmt.Sprintf("c%d-abcdef", len(f.comments)+1)
	f.comments = append(f.comments, wordComment{ID: id, Author: "나", Text: text, Para: from, On: anchor})
	return id, nil
}
func (f *fakeWordDoc) Comments(from, to int) ([]wordComment, error) {
	var out []wordComment
	for _, c := range f.comments {
		if c.Para >= from && c.Para <= to {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeWordDoc) at(id string) (*wordComment, error) {
	for i := range f.comments {
		if f.comments[i].ID == id {
			return &f.comments[i], nil
		}
	}
	return nil, fmt.Errorf("id %s 인 메모가 없습니다", id)
}
func (f *fakeWordDoc) ReplyComment(id, text string) error {
	c, err := f.at(id)
	if err != nil {
		return err
	}
	c.Replies = append(c.Replies, wordReply{Author: "나", Text: text})
	return nil
}
func (f *fakeWordDoc) ResolveComment(id string, resolved, del bool) error {
	c, err := f.at(id)
	if err != nil {
		return err
	}
	c.Resolved = resolved
	return nil
}
func (f *fakeWordDoc) AddBookmark(from, to int, name string) error {
	if f.bookmarks == nil {
		f.bookmarks = map[string][2]int{}
	}
	f.bookmarks[name] = [2]int{from, to}
	return nil
}
func (f *fakeWordDoc) DeleteBookmark(name string) (bool, error) {
	_, had := f.bookmarks[name]
	delete(f.bookmarks, name)
	return had, nil
}
func (f *fakeWordDoc) InsertNote(kind string, para int, anchor, text string) (int, error) {
	n := 1
	for _, x := range f.notes {
		if x.Kind == kind {
			n++
		}
	}
	f.notes = append(f.notes, wordNote{Number: n, Kind: kind, Para: para, On: anchor, Text: text})
	return n, nil
}
func (f *fakeWordDoc) Notes() ([]wordNote, error) { return f.notes, nil }
func (f *fakeWordDoc) DeleteNote(kind string, number int) error {
	for i, x := range f.notes {
		if x.Kind == kind && x.Number == number {
			f.notes = append(f.notes[:i], f.notes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%s %d번이 없습니다", wcNoteKo(kind), number)
}
func (f *fakeWordDoc) Close() { f.closed++ }

// wordRefusingHand 는 2021 의 작업창 — 문단 목록은 주고, 1.4 이상 도구는 창의 문장 그대로 거절한다.
type wordRefusingHand struct {
	doc   string
	calls []string
}

func (h *wordRefusingHand) Attached() bool { return true }
func (h *wordRefusingHand) Call(_ context.Context, _, op string, _ map[string]any) (HandResult, error) {
	h.calls = append(h.calls, op)
	if op == "list_paragraphs" {
		return HandResult{Document: h.doc, Label: "보고서.docx", Result: map[string]any{"count": 3}}, nil
	}
	return HandResult{}, fmt.Errorf("%s 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다", op)
}

func withFakeWordDoc(t *testing.T, paras int) (*fakeWordDoc, *[]string) {
	t.Helper()
	f := &fakeWordDoc{paras: paras}
	var opened []string
	wasOpen, wasOS := openWordDoc, wordComOnThisOS
	openWordDoc = func(id string) (wordDoc, error) { opened = append(opened, id); f.id = id; return f, nil }
	wordComOnThisOS = true
	t.Cleanup(func() { openWordDoc, wordComOnThisOS = wasOpen, wasOS })
	return f, &opened
}

// 이 길은 **버전을 이유로 한 거절**에만, **대신할 줄 아는 도구**에만 탄다. 그 밖의 오류는 창의 말 그대로 간다 —
// 인자가 틀렸다는 거절을 COM 으로 「고쳐서」 해 버리면 틀린 인자가 통과한다.
func TestWordComFallbackTakesOnlyVersionRefusalsOfToolsItKnows(t *testing.T) {
	_, opened := withFakeWordDoc(t, 3)
	hand := &wordRefusingHand{doc: "wd-doc-1"}
	ctx := context.Background()
	if _, handled, _ := wordComFallback(ctx, hand, "", "insert_paragraphs", map[string]any{}, "insert_paragraphs 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다"); handled {
		t.Error("아직 이 길이 모르는 도구를 대신했다")
	}
	if _, handled, _ := wordComFallback(ctx, hand, "", "add_comment", map[string]any{"from": 1, "comment": "x"}, "문서에 9번 문단이 없습니다 — 문단 3개"); handled {
		t.Error("버전이 아닌 거절을 대신했다")
	}
	if len(*opened) != 0 {
		t.Errorf("길이 아닌데 COM 을 열었다: %v", *opened)
	}
}

// 같은 문서인지는 창이 알려 준 열쇠(wd- + MAGI.DOC)로 가른다 — 그 열쇠의 id 로만 COM 문서를 연다.
func TestWordComFallbackOpensTheDocumentThePaneStamped(t *testing.T) {
	f, opened := withFakeWordDoc(t, 3)
	hand := &wordRefusingHand{doc: "wd-doc-7f3a"}
	res, handled, err := wordComFallback(context.Background(), hand, "", "add_comment",
		map[string]any{"from": 2, "comment": "근거 필요"}, "add_comment 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다")
	if !handled || err != nil {
		t.Fatalf("대신하지 못했다: handled=%v err=%v", handled, err)
	}
	if len(*opened) != 1 || (*opened)[0] != "doc-7f3a" {
		t.Fatalf("표식이 아닌 것으로 문서를 열었다: %v", *opened)
	}
	if res.Document != "wd-doc-7f3a" || res.Label != "보고서.docx" {
		t.Errorf("답이 창의 문서를 가리키지 않는다: %q %q", res.Document, res.Label)
	}
	if res.Result["via"] != "com" {
		t.Errorf("COM 으로 했다는 사실을 숨겼다: %v", res.Result)
	}
	if len(res.Changed) != 1 || !strings.Contains(res.Changed[0], "문단 2 에 메모를 달았습니다") {
		t.Errorf("한 일을 말하지 않았다: %v", res.Changed)
	}
	if len(f.comments) != 1 || f.comments[0].Para != 2 {
		t.Errorf("메모가 안 섰다: %+v", f.comments)
	}
	if f.closed != 1 {
		t.Errorf("연 COM 문서를 안 닫았다: %d", f.closed)
	}
}

// 표식을 모르면 가지 않는다 — 「열린 문서가 하나뿐」으로 고르면 다른 문서를 고친다.
func TestWordComFallbackRefusesWithoutTheStamp(t *testing.T) {
	_, opened := withFakeWordDoc(t, 3)
	for _, doc := range []string{"", "doc-without-prefix"} {
		hand := &wordRefusingHand{doc: doc}
		_, handled, err := wordComFallback(context.Background(), hand, "", "read_comments", map[string]any{}, "read_comments 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다")
		if !handled || err == nil || !strings.Contains(err.Error(), "MAGI.DOC") {
			t.Errorf("키 %q: 표식 없이 갔거나 이유를 안 댔다: handled=%v err=%v", doc, handled, err)
		}
	}
	if len(*opened) != 0 {
		t.Errorf("표식 없이 COM 문서를 열었다: %v", *opened)
	}
}

// Windows 가 아니면 이 길은 없다 — 창의 거절이 그대로 간다.
func TestWordComFallbackStaysOutWhereThereIsNoCom(t *testing.T) {
	_, opened := withFakeWordDoc(t, 3)
	wordComOnThisOS = false
	_, handled, _ := wordComFallback(context.Background(), &wordRefusingHand{doc: "wd-doc-1"}, "", "add_bookmark",
		map[string]any{"from": 1, "name": "a"}, "add_bookmark 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다")
	if handled || len(*opened) != 0 {
		t.Errorf("COM 이 없는 곳에서 대신하려 했다: handled=%v opened=%v", handled, *opened)
	}
}

// 인자 검사와 답의 모양은 창과 같다 — 모델이 어느 길로 됐는지 신경 쓰지 않아도 되게.
func TestWordComRunMatchesThePaneOnCommentsBookmarksAndNotes(t *testing.T) {
	d := &fakeWordDoc{paras: 3}

	// 메모: 달기 · 글 앵커 · 없는 글 · 범위 밖 · 읽기 · 답글 · 해결
	if _, _, err := wordComRun(d, "add_comment", map[string]any{"from": 1}); err == nil {
		t.Error("comment 없이 메모를 달았다")
	}
	if _, _, err := wordComRun(d, "add_comment", map[string]any{"comment": "x"}); err == nil || !strings.Contains(err.Error(), "from") {
		t.Errorf("from 없이 갔거나 이유가 다르다: %v", err)
	}
	if _, _, err := wordComRun(d, "add_comment", map[string]any{"from": 9, "comment": "x"}); err == nil || !strings.Contains(err.Error(), "문단 3개") {
		t.Errorf("없는 문단에 메모를 달았거나 문단 수를 안 댔다: %v", err)
	}
	if _, _, err := wordComRun(d, "add_comment", map[string]any{"from": 1, "text": "없는글", "comment": "x"}); err == nil {
		t.Error("없는 글에 메모를 달았다")
	}
	res, changed, err := wordComRun(d, "add_comment", map[string]any{"from": 2, "text": "요약", "comment": "근거는?"})
	if err != nil || res["id"] == "" || !strings.Contains(changed[0], "문단 2 의 「요약」 에 메모를 달았습니다") {
		t.Fatalf("글 앵커 메모: %v %v %v", res, changed, err)
	}
	id := res["id"].(string)
	if _, changed, err = wordComRun(d, "reply_comment", map[string]any{"id": id, "text": "3쪽 표"}); err != nil || !strings.Contains(changed[0], "답글") {
		t.Fatalf("답글: %v %v", changed, err)
	}
	if res, _, err = wordComRun(d, "resolve_comment", map[string]any{"id": id}); err != nil || res["resolved"] != true || res["deleted"] != false {
		t.Fatalf("해결: %v %v", res, err)
	}
	res, _, err = wordComRun(d, "read_comments", map[string]any{})
	if err != nil || res["count"] != 1 {
		t.Fatalf("읽기: %v %v", res, err)
	}
	row := res["comments"].([]any)[0].(map[string]any)
	if row["resolved"] != true || len(row["replies"].([]any)) != 1 {
		t.Errorf("읽은 메모에 해결·답글이 안 실렸다: %v", row)
	}
	if res, _, _ = wordComRun(d, "read_comments", map[string]any{"from": 3}); res["count"] != 0 {
		t.Errorf("범위 밖 메모를 읽었다: %v", res)
	}

	// 책갈피: 이름 규칙 · 넣기 · 없는 것 지우기 · 지우기
	if _, _, err := wordComRun(d, "add_bookmark", map[string]any{"from": 1, "name": "1번"}); err == nil {
		t.Error("규칙에 안 맞는 책갈피 이름을 받았다")
	}
	if _, changed, err := wordComRun(d, "add_bookmark", map[string]any{"from": 1, "to": 2, "name": "intro_1"}); err != nil || !strings.Contains(changed[0], "문단 1–2 에 책갈피 「intro_1」") {
		t.Fatalf("책갈피: %v %v", changed, err)
	}
	if _, _, err := wordComRun(d, "delete_bookmark", map[string]any{"name": "nope"}); err == nil {
		t.Error("없는 책갈피를 지웠다고 했다")
	}
	if _, _, err := wordComRun(d, "delete_bookmark", map[string]any{"name": "intro_1"}); err != nil {
		t.Errorf("책갈피 지우기: %v", err)
	}

	// 각주·미주: 넣기 · 종류 · 없는 문단 · 읽기(범위) · 지우기
	if _, _, err := wordComRun(d, "insert_footnote", map[string]any{"paragraph": 2}); err == nil {
		t.Error("note 없이 각주를 달았다")
	}
	if _, _, err := wordComRun(d, "insert_footnote", map[string]any{"paragraph": 7, "note": "x"}); err == nil {
		t.Error("없는 문단에 각주를 달았다")
	}
	if _, _, err := wordComRun(d, "insert_footnote", map[string]any{"paragraph": 1, "note": "x", "kind": "sidenote"}); err == nil {
		t.Error("모르는 종류를 받았다")
	}
	res, changed, err = wordComRun(d, "insert_footnote", map[string]any{"from": 2, "text": "요약", "note": "출처: 사내 자료"})
	if err != nil || res["number"] != 1 || res["kind"] != "footnote" || !strings.Contains(changed[0], "문단 2 의 「요약」 에 각주를 달았습니다") {
		t.Fatalf("각주: %v %v %v", res, changed, err)
	}
	if _, _, err = wordComRun(d, "insert_footnote", map[string]any{"paragraph": 3, "note": "미주 하나", "kind": "endnote"}); err != nil {
		t.Fatalf("미주: %v", err)
	}
	if res, _, _ = wordComRun(d, "read_footnotes", map[string]any{"from": 2}); res["count"] != 1 {
		t.Errorf("범위로 걸러 읽지 않았다: %v", res)
	}
	if _, _, err = wordComRun(d, "delete_footnote", map[string]any{"number": 1}); err != nil {
		t.Errorf("각주 지우기: %v", err)
	}
	if _, _, err = wordComRun(d, "delete_footnote", map[string]any{"number": 1}); err == nil {
		t.Error("없는 각주를 지웠다고 했다")
	}
}

// 창이 거절 문장을 바꾸면 이 길이 조용히 죽는다 — 그 문장을 창의 소스에서 읽어 맞춰 둔다.
func TestTheWordFallbackRecognisesThePanesOwnRefusal(t *testing.T) {
	b, err := os.ReadFile("../../word/addin/src/adapter/WordHand.js")
	src := string(b)
	if err != nil {
		t.Fatalf("창의 소스를 못 읽었다(%v) — 자리가 바뀌었으면 이 경로를 같이 고친다", err)
	}
	if !strings.Contains(src, "이 필요한데 이 호스트에는 없습니다") {
		t.Fatal("창의 버전 거절 문장이 바뀌었다 — wordNeedsAPI 를 같이 고친다")
	}
	for _, s := range []string{
		"add_comment 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다",
		"insert_footnote 은 WordApi 1.5 이 필요한데 이 호스트에는 없습니다",
		"set_page_setup 은 WordApiDesktop 1.1 이 필요한데 이 호스트에는 없습니다",
	} {
		if !wordNeedsAPI.MatchString(s) {
			t.Errorf("창의 거절을 못 알아본다: %s", s)
		}
	}
	if wordNeedsAPI.MatchString(errors.New("문서에 9번 문단이 없습니다").Error()) {
		t.Error("버전이 아닌 거절을 버전 거절로 읽는다")
	}
}

// COM 의 날짜는 지역 시각이다 — 서울에서 17:19 에 단 메모는 08:19Z 다.
func TestComDatesAreReadAsLocalTime(t *testing.T) {
	was := time.Local
	time.Local = time.FixedZone("KST", 9*3600)
	t.Cleanup(func() { time.Local = was })
	naive := time.Date(2026, 9, 26, 17, 19, 0, 0, time.UTC) // go-ole 이 붙여 주는 모양
	if got := comLocalToUTC(naive).Format(time.RFC3339); got != "2026-09-26T08:19:00Z" {
		t.Fatalf("지역 시각을 UTC 로 옮기지 못했다: %s", got)
	}
}

// ── 둘째 묶음: 변경 추적 · 쪽 설정 · 스타일 서식 ──

func (f *fakeWordDoc) Tracking() (bool, error) { return f.tracking, nil }
func (f *fakeWordDoc) SetTracking(on bool) error {
	f.tracking = on
	return nil
}
func (f *fakeWordDoc) Revisions(from, to int, whole bool) ([]wordRevision, error) {
	return f.revisions, nil
}
func (f *fakeWordDoc) Review(accept bool, from, to int, whole bool) (int, error) {
	n := len(f.revisions)
	f.revisions = nil
	return n, nil
}
func (f *fakeWordDoc) Sections() (int, error) {
	if f.sections == 0 {
		return 1, nil
	}
	return f.sections, nil
}
func (f *fakeWordDoc) PageSetup(section int, s wordPageSetup) (int, error) {
	f.pageSetups = append(f.pageSetups, s)
	if section > 0 {
		return 1, nil
	}
	return f.Sections()
}
func (f *fakeWordDoc) StyleFormat(local string, builtin int, create bool, sf wordStyleFormat) (string, int, bool, error) {
	f.styleCalls = append(f.styleCalls, fmt.Sprintf("%s/%d/%v", local, builtin, create))
	if builtin != 0 {
		return "제목 2", 1, false, nil
	}
	if local == "사내 본문" || create {
		return local, 0, create && local != "사내 본문", nil
	}
	return "", 0, false, fmt.Errorf("「%s」 스타일이 없습니다", local)
}

// 둘째 묶음도 인자 검사와 답의 모양이 창과 같다. 숫자는 **와이어에서 오는 모양(json.Number)** 으로 넣는다 — float64 로
// 넣던 때는 실물에서 숫자가 전부 빠지는 결함을 이 시험이 못 봤다(2026-09-26).
func TestWordComRunMatchesThePaneOnTrackingPageSetupAndStyles(t *testing.T) {
	d := &fakeWordDoc{paras: 3}

	// 변경 추적
	if _, _, err := wordComRun(d, "set_track_changes", map[string]any{"mode": "TrackMineOnly"}); err == nil || !strings.Contains(err.Error(), "TrackAll") {
		t.Errorf("2021 에 없는 「내 것만」을 받았거나 대안을 안 댔다: %v", err)
	}
	if _, changed, err := wordComRun(d, "set_track_changes", map[string]any{"mode": "TrackAll"}); err != nil || changed[0] != "변경 추적 → TrackAll" || !d.tracking {
		t.Fatalf("추적 켜기: %v %v", changed, err)
	}
	d.revisions = []wordRevision{{Type: "Added", Author: "나", Text: "추가"}, {Type: "Deleted", Author: "나", Text: "뺌"}}
	res, _, err := wordComRun(d, "read_tracked_changes", map[string]any{"limit": json.Number("1")})
	if err != nil || res["count"] != 2 || res["mode"] != "TrackAll" || len(res["changes"].([]any)) != 1 {
		t.Fatalf("변경 읽기(limit 은 목록만 자르고 수는 다 센다): %v %v", res, err)
	}
	if _, _, err := wordComRun(d, "review_changes", map[string]any{"what": "maybe"}); err == nil {
		t.Error("accept·reject 가 아닌 것을 받았다")
	}
	if _, changed, err := wordComRun(d, "review_changes", map[string]any{"what": "accept", "from": 1, "to": 2}); err != nil || changed[0] != "변경 2건을 수락했습니다 (문단 1–2)" {
		t.Fatalf("수락: %v %v", changed, err)
	}

	// 쪽 설정
	if _, _, err := wordComRun(d, "set_page_setup", map[string]any{}); err == nil {
		t.Error("바꿀 것 없이 쪽 설정을 했다")
	}
	if _, _, err := wordComRun(d, "set_page_setup", map[string]any{"paper": "B6"}); err == nil {
		t.Error("모르는 용지를 받았다")
	}
	if _, _, err := wordComRun(d, "set_page_setup", map[string]any{"section": 3, "orientation": "Landscape"}); err == nil || !strings.Contains(err.Error(), "구역 1개") {
		t.Errorf("없는 구역에 걸었거나 구역 수를 안 댔다: %v", err)
	}
	_, changed, err := wordComRun(d, "set_page_setup", map[string]any{"orientation": "Landscape", "paper": "A4", "margins": map[string]any{"top": json.Number("36"), "left": json.Number("54")}, "different_first_page": true})
	if err != nil || changed[0] != "구역 1개 쪽 설정: 가로, 용지 A4, 여백 left 54pt/top 36pt, 첫 쪽 따로" {
		t.Fatalf("쪽 설정: %v %v", changed, err)
	}

	// 스타일 서식: 내장 이름은 번호로 간다
	if _, _, err := wordComRun(d, "set_style_format", map[string]any{"style": "Heading2"}); err == nil {
		t.Error("바꿀 것 없이 스타일을 고쳤다")
	}
	if _, _, err := wordComRun(d, "set_style_format", map[string]any{"style": "Heading2", "color": "blue"}); err == nil {
		t.Error("#RRGGBB 가 아닌 색을 받았다")
	}
	res, changed, err = wordComRun(d, "set_style_format", map[string]any{"style": "heading 2", "size": json.Number("16"), "bold": true, "color": "#1F4E79"})
	if err != nil || res["builtin"] != "Heading2" || res["style"] != "제목 2" || changed[0] != "스타일 「제목 2」: 크기 16, 굵게, 색 #1F4E79 — 문단 1개에 걸립니다" {
		t.Fatalf("내장 스타일: %v %v %v", res, changed, err)
	}
	if d.styleCalls[len(d.styleCalls)-1] != "heading 2/-3/false" {
		t.Errorf("내장 이름을 번호로 안 바꿨다: %v", d.styleCalls)
	}
	if _, _, err := wordComRun(d, "set_style_format", map[string]any{"style": "없는 스타일", "size": json.Number("11")}); err == nil {
		t.Error("없는 스타일을 고쳤다고 했다")
	}
}

// 이 길의 내장 스타일 표와 도구 스키마의 내장 이름 목록은 같은 이름들이어야 한다 — 한쪽에만 있으면 창은 광고하고
// COM 은 모르는 이름이 생긴다.
func TestComBuiltinStylesCoverTheAdvertisedOnes(t *testing.T) {
	for _, n := range wordBuiltinStyles {
		if _, ok := wordBuiltinCOM[n]; !ok {
			t.Errorf("광고된 내장 스타일 %s 를 COM 길이 모른다", n)
		}
	}
	if len(wordBuiltinCOM) != len(wordBuiltinStyles) {
		t.Errorf("COM 표 %d개 ≠ 광고 %d개", len(wordBuiltinCOM), len(wordBuiltinStyles))
	}
	for _, p := range wordPapers {
		if _, ok := wordPaperCOM[p]; !ok {
			t.Errorf("광고된 용지 %s 를 COM 길이 모른다", p)
		}
	}
}
