//go:build windows

package office

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// comWordDoc 은 MAGI.DOC 가 id 인 문서 하나. 호출마다 떠 있는 Word 를 잡아 그 문서를 다시 찾는다 — 사이에 사람이
// 창을 닫았거나 다른 문서를 앞에 놓았어도, 늘 표식이 가리키는 그 문서만 만진다.
type comWordDoc struct{ id string }

func openWordDocOS(id string) (wordDoc, error) {
	d := comWordDoc{id: id}
	// 잡히는지만 본다 — 안 떠 있거나 표식의 문서가 없으면 여기서 이유를 댄다.
	if err := d.with(func(*ole.IDispatch) error { return nil }); err != nil {
		return nil, err
	}
	return d, nil
}

func (comWordDoc) Close() {}

// with 는 OS 스레드 하나를 잠그고 STA 로 초기화해서 그 문서를 f 에 건넨다 — Office 의 COM 은 아파트먼트가
// 다르면 조용히 이상해진다(xl_notes_windows.go 와 같은 규칙).
func (c comWordDoc) with(f func(doc *ole.IDispatch) error) (err error) {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("Word COM: %v", r)
			}
		}()
		if e := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); e != nil {
			done <- e
			return
		}
		defer ole.CoUninitialize()
		unk, e := oleutil.GetActiveObject("Word.Application")
		if e != nil {
			done <- fmt.Errorf("떠 있는 Word 를 COM 으로 못 잡았습니다(%v) — Word 가 켜져 있고 문서가 열려 있어야 합니다", e)
			return
		}
		defer unk.Release()
		app, e := unk.QueryInterface(ole.IID_IDispatch)
		if e != nil {
			done <- e
			return
		}
		defer app.Release()
		doc, e := wordDocByStamp(app, c.id)
		if e != nil {
			done <- e
			return
		}
		defer doc.Release()
		done <- f(doc)
	}()
	return <-done
}

// wordDocByStamp 는 열린 문서들 가운데 사용자 지정 속성 MAGI.DOC 가 id 인 것 — 이름으로도 「하나뿐이면 그것」으로도
// 고르지 않는다(word_com.go 머리 주석).
func wordDocByStamp(app *ole.IDispatch, id string) (*ole.IDispatch, error) {
	docs, err := oleutil.GetProperty(app, "Documents")
	if err != nil {
		return nil, err
	}
	dd := docs.ToIDispatch()
	defer dd.Release()
	count := int(oleutil.MustGetProperty(dd, "Count").Val)
	var names []string
	for i := 1; i <= count; i++ {
		d := item(dd, i)
		name := oleutil.MustGetProperty(d, "Name").ToString()
		if wordStamp(d) == id {
			return d, nil
		}
		names = append(names, name)
		d.Release()
	}
	return nil, fmt.Errorf("COM 으로 잡은 Word 에 이 창의 문서가 없습니다(표식 MAGI.DOC=%s, 열린 것: %s) — 다른 Word 인스턴스일 수 있습니다", id, strings.Join(names, ", "))
}

// wordStamp 는 문서의 MAGI.DOC 값. 없으면 빈 문자열.
func wordStamp(doc *ole.IDispatch) (out string) {
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	pv, err := oleutil.GetProperty(doc, "CustomDocumentProperties")
	if err != nil {
		return ""
	}
	props := pv.ToIDispatch()
	defer props.Release()
	iv, err := oleutil.GetProperty(props, "Item", DOCPropertyStamp)
	if err != nil {
		return ""
	}
	p := iv.ToIDispatch()
	defer p.Release()
	v, err := oleutil.GetProperty(p, "Value")
	if err != nil {
		return ""
	}
	return fmt.Sprint(v.Value())
}

// DOCPropertyStamp 는 창이 문서에 새기는 이름 — OfficeDocument.js 의 DOC_PROPERTY 와 같아야 한다.
const DOCPropertyStamp = "MAGI.DOC"

// ── 작은 손잡이들 ──

func wget(d *ole.IDispatch, name string, args ...any) *ole.IDispatch {
	return oleutil.MustGetProperty(d, name, args...).ToIDispatch()
}

func wint(d *ole.IDispatch, name string) int {
	v := oleutil.MustGetProperty(d, name)
	switch x := v.Value().(type) {
	case int32:
		return int(x)
	case int64:
		return int(x)
	case int16:
		return int(x)
	case int:
		return x
	case float64:
		return int(x)
	}
	return int(v.Val)
}

