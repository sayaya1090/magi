package office

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// 워드 2021(볼륨 판)의 작업창은 WordApi 1.3 까지만 있다. 그래서 메모·책갈피·각주처럼 1.4 이상을 요구하는 도구는
// 창의 손이 「… 은 WordApi 1.4 이 필요한데 이 호스트에는 없습니다」로 거절해 왔다 — 도구 23개가 그 판에서 죽어 있었다.
//
// ⚠ **그런데 같은 기능이 COM 에는 다 있다.** 실측 2026-09-26(LTSC 2021, 같은 머신): 메모 달기·답글·해결, 책갈피,
// 각주 넣기·읽기·지우기, 필드, 변경 추적, 쪽 설정, 도형, 스타일 서식, 표 병합이 COM 으로 전부 됐다. 없는 것은 Word 의
// 기능이 아니라 **Office.js 의 그 판**이었다. 엑셀 2021 의 메모를 COM 노트로 대신하는 길(xl_notes.go)과 같은 모양으로,
// 창이 버전을 이유로 거절하면 헬퍼가 열린 Word 를 COM 으로 잡아 대신한다 — Windows 에서만.
//
// **같은 문서인지는 표식으로 가른다.** 창은 문서마다 사용자 지정 속성 MAGI.DOC 에 id 를 새기고(OfficeDocument.js),
// 헬퍼가 그 문서를 부르는 열쇠가 `wd-` + 그 id 다. COM 은 열린 문서들 가운데 그 값을 가진 것만 만진다 — 이름이나
// 「열린 것이 하나」로 고르지 않는다. 저장 안 한 문서는 이름이 「문서1」이라 고를 근거가 못 되고, GetActiveObject 가
// 잡는 Word 가 창의 것과 다른 인스턴스일 수도 있다(xl_notes.go 가 같은 이유로 통장 이름 없이는 안 간다).

// wordNeedsAPI 는 창의 손이 버전 때문에 거절했다는 표시 — WordHand.js 의 #need 가 내는 문장 그대로다.
var wordNeedsAPI = regexp.MustCompile(`은 WordApi(?:Desktop)? [0-9.]+ 이 필요한데 이 호스트에는 없습니다`)

// wordComment 는 메모 하나. 답글은 붙어서 온다.
type wordComment struct {
	ID       string
	Author   string
	Date     string
	On       string // 메모가 걸린 글
	Text     string
	Resolved bool
	Para     int // 걸린 문단(1부터). 모르면 0
	Replies  []wordReply
}

type wordReply struct {
	Author string
	Date   string
	Text   string
}

// wordNote 는 각주·미주 하나.
type wordNote struct {
	Number int
	Kind   string // footnote · endnote
	Para   int
	On     string
	Text   string
}

// wordRevision 은 변경 추적의 변경 하나.
type wordRevision struct {
	Type   string // Added · Deleted · Formatted — 창(Office.js)의 이름 그대로
	Author string
	Date   string
	Text   string
}

// wordPageSetup 은 쪽 설정에서 바꿀 것 — 비어 있는 칸은 안 건드린다.
type wordPageSetup struct {
	Orientation    string // Portrait · Landscape
	Paper          string // A4 …
	Margins        map[string]float64
	HeaderDist     *float64
	FooterDist     *float64
	DifferentFirst *bool
}

// wordStyleFormat 은 스타일에 걸 서식 — 비어 있는 칸은 안 건드린다.
type wordStyleFormat struct {
	Font, Color, Align                                                      string
	Size, SpaceBefore, SpaceAfter, LineSpacing, FirstLineIndent, LeftIndent *float64
	Bold, Italic                                                            *bool
}

// wordBuiltinCOM 은 창의 내장 스타일 이름(word_enums.go 의 wordBuiltinStyles)(handCore.js BUILTIN_PARAGRAPH_STYLES) → COM 의 WdBuiltinStyle.
//
// ⚠ **한국어 Word 에서 COM 은 영문 이름을 모른다.** `Styles("Heading 2")` 는 실패하고 `Styles(-3)` 이 「제목 2」를
// 준다(실측 2026-09-26, LTSC 2021 ko-kr — 스물여덟 개를 하나씩 불러 현지 이름을 확인했다). 그래서 이름이 아니라 번호로 간다.
var wordBuiltinCOM = map[string]int{
	"Normal": -1, "Title": -63, "Subtitle": -75,
	"Heading1": -2, "Heading2": -3, "Heading3": -4, "Heading4": -5, "Heading5": -6, "Heading6": -7, "Heading7": -8, "Heading8": -9, "Heading9": -10,
	"Quote": -181, "IntenseQuote": -182, "ListParagraph": -180, "Caption": -35, "NoSpacing": -158, "TocHeading": -267,
	"Toc1": -20, "Toc2": -21, "Toc3": -22,
	"Emphasis": -89, "Strong": -88, "SubtleEmphasis": -261, "IntenseEmphasis": -262, "SubtleReference": -263, "IntenseReference": -264, "BookTitle": -265,
}

