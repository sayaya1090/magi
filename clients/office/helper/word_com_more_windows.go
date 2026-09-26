//go:build windows

package office

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// ── 도형 ──

// shapeTypeName 은 MsoShapeType 을 창(Office.js Word.ShapeType)의 이름으로.
func shapeTypeName(t int) string {
	switch t {
	case 1:
		return "GeometricShape"
	case 17:
		return "TextBox"
	case 13, 11:
		return "Picture"
	case 3:
		return "Chart"
	case 6:
		return "Group"
	}
	return "Unsupported"
}

func shapeTextOf(sh *ole.IDispatch) (out string) {
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	tf := wget(sh, "TextFrame")
	defer tf.Release()
	if wint(tf, "HasText") == 0 {
		return ""
	}
	tr := wget(tf, "TextRange")
	defer tr.Release()
	return wordText(wstr(tr, "Text"))
}

func wfloat(d *ole.IDispatch, name string) float64 {
	switch x := oleutil.MustGetProperty(d, name).Value().(type) {
	case float32:
		return float64(x)
	case float64:
		return x
	case int32:
		return float64(x)
	}
	return 0
}

// shapeBy 는 id 나 이름으로 도형 하나 — 창의 #shapePick 과 같은 거절문.
func shapeBy(doc *ole.IDispatch, id int, name string) (*ole.IDispatch, error) {
	ss := wget(doc, "Shapes")
	defer ss.Release()
	n := wint(ss, "Count")
	var have []string
	for i := 1; i <= n; i++ {
		sh := item(ss, i)
		sid, snm := wint(sh, "ID"), wstr(sh, "Name")
		if (id != 0 && sid == id) || (id == 0 && name != "" && snm == name) {
			return sh, nil
		}
		have = append(have, fmt.Sprintf("%s#%d", snm, sid))
		sh.Release()
	}
	who := fmt.Sprintf("id %d", id)
	if id == 0 {
		who = fmt.Sprintf("이름 「%s」", name)
	}
	if len(have) == 0 {
		return nil, fmt.Errorf("%s인 도형이 없습니다 — 하나도 없습니다", who)
	}
	return nil, fmt.Errorf("%s인 도형이 없습니다 — 있는 것: %s", who, strings.Join(have, ", "))
}

func (c comWordDoc) Shapes() (out []wordShape, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ss := wget(doc, "Shapes")
		defer ss.Release()
		for i := 1; i <= wint(ss, "Count"); i++ {
			sh := item(ss, i)
			out = append(out, wordShape{ID: wint(sh, "ID"), Name: wstr(sh, "Name"), Type: shapeTypeName(wint(sh, "Type")), Text: shapeTextOf(sh),
				Left: wfloat(sh, "Left"), Top: wfloat(sh, "Top"), Width: wfloat(sh, "Width"), Height: wfloat(sh, "Height")})
			sh.Release()
		}
		return nil
	})
	return
}

func paintShape(sh *ole.IDispatch, fill, line string) {
	if fill != "" {
		f := wget(sh, "Fill")
		if fill == "none" {
			oleutil.MustPutProperty(f, "Visible", 0)
		} else {
			oleutil.MustPutProperty(f, "Visible", -1)
			oleutil.MustCallMethod(f, "Solid")
			fc := wget(f, "ForeColor")
			oleutil.MustPutProperty(fc, "RGB", wordBGR(fill))
			fc.Release()
		}
		f.Release()
	}
	if line != "" {
		l := wget(sh, "Line")
		if line == "none" {
			oleutil.MustPutProperty(l, "Visible", 0)
		} else {
			oleutil.MustPutProperty(l, "Visible", -1)
			fc := wget(l, "ForeColor")
			oleutil.MustPutProperty(fc, "RGB", wordBGR(line))
			fc.Release()
		}
		l.Release()
	}
}

func setShapeText(sh *ole.IDispatch, text string) {
	tf := wget(sh, "TextFrame")
	defer tf.Release()
	tr := wget(tf, "TextRange")
	defer tr.Release()
	oleutil.MustPutProperty(tr, "Text", text)
}