func wstr(d *ole.IDispatch, name string) string {
	return fmt.Sprint(oleutil.MustGetProperty(d, name).Value())
}

func wdate(d *ole.IDispatch, name string) string {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return ""
	}
	if t, ok := v.Value().(time.Time); ok {
		return comLocalToUTC(t).Format(time.RFC3339)
	}
	return fmt.Sprint(v.Value())
}

// wordText 는 Word 의 글에서 표식 문자(\r·\a·\x02·\x05)를 걷는다 — 문단 끝·셀 끝·각주 표식·메모 표식.
func wordText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\a', '\x02', '\x05', '\x0b':
			return -1
		}
		return r
	}, s))
}

// paraRange 는 문단 from..to 를 덮는 범위 — 끝의 문단 표식은 뺀다(메모·책갈피가 다음 문단으로 새지 않게).
func paraRange(doc *ole.IDispatch, from, to int) (*ole.IDispatch, error) {
	ps := wget(doc, "Paragraphs")
	defer ps.Release()
	n := wint(ps, "Count")
	if from < 1 || to < from || to > n {
		return nil, fmt.Errorf("문서에 문단 %d–%d 이 없습니다 — 문단 %d개", from, to, n)
	}
	a := item(ps, from)
	defer a.Release()
	ar := wget(a, "Range")
	defer ar.Release()
	b := item(ps, to)
	defer b.Release()
	br := wget(b, "Range")
	defer br.Release()
	start, end := wint(ar, "Start"), wint(br, "End")
	if end-1 > start {
		end--
	}
	return oleutil.MustCallMethod(doc, "Range", start, end).ToIDispatch(), nil
}

// findIn 은 rng 안에서 글을 찾아 rng 를 그 자리로 옮긴다. 못 찾으면 거절문.
func findIn(rng *ole.IDispatch, anchor, where string) error {
	f := wget(rng, "Find")
	defer f.Release()
	ok, err := oleutil.CallMethod(f, "Execute", anchor)
	if err != nil {
		return err
	}
	if b, _ := ok.Value().(bool); !b {
		return fmt.Errorf("%s 에 「%s」 가 없습니다", where, anchor)
	}
	return nil
}

// paraAt 은 문서 위치 pos 가 든 문단 번호(1부터).
func paraAt(doc *ole.IDispatch, pos int) int {
	r := oleutil.MustCallMethod(doc, "Range", 0, pos).ToIDispatch()
	defer r.Release()
	ps := wget(r, "Paragraphs")
	defer ps.Release()
	return wint(ps, "Count")
}

// ── 문단 ──

func (c comWordDoc) Paragraphs() (n int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ps := wget(doc, "Paragraphs")
		defer ps.Release()
		n = wint(ps, "Count")
		return nil
	})
	return
}

// ── 메모 ──
//
// COM 의 메모에는 오래가는 id 가 없다 — 있는 것은 문서 순서의 번호(Index)뿐이고, 앞에 메모가 하나 붙으면 번호가
// 민다. 그래서 id 에 번호와 **지문**(작성자·글의 해시 앞 6자리)을 같이 싣고, 쓸 때 지문이 맞는지 본다. 틀리면
// 엉뚱한 메모를 고치는 대신 「다시 읽어라」로 거절한다.

func commentID(index int, author, text string) string {
	h := sha1.Sum([]byte(author + "\x00" + text))
	return "c" + strconv.Itoa(index) + "-" + hex.EncodeToString(h[:])[:6]
}

func commentText(cm *ole.IDispatch) string {
	r := wget(cm, "Range")
	defer r.Release()
	return wordText(wstr(r, "Text"))
}

func isReply(cm *ole.IDispatch) (reply bool) {
	defer func() {
		if recover() != nil {
			reply = false
		}
	}()
	v, err := oleutil.GetProperty(cm, "Ancestor")
	if err != nil || v == nil || v.VT == ole.VT_EMPTY || v.VT == ole.VT_NULL {
		return false
	}
	if v.VT == ole.VT_DISPATCH && v.ToIDispatch() != nil {
		v.ToIDispatch().Release()
		return true
	}
	return false
}