// wordBuiltinOf 는 창과 같은 규칙(공백·밑줄·붙임표를 빼고 대소문자 무시)으로 내장 이름을 찾는다.
func wordBuiltinOf(given string) (name string, id int) {
	key := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(given))
	for n, v := range wordBuiltinCOM {
		if strings.ToLower(n) == key {
			return n, v
		}
	}
	return "", 0
}

// wordDoc 은 COM 으로 잡은 **그 문서** 하나 — Windows 에서는 COM(word_com_windows.go), 시험에서는 가짜.
// 문단 번호는 창과 같은 본문 문단 번호(1부터)다.
type wordDoc interface {
	Paragraphs() (int, error)

	AddComment(from, to int, anchor, text string) (id string, err error)
	Comments(from, to int) ([]wordComment, error)
	ReplyComment(id, text string) error
	ResolveComment(id string, resolved, del bool) error

	AddBookmark(from, to int, name string) error
	DeleteBookmark(name string) (existed bool, err error)

	InsertNote(kind string, para int, anchor, text string) (number int, err error)
	Notes() ([]wordNote, error)
	DeleteNote(kind string, number int) error

	Tracking() (on bool, err error)
	SetTracking(on bool) error
	Revisions(from, to int, whole bool) ([]wordRevision, error)
	Review(accept bool, from, to int, whole bool) (count int, err error)

	Sections() (int, error)
	PageSetup(section int, s wordPageSetup) (sections int, err error)

	// StyleFormat 은 builtin 이 0 이 아니면 그 내장 스타일을, 아니면 현지 이름 local 인 스타일을 고친다.
	StyleFormat(local string, builtin int, create bool, f wordStyleFormat) (name string, affected int, created bool, err error)

	Close()
}

// openWordDoc 은 MAGI.DOC 가 id 인 문서를 잡는다 — 플랫폼이 정하고, 시험은 가짜로 바꿔 끼운다.
var openWordDoc = openWordDocOS

// wordComOnThisOS — COM 은 Windows 에만 있다.
var wordComOnThisOS = runtime.GOOS == "windows"

// wordComTools 는 이 길이 대신할 수 있는 도구들. 나머지는 창의 거절이 그대로 간다.
var wordComTools = map[string]bool{
	"add_comment": true, "read_comments": true, "reply_comment": true, "resolve_comment": true,
	"add_bookmark": true, "delete_bookmark": true,
	"insert_footnote": true, "read_footnotes": true, "delete_footnote": true,
	"set_track_changes": true, "read_tracked_changes": true, "review_changes": true,
	"set_page_setup": true, "set_style_format": true,
	// 제안(suggest·read_suggestions·drop_suggestion)은 일부러 뺐다: 작업창 화면이 제 손(Office.js)으로 직접 읽어 「적용」
	// 단추로 보여 주는 것이라, COM 으로 문서에 적어 두어도 2021 의 창은 못 읽는다. 모델은 「붙였습니다」라 하고 사람은 아무것도
	// 못 보는 일이 된다 — 거절이 정직하다.
}

// wordComVia 는 답에 실려 「창이 아니라 COM 으로 했다」를 알린다. 결과는 같지만 **길이 다르다는 사실은 숨기지 않는다** —
// 무엇이 안 되면 어디를 봐야 하는지가 거기서 갈린다.
const wordComVia = "com"

