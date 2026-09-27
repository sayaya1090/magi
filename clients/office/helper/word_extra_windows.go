//go:build windows

package office

import (
	"fmt"
	"path/filepath"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// comWordExtra 는 COM 전용 도구의 Windows 쪽 — comWordDoc 과 같이 호출마다 표식의 문서를 다시 잡는다.
type comWordExtra struct{ comWordDoc }

func openWordExtraOS(id string) (wordExtra, error) {
	d, err := openWordDocOS(id)
	if err != nil {
		return nil, err
	}
	return comWordExtra{d.(comWordDoc)}, nil
}

func (c comWordExtra) FilePath() (out string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		if strings.TrimSpace(wstr(doc, "Path")) != "" {
			out = wstr(doc, "FullName")
		}
		return nil
	})
	return out, err
}

// ExportPDF 는 ExportAsFixedFormat(경로, wdExportFormatPDF=17) — 문서의 경로·저장 상태는 안 바뀐다.
func (c comWordExtra) ExportPDF(path string) error {
	return c.with(func(doc *ole.IDispatch) error {
		_, err := oleutil.CallMethod(doc, "ExportAsFixedFormat", path, 17)
		return err
	})
}

func (c comWordExtra) Proof(from, to, limit int) (items []wordProof, more bool, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		rng, e := paraRange(doc, from, to)
		if e != nil {
			return e
		}
		defer rng.Release()
		app := wget(doc, "Application")
		defer app.Release()
		for _, kind := range []string{"spelling", "grammar"} {
			prop := "SpellingErrors"
			if kind == "grammar" {
				prop = "GrammaticalErrors"
			}
			errs := wget(rng, prop)
			n := wint(errs, "Count")
			for i := 1; i <= n; i++ {
				if len(items) >= limit {
					more = true
					break
				}
				r := oleutil.MustCallMethod(errs, "Item", i).ToIDispatch()
				text := strings.TrimSpace(wordText(wstr(r, "Text")))
				it := wordProof{Kind: kind, Text: wcClip(text, 200), Paragraph: paraAt(doc, wint(r, "Start"))}
				if kind == "spelling" && text != "" {
					it.Suggestions = spellingSuggestions(app, text)
				}
				r.Release()
				items = append(items, it)
			}
			errs.Release()
		}
		return nil
	})
	return items, more, err
}

// spellingSuggestions 는 Word 의 제안 다섯까지. 그 언어의 사전이 없으면 COM 이 던진다 — 제안이 없는 것으로 둔다.
func spellingSuggestions(app *ole.IDispatch, word string) (out []string) {
	defer func() { _ = recover() }()
	v, err := oleutil.CallMethod(app, "GetSpellingSuggestions", word)
	if err != nil {
		return nil
	}
	s := v.ToIDispatch()
	defer s.Release()
	n := wint(s, "Count")
	for i := 1; i <= n && i <= 5; i++ {
		it := oleutil.MustCallMethod(s, "Item", i).ToIDispatch()
		out = append(out, wstr(it, "Name"))
		it.Release()
	}
	return out
}

// Stats 는 ComputeStatistics — wdStatisticWords 0, Lines 1, Pages 2, Characters 3, Paragraphs 4, CharactersWithSpaces 5, FarEastCharacters 6.
func (c comWordExtra) Stats() (st wordStats, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		get := func(k int) int {
			v := oleutil.MustCallMethod(doc, "ComputeStatistics", k, false)
			return intOf(v.Value())
		}
		st = wordStats{Words: get(0), Lines: get(1), Pages: get(2), Characters: get(3), Paragraphs: get(4), CharactersWithSpaces: get(5), EastAsian: get(6)}
		return nil
	})
	return st, err
}

// Compare 는 다른 파일을 읽기 전용·숨김으로 열어 Application.CompareDocuments 로 새 문서를 만든다. 연 파일은 닫는다(저장 안 함).
func (c comWordExtra) Compare(other string, otherIsRevised bool) (name string, revisions int, leftOpen string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		app := wget(doc, "Application")
		defer app.Release()
		docs := wget(app, "Documents")
		defer docs.Release()
		// Open(FileName, ConfirmConversions, ReadOnly, AddToRecentFiles, PasswordDocument, PasswordTemplate, Revert,
		//      WritePasswordDocument, WritePasswordTemplate, Format, Encoding, Visible)
		v, e := oleutil.CallMethod(docs, "Open", other, false, true, false, "", "", false, "", "", 0, 0, false)
		if e != nil {
			return fmt.Errorf("%s 를 못 열었습니다(%v)", filepath.Base(other), e)
		}
		od := v.ToIDispatch()
		// 숨겨 연 문서는 **꼭 닫는다** — 못 닫으면 사람 눈에 안 보이는 문서가 Word 에 남아, 같은 파일을 다시 열 때 「이미 열려
		// 있음」이나 잠김으로 나타난다. 한 번 더(저장된 것으로 표시하고) 시도하고, 그래도 안 되면 답에 싣는다(#201 검토 5855089115).
		defer func() {
			if _, e := oleutil.CallMethod(od, "Close", 0); e != nil { // wdDoNotSaveChanges
				if _, pe := oleutil.PutProperty(od, "Saved", true); pe == nil {
					_, e = oleutil.CallMethod(od, "Close")
				}
				if e != nil {
					leftOpen = fmt.Sprintf("비교 대상 %s 를 닫지 못했습니다(%v) — Word 안에 보이지 않는 문서로 열려 있을 수 있습니다. Word 를 끝내면 같이 닫히고, 그 전에 같은 파일을 열면 「이미 열려 있음」이 뜰 수 있습니다", filepath.Base(other), e)
				}
			}
			od.Release()
		}()
		orig, rev := doc, od
		if !otherIsRevised {
			orig, rev = od, doc
		}
		// CompareDocuments(Original, Revised, Destination=wdCompareDestinationNew 2, Granularity=wdGranularityWordLevel 1,
		//   Formatting, CaseChanges, Whitespace, Tables, Headers, Footnotes, Textboxes, Fields, Comments, Moves, RevisedAuthor, IgnoreWarnings)
		r, e := oleutil.CallMethod(app, "CompareDocuments", orig, rev, 2, 1, true, true, false, true, true, true, true, true, true, true, wstr(rev, "Name"), true)
		if e != nil {
			return fmt.Errorf("비교하지 못했습니다(%v)", e)
		}
		res := r.ToIDispatch()
		defer res.Release()
		name = wstr(res, "Name")
		rs := wget(res, "Revisions")
		revisions = wint(rs, "Count")
		rs.Release()
		return nil
	})
	return name, revisions, leftOpen, err
}