func (c comWordDoc) commentByID(doc *ole.IDispatch, id string) (*ole.IDispatch, error) {
	var index int
	var fp string
	if _, err := fmt.Sscanf(strings.Replace(id, "-", " ", 1), "c%d %s", &index, &fp); err != nil || index < 1 {
		return nil, fmt.Errorf("id %s 는 이 길(COM)의 메모 id 가 아닙니다 — read_comments 가 준 id 를 주세요", id)
	}
	cs := wget(doc, "Comments")
	defer cs.Release()
	if index > wint(cs, "Count") {
		return nil, fmt.Errorf("id %s 인 메모가 없습니다 — read_comments 로 다시 읽으세요", id)
	}
	cm := item(cs, index)
	if isReply(cm) || commentID(index, wstr(cm, "Author"), commentText(cm)) != id {
		cm.Release()
		return nil, fmt.Errorf("id %s 의 메모가 바뀌었습니다(앞에 메모가 붙거나 지워지면 번호가 민다) — read_comments 로 다시 읽으세요", id)
	}
	return cm, nil
}

func (c comWordDoc) AddComment(from, to int, anchor, text string) (id string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		rng, e := paraRange(doc, from, to)
		if e != nil {
			return e
		}
		defer rng.Release()
		if anchor != "" {
			if e := findIn(rng, anchor, fmt.Sprintf("문단 %d–%d", from, to)); e != nil {
				return e
			}
		}
		cs := wget(doc, "Comments")
		defer cs.Release()
		cm := oleutil.MustCallMethod(cs, "Add", rng, text).ToIDispatch()
		defer cm.Release()
		id = commentID(wint(cm, "Index"), wstr(cm, "Author"), commentText(cm))
		return nil
	})
	return
}

func (c comWordDoc) Comments(from, to int) (out []wordComment, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		cs := wget(doc, "Comments")
		defer cs.Release()
		n := wint(cs, "Count")
		for i := 1; i <= n; i++ {
			cm := item(cs, i)
			if isReply(cm) {
				cm.Release()
				continue
			}
			scope := wget(cm, "Scope")
			para := paraAt(doc, wint(scope, "Start"))
			on := wordText(wstr(scope, "Text"))
			scope.Release()
			if para < from || para > to {
				cm.Release()
				continue
			}
			author, text := wstr(cm, "Author"), commentText(cm)
			done, _ := oleutil.MustGetProperty(cm, "Done").Value().(bool)
			x := wordComment{ID: commentID(i, author, text), Author: author, Date: wdate(cm, "Date"), On: on, Text: text, Resolved: done, Para: para}
			rs := wget(cm, "Replies")
			for j := 1; j <= wint(rs, "Count"); j++ {
				r := item(rs, j)
				x.Replies = append(x.Replies, wordReply{Author: wstr(r, "Author"), Date: wdate(r, "Date"), Text: commentText(r)})
				r.Release()
			}
			rs.Release()
			cm.Release()
			out = append(out, x)
		}
		return nil
	})
	return
}

func (c comWordDoc) ReplyComment(id, text string) error {
	return c.with(func(doc *ole.IDispatch) error {
		cm, e := c.commentByID(doc, id)
		if e != nil {
			return e
		}
		defer cm.Release()
		rs := wget(cm, "Replies")
		defer rs.Release()
		scope := wget(cm, "Scope")
		defer scope.Release()
		r := oleutil.MustCallMethod(rs, "Add", scope, text).ToIDispatch()
		r.Release()
		return nil
	})
}

func (c comWordDoc) ResolveComment(id string, resolved, del bool) error {
	return c.with(func(doc *ole.IDispatch) error {
		cm, e := c.commentByID(doc, id)
		if e != nil {
			return e
		}
		defer cm.Release()
		if del {
			// ⚠ **메모를 지우면 답글도 같이 가야 한다.** 창(Office.js)의 delete 는 실을 통째로 지우는데, COM 에서는
			// 답글이 따로 선 메모라 부모만 지우면 답글이 **고아로 남아** 혼자 선 메모가 된다(실측 2026-09-26, LTSC 2021:
			// 메모를 지웠더니 「3쪽 표 참고」가 남았다). 답글부터 뒤에서 앞으로 지운다.
			rs := wget(cm, "Replies")
			for j := wint(rs, "Count"); j >= 1; j-- {
				r := item(rs, j)
				_, e := oleutil.CallMethod(r, "Delete")
				r.Release()
				if e != nil {
					rs.Release()
					return e
				}
			}
			rs.Release()
			_, e = oleutil.CallMethod(cm, "Delete")
			return e
		}
		_, e = oleutil.PutProperty(cm, "Done", resolved)
		return e
	})
}