// wordComFallback 은 창의 손이 WordApi 버전을 이유로 거절한 도구를 COM 으로 대신한다.
// 두 번째 값이 false 면 이 길이 아니다 — 원래 오류가 그대로 간다.
func wordComFallback(ctx context.Context, hand Hand, where, name string, args map[string]any, handErr string) (HandResult, bool, error) {
	if !wordComTools[name] || !wordNeedsAPI.MatchString(handErr) {
		return HandResult{}, false, nil
	}
	if !wordComOnThisOS {
		return HandResult{}, false, nil
	}

	// 어느 문서인지는 창에게 묻는다 — 답의 Document 가 `wd-` + MAGI.DOC 다. 짐작하지 않는다.
	probe, perr := hand.Call(ctx, where, "list_paragraphs", map[string]any{"from": 1, "to": 1})
	if perr != nil {
		return HandResult{}, true, fmt.Errorf("%s: 이 Word 판의 작업창은 이 기능이 없어(%s) COM 으로 대신하려 했는데, 어느 문서인지 창이 답하지 못했습니다(%v)", name, strings.TrimSpace(handErr), perr)
	}
	docKey := probe.Document
	if docKey == "" {
		docKey = where
	}
	id := strings.TrimPrefix(docKey, "wd-")
	if id == "" || id == docKey {
		return HandResult{}, true, fmt.Errorf("%s: 이 Word 판의 작업창은 이 기능이 없어 COM 으로 대신하려 했는데, 문서 표식(MAGI.DOC)을 모릅니다(키 %q) — 창을 다시 열면 새겨집니다", name, docKey)
	}
	d, err := openWordDoc(id)
	if err != nil {
		return HandResult{}, true, fmt.Errorf("%s: 이 Word 판의 작업창은 이 기능이 없어 COM 으로 대신하려 했는데 못 했습니다 — %v", name, err)
	}
	defer d.Close()

	res, changed, err := wordComRun(d, name, args)
	if err != nil {
		return HandResult{}, true, fmt.Errorf("%s: %v", name, err)
	}
	res["via"] = wordComVia
	return HandResult{Document: docKey, Label: probe.Label, Result: res, Changed: changed}, true, nil
}