func (c comWordDoc) AddShape(para int, s wordShapeSpec) (id int, name string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		// 도형은 문단에 닻을 내린다 — 창과 같다(paragraph 기본 1).
		anchor, e := paraRange(doc, para, para)
		if e != nil {
			return e
		}
		defer anchor.Release()
		ss := wget(doc, "Shapes")
		defer ss.Release()
		var sh *ole.IDispatch
		if s.Mso == 0 {
			sh = oleutil.MustCallMethod(ss, "AddTextbox", 1, s.Left, s.Top, s.Width, s.Height, anchor).ToIDispatch()
		} else {
			sh = oleutil.MustCallMethod(ss, "AddShape", s.Mso, s.Left, s.Top, s.Width, s.Height, anchor).ToIDispatch()
		}
		defer sh.Release()
		pinToPage(sh, &s.Left, &s.Top)
		if s.Name != "" {
			oleutil.MustPutProperty(sh, "Name", s.Name)
		}
		paintShape(sh, s.Fill, s.Line)
		if s.Text != "" {
			setShapeText(sh, s.Text)
		}
		id, name = wint(sh, "ID"), wstr(sh, "Name")
		return nil
	})
	return
}

func (c comWordDoc) EditShape(id int, name string, e wordShapeEdit) (gotID int, gotName string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		sh, er := shapeBy(doc, id, name)
		if er != nil {
			return er
		}
		defer sh.Release()
		gotID, gotName = wint(sh, "ID"), wstr(sh, "Name")
		paintShape(sh, e.Fill, e.Line)
		if e.Left != nil || e.Top != nil {
			pinToPage(sh, e.Left, e.Top)
			e.Left, e.Top = nil, nil
		}
		for k, v := range map[string]*float64{"Left": e.Left, "Top": e.Top, "Width": e.Width, "Height": e.Height} {
			if v != nil {
				oleutil.MustPutProperty(sh, k, *v)
			}
		}
		if e.NewName != "" {
			oleutil.MustPutProperty(sh, "Name", e.NewName)
		}
		if e.Text != nil {
			setShapeText(sh, *e.Text)
		}
		return nil
	})
	return
}

func (c comWordDoc) DeleteShape(id int, name string) (gotID int, gotName string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		sh, er := shapeBy(doc, id, name)
		if er != nil {
			return er
		}
		defer sh.Release()
		gotID, gotName = wint(sh, "ID"), wstr(sh, "Name")
		_, er = oleutil.CallMethod(sh, "Delete")
		return er
	})
	return
}

// ── 표 ──

func (c comWordDoc) Tables() (n int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ts := wget(doc, "Tables")
		defer ts.Release()
		n = wint(ts, "Count")
		return nil
	})
	return
}

func cellAt(t *ole.IDispatch, r, col int) *ole.IDispatch {
	return oleutil.MustCallMethod(t, "Cell", r, col).ToIDispatch()
}