// ── 책갈피 ──

func (c comWordDoc) AddBookmark(from, to int, name string) error {
	return c.with(func(doc *ole.IDispatch) error {
		rng, e := paraRange(doc, from, to)
		if e != nil {
			return e
		}
		defer rng.Release()
		bs := wget(doc, "Bookmarks")
		defer bs.Release()
		b := oleutil.MustCallMethod(bs, "Add", name, rng).ToIDispatch()
		b.Release()
		return nil
	})
}

func (c comWordDoc) DeleteBookmark(name string) (had bool, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		bs := wget(doc, "Bookmarks")
		defer bs.Release()
		ex, _ := oleutil.MustCallMethod(bs, "Exists", name).Value().(bool)
		if !ex {
			return nil
		}
		had = true
		b := oleutil.MustCallMethod(bs, "Item", name).ToIDispatch()
		defer b.Release()
		_, e := oleutil.CallMethod(b, "Delete")
		return e
	})
	return
}

// ── 각주·미주 ──

func notesOf(doc *ole.IDispatch, kind string) *ole.IDispatch {
	if kind == "endnote" {
		return wget(doc, "Endnotes")
	}
	return wget(doc, "Footnotes")
}

func (c comWordDoc) InsertNote(kind string, para int, anchor, text string) (number int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		rng, e := paraRange(doc, para, para)
		if e != nil {
			return e
		}
		defer rng.Release()
		if anchor != "" {
			if e := findIn(rng, anchor, fmt.Sprintf("문단 %d", para)); e != nil {
				return e
			}
		}
		// 표식은 걸린 글 **뒤에** 선다 — 범위를 끝으로 접는다(wdCollapseEnd = 0).
		oleutil.MustCallMethod(rng, "Collapse", 0)
		ns := notesOf(doc, kind)
		defer ns.Release()
		n := oleutil.MustCallMethod(ns, "Add", rng).ToIDispatch()
		defer n.Release()
		nr := wget(n, "Range")
		oleutil.MustPutProperty(nr, "Text", text)
		nr.Release()
		number = wint(n, "Index")
		return nil
	})
	return
}

func (c comWordDoc) Notes() (out []wordNote, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		for _, kind := range []string{"footnote", "endnote"} {
			ns := notesOf(doc, kind)
			n := wint(ns, "Count")
			for i := 1; i <= n; i++ {
				fn := item(ns, i)
				ref := wget(fn, "Reference")
				at := wint(ref, "Start")
				para := paraAt(doc, at)
				// 걸린 글: 표식 바로 앞 30자 — 창(WordHand.js readFootnotes)과 같은 규칙.
				lo := at - 30
				pr, _ := paraRange(doc, para, para)
				if pr != nil {
					if s := wint(pr, "Start"); lo < s {
						lo = s
					}
					pr.Release()
				}
				before := oleutil.MustCallMethod(doc, "Range", lo, at).ToIDispatch()
				on := wordText(wstr(before, "Text"))
				before.Release()
				ref.Release()
				body := wget(fn, "Range")
				out = append(out, wordNote{Number: i, Kind: kind, Para: para, On: on, Text: wordText(wstr(body, "Text"))})
				body.Release()
				fn.Release()
			}
			ns.Release()
		}
		return nil
	})
	return
}

func (c comWordDoc) DeleteNote(kind string, number int) error {
	return c.with(func(doc *ole.IDispatch) error {
		ns := notesOf(doc, kind)
		defer ns.Release()
		if number < 1 || number > wint(ns, "Count") {
			return fmt.Errorf("%s %d번이 없습니다 — %d개", wcNoteKo(kind), number, wint(ns, "Count"))
		}
		fn := item(ns, number)
		defer fn.Release()
		_, e := oleutil.CallMethod(fn, "Delete")
		return e
	})
}

// ── 변경 추적 ──

func (c comWordDoc) Tracking() (on bool, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		on, _ = oleutil.MustGetProperty(doc, "TrackRevisions").Value().(bool)
		return nil
	})
	return
}

func (c comWordDoc) SetTracking(on bool) error {
	return c.with(func(doc *ole.IDispatch) error {
		_, e := oleutil.PutProperty(doc, "TrackRevisions", on)
		return e
	})
}