// wordComRun 은 도구 하나를 COM 문서에 건다 — 인자 검사와 답의 모양은 창(WordHand.js)과 같게 둔다. 모델이 어느
// 길로 됐는지 신경 쓰지 않아도 되게.
func wordComRun(d wordDoc, name string, args map[string]any) (map[string]any, []string, error) {
	switch name {
	case "add_comment":
		text := wcStr(args, "comment")
		if text == "" {
			return nil, nil, fmt.Errorf("comment 가 없습니다")
		}
		from, to, said, err := wcRange(d, args, true)
		if err != nil {
			return nil, nil, err
		}
		anchor := wcStr(args, "text")
		if anchor != "" {
			said = fmt.Sprintf("%s 의 「%s」", said, wcClip(anchor, 30))
		}
		id, err := d.AddComment(from, to, anchor, text)
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"id": id, "on": said}, []string{fmt.Sprintf("%s 에 메모를 달았습니다 — 「%s」", said, wcClip(text, 40))}, nil

	case "read_comments":
		from, to, _, err := wcRange(d, args, false)
		if err != nil {
			return nil, nil, err
		}
		cs, err := d.Comments(from, to)
		if err != nil {
			return nil, nil, err
		}
		rows := make([]any, 0, len(cs))
		for _, c := range cs {
			reps := make([]any, 0, len(c.Replies))
			for _, r := range c.Replies {
				reps = append(reps, map[string]any{"author": r.Author, "date": r.Date, "text": r.Text})
			}
			rows = append(rows, map[string]any{"id": c.ID, "author": c.Author, "date": c.Date, "on": wcClip(c.On, 80), "text": c.Text, "resolved": c.Resolved, "replies": reps})
		}
		return map[string]any{"from": from, "to": to, "count": len(rows), "comments": rows}, nil, nil

	case "reply_comment":
		id, text := wcStr(args, "id"), wcStr(args, "text")
		if id == "" || text == "" {
			return nil, nil, fmt.Errorf("id 와 text 가 있어야 합니다 — read_comments 가 id 를 줍니다")
		}
		if err := d.ReplyComment(id, text); err != nil {
			return nil, nil, err
		}
		return map[string]any{"id": id}, []string{fmt.Sprintf("메모 %s 에 답글 — 「%s」", id, wcClip(text, 40))}, nil

	case "resolve_comment":
		id := wcStr(args, "id")
		if id == "" {
			return nil, nil, fmt.Errorf("id 가 없습니다 — read_comments 가 id 를 줍니다")
		}
		del := wcBool(args, "delete", false)
		resolved := wcBool(args, "resolved", true)
		if err := d.ResolveComment(id, resolved, del); err != nil {
			return nil, nil, err
		}
		said := fmt.Sprintf("메모 %s 를 해결로 표시했습니다", id)
		switch {
		case del:
			said = fmt.Sprintf("메모 %s 를 지웠습니다", id)
		case !resolved:
			said = fmt.Sprintf("메모 %s 의 해결 표시를 풀었습니다", id)
		}
		return map[string]any{"id": id, "deleted": del, "resolved": !del && resolved}, []string{said}, nil

	case "add_bookmark":
		name := wcStr(args, "name")
		if !wordBookmarkName.MatchString(name) {
			return nil, nil, fmt.Errorf("책갈피 이름은 영문자로 시작하는 영문·숫자·밑줄 40자 이내 — %s", name)
		}
		from, to, said, err := wcRange(d, args, true)
		if err != nil {
			return nil, nil, err
		}
		if err := d.AddBookmark(from, to, name); err != nil {
			return nil, nil, err
		}
		return map[string]any{"name": name, "from": from, "to": to}, []string{fmt.Sprintf("%s 에 책갈피 「%s」", said, name)}, nil

	case "delete_bookmark":
		name := wcStr(args, "name")
		if name == "" {
			return nil, nil, fmt.Errorf("name 이 없습니다")
		}
		had, err := d.DeleteBookmark(name)
		if err != nil {
			return nil, nil, err
		}
		if !had {
			return nil, nil, fmt.Errorf("책갈피 「%s」 가 없습니다", name)
		}
		return map[string]any{"name": name, "deleted": true}, []string{fmt.Sprintf("책갈피 「%s」 를 지웠습니다 — 글은 그대로입니다", name)}, nil

	case "insert_footnote":
		note := wcStr(args, "note")
		if note == "" {
			return nil, nil, fmt.Errorf("note 가 없습니다 — 각주 글")
		}
		kind, err := wcNoteKind(args)
		if err != nil {
			return nil, nil, err
		}
		para := wcInt(args, "paragraph")
		if para == 0 {
			para = wcInt(args, "from")
		}
		if para == 0 {
			return nil, nil, fmt.Errorf("paragraph 가 없습니다 — 각주가 걸릴 문단 번호")
		}
		if err := wcParaInRange(d, para); err != nil {
			return nil, nil, err
		}
		anchor := wcStr(args, "text")
		n, err := d.InsertNote(kind, para, anchor, note)
		if err != nil {
			return nil, nil, err
		}
		said := fmt.Sprintf("문단 %d", para)
		if anchor != "" {
			said = fmt.Sprintf("문단 %d 의 「%s」", para, wcClip(anchor, 30))
		}
		return map[string]any{"paragraph": para, "kind": kind, "number": n}, []string{fmt.Sprintf("%s 에 %s를 달았습니다 — 「%s」", said, wcNoteKo(kind), wcClip(note, 40))}, nil

	case "read_footnotes":
		from, to, _, err := wcRange(d, args, false)
		if err != nil {
			return nil, nil, err
		}
		all, err := d.Notes()
		if err != nil {
			return nil, nil, err
		}
		rows := []any{}
		for _, n := range all {
			if n.Para != 0 && (n.Para < from || n.Para > to) {
				continue
			}
			var para any
			if n.Para != 0 {
				para = n.Para
			}
			rows = append(rows, map[string]any{"number": n.Number, "kind": n.Kind, "paragraph": para, "on": wcClip(n.On, 40), "text": wcClip(n.Text, 200)})
		}
		return map[string]any{"from": from, "to": to, "count": len(rows), "notes": rows}, nil, nil

	case "delete_footnote":
		number := wcInt(args, "number")
		if number < 1 {
			return nil, nil, fmt.Errorf("number 가 없습니다 — read_footnotes 의 번호")
		}
		kind, err := wcNoteKind(args)
		if err != nil {
			return nil, nil, err
		}
		if err := d.DeleteNote(kind, number); err != nil {
			return nil, nil, err
		}
		return map[string]any{"number": number, "kind": kind, "deleted": true}, []string{fmt.Sprintf("%s %d번을 지웠습니다 — 글은 그대로입니다", wcNoteKo(kind), number)}, nil
	case "set_track_changes":
		mode := wcStr(args, "mode")
		switch mode {
		case "Off", "TrackAll":
		case "TrackMineOnly":
			return nil, nil, fmt.Errorf("「내 것만 추적(TrackMineOnly)」은 이 길(COM, Word 2021)이 켤 수 없습니다 — 그 설정이 2021 객체 모델에 없습니다. TrackAll 로 켜세요")
		case "":
			return nil, nil, fmt.Errorf("mode 가 없습니다 — Off, TrackAll, TrackMineOnly")
		default:
			return nil, nil, fmt.Errorf("mode 는 Off, TrackAll, TrackMineOnly 중 하나입니다 — %s", mode)
		}
		if err := d.SetTracking(mode == "TrackAll"); err != nil {
			return nil, nil, err
		}
		return map[string]any{"mode": mode}, []string{"변경 추적 → " + mode}, nil

	case "read_tracked_changes":
		from, to, _, err := wcRange(d, args, false)
		if err != nil {
			return nil, nil, err
		}
		whole := wcInt(args, "from") == 0 && wcInt(args, "to") == 0
		limit := wcInt(args, "limit")
		if limit <= 0 {
			limit = 100
		}
		revs, err := d.Revisions(from, to, whole)
		if err != nil {
			return nil, nil, err
		}
		on, err := d.Tracking()
		if err != nil {
			return nil, nil, err
		}
		mode := "Off"
		if on {
			mode = "TrackAll"
		}
		rows := []any{}
		for i, r := range revs {
			if i >= limit {
				break
			}
			rows = append(rows, map[string]any{"type": r.Type, "author": r.Author, "date": r.Date, "text": wcClip(r.Text, 120)})
		}
		return map[string]any{"from": from, "to": to, "mode": mode, "count": len(revs), "changes": rows}, nil, nil

	case "review_changes":
		what := wcStr(args, "what")
		if what != "accept" && what != "reject" {
			return nil, nil, fmt.Errorf("what 이 없습니다 — accept 나 reject")
		}
		from, to, _, err := wcRange(d, args, false)
		if err != nil {
			return nil, nil, err
		}
		whole := wcInt(args, "from") == 0 && wcInt(args, "to") == 0
		n, err := d.Review(what == "accept", from, to, whole)
		if err != nil {
			return nil, nil, err
		}
		verb := "거부"
		if what == "accept" {
			verb = "수락"
		}
		said := fmt.Sprintf("변경 %d건을 %s했습니다", n, verb)
		if !whole {
			said += fmt.Sprintf(" (문단 %d–%d)", from, to)
		}
		return map[string]any{"what": what, "count": n, "from": from, "to": to}, []string{said}, nil

	case "set_page_setup":
		var s wordPageSetup
		var words []string
		switch o := wcStr(args, "orientation"); o {
		case "":
		case "Portrait":
			s.Orientation = o
			words = append(words, "세로")
		case "Landscape":
			s.Orientation = o
			words = append(words, "가로")
		default:
			return nil, nil, fmt.Errorf("orientation 은 Portrait 나 Landscape 입니다 — %s", o)
		}
		if p := wcStr(args, "paper"); p != "" {
			if _, ok := wordPaperCOM[p]; !ok {
				return nil, nil, fmt.Errorf("paper 는 A4, A3, A5, B4, B5, Letter, Legal, Tabloid, Executive 중 하나입니다 — %s", p)
			}
			s.Paper = p
			words = append(words, "용지 "+p)
		}
		if m, ok := args["margins"].(map[string]any); ok {
			s.Margins = map[string]float64{}
			var ms []string
			for _, k := range []string{"left", "right", "top", "bottom"} {
				if v, ok := wcNum(m, k); ok {
					s.Margins[k] = v
					ms = append(ms, fmt.Sprintf("%s %gpt", k, v))
				}
			}
			if len(ms) > 0 {
				words = append(words, "여백 "+strings.Join(ms, "/"))
			}
		}
		if v, ok := wcNum(args, "header_distance"); ok {
			s.HeaderDist = &v
			words = append(words, fmt.Sprintf("머리글 거리 %gpt", v))
		}
		if v, ok := wcNum(args, "footer_distance"); ok {
			s.FooterDist = &v
			words = append(words, fmt.Sprintf("바닥글 거리 %gpt", v))
		}
		if b, ok := args["different_first_page"].(bool); ok {
			s.DifferentFirst = &b
			if b {
				words = append(words, "첫 쪽 따로")
			} else {
				words = append(words, "첫 쪽 같이")
			}
		}
		if len(words) == 0 {
			return nil, nil, fmt.Errorf("바꿀 것이 없습니다 — orientation·paper·margins·header_distance·footer_distance·different_first_page 중 하나")
		}
		section := wcInt(args, "section")
		total, err := d.Sections()
		if err != nil {
			return nil, nil, err
		}
		if _, given := args["section"]; given && (section < 1 || section > total) {
			return nil, nil, fmt.Errorf("문서에 %d번 구역이 없습니다 — 구역 %d개", section, total)
		}
		n, err := d.PageSetup(section, s)
		if err != nil {
			return nil, nil, err
		}
		where := fmt.Sprintf("구역 %d개", n)
		if section > 0 {
			where = fmt.Sprintf("구역 %d", section)
		}
		return map[string]any{"sections": n}, []string{fmt.Sprintf("%s 쪽 설정: %s", where, strings.Join(words, ", "))}, nil

	case "set_style_format":
		given := wcStr(args, "style")
		if given == "" {
			return nil, nil, fmt.Errorf("style 이 없습니다")
		}
		var f wordStyleFormat
		var words []string
		if v := wcStr(args, "font"); v != "" {
			f.Font = v
			words = append(words, "글꼴 "+v)
		}
		if v, ok := wcNum(args, "size"); ok {
			f.Size = &v
			words = append(words, fmt.Sprintf("크기 %g", v))
		}
		if b, ok := args["bold"].(bool); ok {
			f.Bold = &b
			if b {
				words = append(words, "굵게")
			} else {
				words = append(words, "굵게 해제")
			}
		}
		if b, ok := args["italic"].(bool); ok {
			f.Italic = &b
			if b {
				words = append(words, "기울임")
			} else {
				words = append(words, "기울임 해제")
			}
		}
		if v := wcStr(args, "color"); v != "" {
			if !wordHexColor.MatchString(v) {
				return nil, nil, fmt.Errorf("color 는 #RRGGBB 입니다 — %s", v)
			}
			f.Color = v
			words = append(words, "색 "+v)
		}
		if v := wcStr(args, "align"); v != "" {
			if _, ok := wordAlignCOM[v]; !ok {
				return nil, nil, fmt.Errorf("align 은 Left, Centered, Right, Justified 중 하나입니다 — %s", v)
			}
			f.Align = v
			words = append(words, "정렬 "+v)
		}
		nums := []struct {
			k, ko string
			dst   **float64
		}{
			{"space_before", "앞", &f.SpaceBefore}, {"space_after", "뒤", &f.SpaceAfter}, {"line_spacing", "줄 간격", &f.LineSpacing},
			{"first_line_indent", "첫 줄 들여쓰기", &f.FirstLineIndent}, {"left_indent", "왼쪽 들여쓰기", &f.LeftIndent},
		}
		for _, x := range nums {
			if v, ok := wcNum(args, x.k); ok {
				v := v
				*x.dst = &v
				words = append(words, fmt.Sprintf("%s %gpt", x.ko, v))
			}
		}
		if len(words) == 0 {
			return nil, nil, fmt.Errorf("바꿀 것이 없습니다 — font·size·bold·italic·color·align·space_before·space_after·line_spacing·first_line_indent·left_indent 중 하나")
		}
		builtinName, builtin := wordBuiltinOf(given)
		name, affected, created, err := d.StyleFormat(given, builtin, wcBool(args, "create", false), f)
		if err != nil {
			return nil, nil, err
		}
		var b any
		if builtinName != "" {
			b = builtinName
		}
		return map[string]any{"style": name, "builtin": b, "affected": affected, "created": created},
			[]string{fmt.Sprintf("스타일 「%s」: %s — 문단 %d개에 걸립니다", name, strings.Join(words, ", "), affected)}, nil
	}
	return nil, nil, fmt.Errorf("COM 길이 모르는 도구입니다")
}