func (c comWordDoc) EditTable(n int, e wordTableEdit) (rows, cols int, done []string, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		ts := wget(doc, "Tables")
		defer ts.Release()
		t := item(ts, n)
		defer t.Release()
		rs, cs := wget(t, "Rows"), wget(t, "Columns")
		rows, cols = wint(rs, "Count"), wint(cs, "Count")
		rs.Release()
		cs.Release()

		// ⚠ **다 재고 나서 고친다.** COM 은 한 호출을 한 번에 되돌리지 않는다 — 병합을 건 뒤 열 추가에서 죽으면 반쯤 고친 표가
		// 남는다(실측 2026-09-26: 병합·열 추가까지 걸리고 값 채우기에서 죽어 1행 3칸·2행 4칸짜리가 남았다). 번호는 창처럼 0부터 받는다.
		if m := e.Merge; m != nil {
			fr, fc, tr, tc := m[0], m[1], m[2], m[3]
			if fr < 0 || fc < 0 || tr >= rows || tc >= cols || fr > tr || fc > tc {
				return fmt.Errorf("merge 가 표 밖입니다 — (%d,%d)–(%d,%d) (표는 %d×%d, 0부터)", fr, fc, tr, tc, rows, cols)
			}
		}
		addIdx := -1
		switch e.AddAt {
		case "", "end", "start":
		default:
			idx, er := strconv.Atoi(e.AddAt)
			if er != nil || idx < 0 || idx >= cols {
				return fmt.Errorf("add_columns.at 이 표 밖입니다 — %s (0부터 %d까지, 또는 end·start)", e.AddAt, cols-1)
			}
			addIdx = idx
		}
		colsAfterAdd := cols
		if e.AddAt != "" {
			colsAfterAdd += e.AddCount
		}
		delCols, delRows := uniqDesc(e.DelCols), uniqDesc(e.DelRows)
		for _, c := range delCols {
			if c < 0 || c >= colsAfterAdd {
				return fmt.Errorf("delete_columns 가 표 밖입니다 — %d (0부터 %d까지)", c, colsAfterAdd-1)
			}
		}
		for _, r := range delRows {
			if r < 0 || r >= rows {
				return fmt.Errorf("delete_rows 가 표 밖입니다 — %d (0부터 %d까지)", r, rows-1)
			}
		}

		// 순서는 창과 같다: 병합 → 열 추가 → 열 삭제 → 행 삭제.
		if m := e.Merge; m != nil {
			a, b := cellAt(t, m[0]+1, m[1]+1), cellAt(t, m[2]+1, m[3]+1)
			_, er := oleutil.CallMethod(a, "Merge", b)
			a.Release()
			b.Release()
			if er != nil {
				return fmt.Errorf("병합을 Word 가 거절했습니다: %v", er)
			}
			done = append(done, fmt.Sprintf("(%d,%d)–(%d,%d) 병합", m[0], m[1], m[2], m[3]))
		}
		if e.AddAt != "" {
			// ⚠ **표 전체의 열이 아니라 행마다 칸을 붙인다.** `Columns.Add()` 는 병합된 행이 있는 표에서 끝이 아니라 **마지막 열 앞에**
			// 새 열을 넣었고, 그 열을 끝이라 믿고 값을 채우다 원래 값을 덮어썼다 — 실측 2026-09-26: 「매출 | 100 | 112」가
			// 「매출 | 100 | (빈칸) | 212」가 됐다. 사람의 표에서라면 숫자가 조용히 사라진다. 행마다 `Cells.Add` 로 새 칸을 만들고
			// **그 칸에** 값을 넣으면, 어디에 섰든 값은 새 칸에만 간다.
			rs := wget(t, "Rows")
			var skipped []string
			for r := 1; r <= rows; r++ {
				row := item(rs, r)
				rc := wget(row, "Cells")
				count := wint(rc, "Count")
				for j := 0; j < e.AddCount; j++ {
					var before int // 0 이면 행 끝에 붙인다
					switch {
					case e.AddAt == "end":
					case e.AddAt == "start":
						before = j + 1
					case count == cols: // 병합 없는 행 — 열 번호 그대로
						if addIdx+2 <= cols {
							before = addIdx + 2 + j
						}
					default: // 병합된 행 — 뒤에서 센다(idx 뒤의 원래 열 수만큼 뒤에서)
						tail := cols - 1 - addIdx
						before = count - tail + 1 + j
						if before < 1 {
							before = 1
						}
						if before > count+j {
							before = 0
						}
					}
					var cell *ole.IDispatch
					var er error
					if before == 0 {
						var v *ole.VARIANT
						v, er = oleutil.CallMethod(rc, "Add")
						if er == nil {
							cell = v.ToIDispatch()
						}
					} else {
						b := item(rc, before)
						var v *ole.VARIANT
						v, er = oleutil.CallMethod(rc, "Add", b)
						b.Release()
						if er == nil {
							cell = v.ToIDispatch()
						}
					}
					if er != nil {
						rc.Release()
						row.Release()
						rs.Release()
						return fmt.Errorf("행 %d 에 새 칸을 Word 가 못 넣었습니다: %v", r-1, er)
					}
					if j < len(e.AddValues) && r-1 < len(e.AddValues[j]) {
						cr := wget(cell, "Range")
						oleutil.MustPutProperty(cr, "Text", e.AddValues[j][r-1])
						cr.Release()
					}
					cell.Release()
				}
				if n := wint(rc, "Count"); n != count+e.AddCount {
					skipped = append(skipped, strconv.Itoa(r-1))
				}
				rc.Release()
				row.Release()
			}
			rs.Release()
			cols += e.AddCount
			said := fmt.Sprintf("열 %d개 추가(%s)", e.AddCount, e.AddAt)
			if len(skipped) > 0 {
				said += fmt.Sprintf(" — 행 %s 의 칸 수가 기대와 다릅니다", strings.Join(skipped, "·"))
			}
			done = append(done, said)
		}
		if len(delCols) > 0 {
			cs := wget(t, "Columns")
			for _, c := range delCols {
				col := item(cs, c+1)
				_, er := oleutil.CallMethod(col, "Delete")
				col.Release()
				if er != nil {
					cs.Release()
					return fmt.Errorf("열 %d 을 Word 가 못 지웠습니다(병합된 칸이 걸치면 그렇다): %v", c, er)
				}
			}
			cs.Release()
			cols -= len(delCols)
			done = append(done, fmt.Sprintf("열 %d개 삭제", len(delCols)))
		}
		if len(delRows) > 0 {
			rs := wget(t, "Rows")
			for _, r := range delRows {
				row := item(rs, r+1)
				_, er := oleutil.CallMethod(row, "Delete")
				row.Release()
				if er != nil {
					rs.Release()
					return fmt.Errorf("행 %d 을 Word 가 못 지웠습니다(병합된 칸이 걸치면 그렇다): %v", r, er)
				}
			}
			rs.Release()
			rows -= len(delRows)
			done = append(done, fmt.Sprintf("행 %d개 삭제", len(delRows)))
		}
		return nil
	})
	return
}