// revisionsOf 는 본문 전체나 문단 from..to 의 변경 모음.
func revisionsOf(doc *ole.IDispatch, from, to int, whole bool) (*ole.IDispatch, func(), error) {
	if whole {
		rv := wget(doc, "Revisions")
		return rv, func() { rv.Release() }, nil
	}
	rng, e := paraRange(doc, from, to)
	if e != nil {
		return nil, func() {}, e
	}
	rv := wget(rng, "Revisions")
	return rv, func() { rv.Release(); rng.Release() }, nil
}

// revisionType 은 WdRevisionType 을 창(Office.js TrackedChangeType)의 이름으로.
func revisionType(t int) string {
	switch t {
	case 1:
		return "Added"
	case 2:
		return "Deleted"
	}
	return "Formatted"
}

func (c comWordDoc) Revisions(from, to int, whole bool) (out []wordRevision, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		rv, done, e := revisionsOf(doc, from, to, whole)
		if e != nil {
			return e
		}
		defer done()
		n := wint(rv, "Count")
		for i := 1; i <= n; i++ {
			r := item(rv, i)
			rr := wget(r, "Range")
			out = append(out, wordRevision{Type: revisionType(wint(r, "Type")), Author: wstr(r, "Author"), Date: wdate(r, "Date"), Text: wordText(wstr(rr, "Text"))})
			rr.Release()
			r.Release()
		}
		return nil
	})
	return
}

func (c comWordDoc) Review(accept bool, from, to int, whole bool) (n int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		rv, done, e := revisionsOf(doc, from, to, whole)
		if e != nil {
			return e
		}
		defer done()
		n = wint(rv, "Count")
		if n == 0 {
			return nil
		}
		verb := "RejectAll"
		if accept {
			verb = "AcceptAll"
		}
		_, e = oleutil.CallMethod(rv, verb)
		return e
	})
	return
}

// ── 쪽 설정 ──

func (c comWordDoc) Sections() (n int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ss := wget(doc, "Sections")
		defer ss.Release()
		n = wint(ss, "Count")
		return nil
	})
	return
}

func (c comWordDoc) PageSetup(section int, s wordPageSetup) (n int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ss := wget(doc, "Sections")
		defer ss.Release()
		total := wint(ss, "Count")
		lo, hi := 1, total
		if section > 0 {
			lo, hi = section, section
		}
		for i := lo; i <= hi; i++ {
			sec := item(ss, i)
			ps := wget(sec, "PageSetup")
			if e := applyPageSetup(ps, s); e != nil {
				ps.Release()
				sec.Release()
				return e
			}
			ps.Release()
			sec.Release()
			n++
		}
		return nil
	})
	return
}

func applyPageSetup(ps *ole.IDispatch, s wordPageSetup) error {
	// 용지를 먼저, 방향을 나중에 — 방향을 먼저 걸면 용지를 바꿀 때 폭·높이가 도로 세로로 선다.
	if s.Paper != "" {
		p := wordPaperCOM[s.Paper]
		if _, e := oleutil.PutProperty(ps, "PaperSize", p.id); e != nil {
			// 프린터가 그 용지 번호를 안 받으면 치수로 건다(wordPaperCOM 주석).
			if _, e := oleutil.PutProperty(ps, "PageWidth", p.w); e != nil {
				return fmt.Errorf("용지 %s 를 못 걸었습니다: %v", s.Paper, e)
			}
			if _, e := oleutil.PutProperty(ps, "PageHeight", p.h); e != nil {
				return fmt.Errorf("용지 %s 를 못 걸었습니다: %v", s.Paper, e)
			}
		}
	}
	if s.Orientation != "" {
		o := 0
		if s.Orientation == "Landscape" {
			o = 1
		}
		if _, e := oleutil.PutProperty(ps, "Orientation", o); e != nil {
			return e
		}
	}
	for k, prop := range map[string]string{"left": "LeftMargin", "right": "RightMargin", "top": "TopMargin", "bottom": "BottomMargin"} {
		if v, ok := s.Margins[k]; ok {
			if _, e := oleutil.PutProperty(ps, prop, v); e != nil {
				return fmt.Errorf("여백 %s 를 못 걸었습니다: %v", k, e)
			}
		}
	}
	if s.HeaderDist != nil {
		oleutil.MustPutProperty(ps, "HeaderDistance", *s.HeaderDist)
	}
	if s.FooterDist != nil {
		oleutil.MustPutProperty(ps, "FooterDistance", *s.FooterDist)
	}
	if s.DifferentFirst != nil {
		v := 0
		if *s.DifferentFirst {
			v = -1
		}
		oleutil.MustPutProperty(ps, "DifferentFirstPageHeaderFooter", v)
	}
	return nil
}