// wordPaperCOM 은 용지 이름(word_enums.go 의 wordPapers) → (WdPaperSize, 가로 pt, 세로 pt). 치수는 PaperSize 를 프린터가 거절할 때 쓴다 —
// 실측 2026-09-26: 이 머신의 기본 프린터는 B4(10)를 거절했고 나머지 여덟은 받았다. Word 의 B4·B5 는 JIS 판이다
// (B5 를 걸면 515.95×728.55pt = 182×257mm 가 선다).
var wordPaperCOM = map[string]struct {
	id   int
	w, h float64
}{
	"A4": {7, 595.35, 841.95}, "A3": {6, 841.95, 1190.7}, "A5": {9, 419.55, 595.35},
	"B4": {10, 728.5, 1031.8}, "B5": {11, 515.95, 728.55},
	"Letter": {2, 612, 792}, "Legal": {4, 612, 1008}, "Tabloid": {23, 792, 1224}, "Executive": {5, 522, 756},
}

// wordAlignCOM 은 창의 정렬 이름(word_enums.go 의 wordAligns) → WdParagraphAlignment.
var wordAlignCOM = map[string]int{"Left": 0, "Centered": 1, "Right": 2, "Justified": 3}

var wordHexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// wcNum 은 숫자 칸 하나. ⚠ **헬퍼는 JSON 숫자를 json.Number 로 넘긴다**(mcp.go 가 UseNumber 로 푼다) — float64 만 읽던 때는
// 여백·크기·간격이 전부 「없는 칸」이 되어 조용히 빠졌고, 크기만 준 호출은 「바꿀 것이 없습니다」로 거절됐다(실측 2026-09-26).
// 단위 시험은 float64 를 넣어서 초록이었다 — 시험의 값도 와이어에서 오는 모양이어야 한다.
func wcNum(args map[string]any, k string) (float64, bool) {
	switch v := args[k].(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

var wordBookmarkName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)

// wcRange 는 from/to 를 창과 같은 규칙으로 읽는다: 둘 다 없으면 본문 전체(must 면 거절), to 가 없으면 from 하나.
func wcRange(d wordDoc, args map[string]any, must bool) (from, to int, said string, err error) {
	total, err := d.Paragraphs()
	if err != nil {
		return 0, 0, "", err
	}
	from, to = wcInt(args, "from"), wcInt(args, "to")
	if from == 0 && to == 0 {
		if must {
			return 0, 0, "", fmt.Errorf("from 이 없습니다 — 문단 번호(list_paragraphs)")
		}
		return 1, total, "본문 전체", nil
	}
	if from == 0 {
		from = to
	}
	if to == 0 {
		to = from
	}
	if from < 1 || to < from || to > total {
		return 0, 0, "", fmt.Errorf("문서에 문단 %d–%d 이 없습니다 — 문단 %d개", from, to, total)
	}
	if from == to {
		return from, to, fmt.Sprintf("문단 %d", from), nil
	}
	return from, to, fmt.Sprintf("문단 %d–%d", from, to), nil
}

func wcParaInRange(d wordDoc, n int) error {
	total, err := d.Paragraphs()
	if err != nil {
		return err
	}
	if n < 1 || n > total {
		return fmt.Errorf("문서에 %d번 문단이 없습니다 — 문단 %d개", n, total)
	}
	return nil
}

func wcNoteKind(args map[string]any) (string, error) {
	k := wcStr(args, "kind")
	switch k {
	case "", "footnote":
		return "footnote", nil
	case "endnote":
		return "endnote", nil
	}
	return "", fmt.Errorf("kind 는 footnote 나 endnote 입니다 — %s", k)
}

func wcNoteKo(kind string) string {
	if kind == "endnote" {
		return "미주"
	}
	return "각주"
}

func wcStr(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}

func wcInt(args map[string]any, k string) int { return intOf(args[k]) }

func wcBool(args map[string]any, k string, def bool) bool {
	if b, ok := args[k].(bool); ok {
		return b
	}
	return def
}

// wcClip 은 창의 clip 과 같다 — 글자(rune) 수로 자르고 말줄임표를 붙인다.
func wcClip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// comLocalToUTC 는 COM 의 날짜를 UTC 로 옮긴다.
//
// ⚠ COM 의 날짜(VT_DATE)는 **지역 시각**이고 시간대를 안 싣는데, go-ole 은 그것을 UTC 라고 붙여 돌려준다. 그대로
// 쓰면 한국에서 17:19 에 단 메모가 「17:19Z」 — 아홉 시간 뒤의 일 — 로 읽혔다(실측 2026-09-26). 창(Office.js)은 같은
// 메모를 제대로 된 UTC 로 준다. 벽시계 값을 지역 시각으로 다시 읽어 옮긴다.
func comLocalToUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local).UTC()
}