func uniqDesc(xs []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// ── 필드 ──

// fieldHost 는 필드 줄이 설 빈 문단을 만들어 그 범위를 준다 — 창의 insert_field 와 같은 자리.
func fieldHost(doc *ole.IDispatch, w wordFieldWhere) (*ole.IDispatch, error) {
	if w.Which != "" {
		ss := wget(doc, "Sections")
		defer ss.Release()
		sec := item(ss, w.Section)
		defer sec.Release()
		coll := "Headers"
		if w.Which == "footer" {
			coll = "Footers"
		}
		hfs := wget(sec, coll)
		defer hfs.Release()
		hf := item(hfs, wordHeaderKindCOM[w.Kind])
		defer hf.Release()
		r := wget(hf, "Range")
		// 비어 있으면(문단 표식 하나) 그 자리를, 아니면 끝에 새 줄을 — 창은 늘 새 줄을 붙인다(insertParagraph('', 'End')).
		if wordText(wstr(r, "Text")) != "" {
			oleutil.MustCallMethod(r, "InsertParagraphAfter")
			ps := wget(r, "Paragraphs")
			last := wget(ps, "Last")
			ps.Release()
			r.Release()
			lr := wget(last, "Range")
			last.Release()
			return lr, nil
		}
		return r, nil
	}
	ps := wget(doc, "Paragraphs")
	defer ps.Release()
	var at int
	switch {
	case w.After != 0:
		p := item(ps, w.After)
		pr := wget(p, "Range")
		oleutil.MustCallMethod(pr, "InsertParagraphAfter")
		pr.Release()
		p.Release()
		at = w.After + 1
	case w.Before != 0:
		p := item(ps, w.Before)
		pr := wget(p, "Range")
		oleutil.MustCallMethod(pr, "InsertParagraphBefore")
		pr.Release()
		p.Release()
		at = w.Before
	case w.At == "start":
		p := item(ps, 1)
		pr := wget(p, "Range")
		oleutil.MustCallMethod(pr, "InsertParagraphBefore")
		pr.Release()
		p.Release()
		at = 1
	default:
		content := wget(doc, "Content")
		oleutil.MustCallMethod(content, "InsertParagraphAfter")
		content.Release()
		ps2 := wget(doc, "Paragraphs")
		at = wint(ps2, "Count")
		ps2.Release()
	}
	ps3 := wget(doc, "Paragraphs")
	defer ps3.Release()
	host := item(ps3, at)
	defer host.Release()
	// 끝 문단의 제목 스타일을 물려받지 않게 — 필드 줄은 본문이다(창과 같은 규칙).
	oleutil.MustPutProperty(host, "Style", -1)
	return wget(host, "Range"), nil
}

func (c comWordDoc) InsertFields(w wordFieldWhere, pieces []wordFieldPiece, align string) (count int, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		host, e := fieldHost(doc, w)
		if e != nil {
			return e
		}
		defer host.Release()
		if align != "" {
			pf := wget(host, "ParagraphFormat")
			oleutil.MustPutProperty(pf, "Alignment", wordAlignCOM[align])
			pf.Release()
		}
		// ⚠ **찾아 바꾸지 않고, 자리를 세며 끼운다.** 처음엔 창처럼 글을 통째로 적고 자리 표시(「⁣F0⁣」)를 찾아 필드로 바꿨는데,
		// COM 의 Find 는 범위 밖으로 이어 찾고(설정이 Word 전체에 끈적하게 남는다), 자리 표시는 호출마다 같은 글자라 — 실측
		// 2026-09-26 — 날짜 필드는 제 자리를 못 찾고, 뒤이은 목차 필드가 **날짜의 남은 자리 표시를 채웠다.** 문서에는 「⁣F0⁣」가 남았다.
		//
		// 그래서 host 를 복제한 범위(같은 이야기 — 본문이든 바닥글이든)를 삽입점으로 삼아 조각을 차례로 넣고, 넣을 때마다 끝으로
		// 옮긴다(SetRange 는 이야기를 바꾸지 않는다). 필드 뒤는 결과 범위 끝 + 1(필드 끝 표식) 이다.
		ip := wget(host, "Duplicate")
		defer ip.Release()
		oleutil.MustCallMethod(ip, "Collapse", 1) // wdCollapseStart
		for _, p := range pieces {
			at := wint(ip, "Start")
			if p.Text != nil {
				oleutil.MustCallMethod(ip, "InsertAfter", *p.Text)
				end := at + len(utf16.Encode([]rune(*p.Text)))
				oleutil.MustCallMethod(ip, "SetRange", end, end)
				continue
			}
			fs := wget(ip, "Fields")
			f := oleutil.MustCallMethod(fs, "Add", ip, p.Type, p.Code, false).ToIDispatch()
			fs.Release()
			oleutil.MustCallMethod(f, "Update") // 목차는 갱신해야 채워진다 — 뒤에 올 조각의 자리는 결과가 다 선 다음에 잰다
			res := wget(f, "Result")
			end := wint(res, "End") + 1
			res.Release()
			f.Release()
			oleutil.MustCallMethod(ip, "SetRange", end, end)
			count++
		}
		return nil
	})
	return
}