// ── 스타일 서식 ──

// wordBGR 은 #RRGGBB 를 Word 의 색 값(BGR 정수)으로.
func wordBGR(hex string) int {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	r, g, b := (v>>16)&0xFF, (v>>8)&0xFF, v&0xFF
	return int(r | g<<8 | b<<16)
}

func (c comWordDoc) StyleFormat(local string, builtin int, create bool, f wordStyleFormat) (name string, affected int, created bool, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		sts := wget(doc, "Styles")
		defer sts.Release()
		var st *ole.IDispatch
		if builtin != 0 {
			// Styles.Item 은 속성이 아니라 메서드다 — 속성으로 부르면 「Item 은 속성이 아닙니다」(실측 2026-09-26). item() 이 둘 다 해 본다.
			st = item(sts, builtin)
		} else {
			n := wint(sts, "Count")
			var names []string
			for i := 1; i <= n; i++ {
				x := item(sts, i)
				nl := wstr(x, "NameLocal")
				if nl == local {
					st = x
					break
				}
				if len(names) < 24 {
					names = append(names, nl)
				}
				x.Release()
			}
			if st == nil && create {
				st = oleutil.MustCallMethod(sts, "Add", local, 1).ToIDispatch() // wdStyleTypeParagraph
				created = true
			}
			if st == nil {
				return fmt.Errorf("「%s」 스타일이 없습니다 — 이 문서의 스타일: %s … 새로 만들려면 create: true", local, strings.Join(names, ", "))
			}
		}
		defer st.Release()
		name = wstr(st, "NameLocal")
		font := wget(st, "Font")
		if f.Font != "" {
			oleutil.MustPutProperty(font, "Name", f.Font)
		}
		if f.Size != nil {
			oleutil.MustPutProperty(font, "Size", *f.Size)
		}
		if f.Bold != nil {
			oleutil.MustPutProperty(font, "Bold", *f.Bold)
		}
		if f.Italic != nil {
			oleutil.MustPutProperty(font, "Italic", *f.Italic)
		}
		if f.Color != "" {
			oleutil.MustPutProperty(font, "Color", wordBGR(f.Color))
		}
		font.Release()
		pf := wget(st, "ParagraphFormat")
		if f.Align != "" {
			oleutil.MustPutProperty(pf, "Alignment", wordAlignCOM[f.Align])
		}
		if f.SpaceBefore != nil {
			oleutil.MustPutProperty(pf, "SpaceBefore", *f.SpaceBefore)
		}
		if f.SpaceAfter != nil {
			oleutil.MustPutProperty(pf, "SpaceAfter", *f.SpaceAfter)
		}
		if f.LineSpacing != nil {
			// 창(Office.js)의 lineSpacing 은 「줄 사이 pt, 12 ≈ 한 줄」이다 — COM 에서 그 뜻은 배수 규칙(wdLineSpaceMultiple = 5)
			// 위의 pt 값이다. 규칙을 먼저 걸어야 값이 그 규칙으로 읽힌다.
			oleutil.MustPutProperty(pf, "LineSpacingRule", 5)
			oleutil.MustPutProperty(pf, "LineSpacing", *f.LineSpacing)
		}
		if f.FirstLineIndent != nil {
			oleutil.MustPutProperty(pf, "FirstLineIndent", *f.FirstLineIndent)
		}
		if f.LeftIndent != nil {
			oleutil.MustPutProperty(pf, "LeftIndent", *f.LeftIndent)
		}
		pf.Release()

		// 몇 문단에 걸리는가 — 그 스타일인 본문 문단 수.
		ps := wget(doc, "Paragraphs")
		defer ps.Release()
		for i := 1; i <= wint(ps, "Count"); i++ {
			p := item(ps, i)
			ps2 := wget(p, "Style")
			if wstr(ps2, "NameLocal") == name {
				affected++
			}
			ps2.Release()
			p.Release()
		}
		return nil
	})
	return
}