// ── 문서 변수(제안) ──

func (c comWordDoc) Variables(prefix string) (out map[string]string, err error) {
	out = map[string]string{}
	err = c.with(func(doc *ole.IDispatch) error {
		vs := wget(doc, "Variables")
		defer vs.Release()
		for i := 1; i <= wint(vs, "Count"); i++ {
			v := item(vs, i)
			if name := wstr(v, "Name"); strings.HasPrefix(name, prefix) {
				out[name] = wstr(v, "Value")
			}
			v.Release()
		}
		return nil
	})
	return
}

func (c comWordDoc) SetVariable(name, value string) error {
	return c.with(func(doc *ole.IDispatch) error {
		vs := wget(doc, "Variables")
		defer vs.Release()
		v, e := oleutil.CallMethod(vs, "Add", name, value)
		if e != nil {
			return e
		}
		v.ToIDispatch().Release()
		return nil
	})
}

func (c comWordDoc) DeleteVariable(name string) (had bool, err error) {
	err = c.with(func(doc *ole.IDispatch) error {
		vs := wget(doc, "Variables")
		defer vs.Release()
		for i := 1; i <= wint(vs, "Count"); i++ {
			v := item(vs, i)
			if wstr(v, "Name") == name {
				_, e := oleutil.CallMethod(v, "Delete")
				v.Release()
				had = e == nil
				return e
			}
			v.Release()
		}
		return nil
	})
	return
}

// pinToPage 는 도형의 자리를 **쪽 기준**으로 건다 — (left, top) 이 쪽의 왼쪽 위에서 잰 pt 가 되게.
//
// ⚠ AddShape 는 기본이 단·문단 기준(RelativeHorizontalPosition=Column, RelativeVerticalPosition=Paragraph)이라, (300, 20) 을
// 주면 목록에 (228, −64) 가 읽혔다(실측 2026-09-26) — 같은 수가 부를 때와 읽을 때 다른 뜻이 된다. 기준을 쪽으로 못박고 다시 건다.
func pinToPage(sh *ole.IDispatch, left, top *float64) {
	if left != nil {
		oleutil.MustPutProperty(sh, "RelativeHorizontalPosition", 1) // wdRelativeHorizontalPositionPage
		oleutil.MustPutProperty(sh, "Left", *left)
	}
	if top != nil {
		oleutil.MustPutProperty(sh, "RelativeVerticalPosition", 1) // wdRelativeVerticalPositionPage
		oleutil.MustPutProperty(sh, "Top", *top)
	}
}
