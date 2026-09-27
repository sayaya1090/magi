//go:build windows

package office

import (
	"fmt"
	"strconv"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// comXLExtra 는 COM 전용 도구의 Windows 쪽 — 호출마다 떠 있는 Excel 을 잡아 이름의 통장을 다시 찾는다(xl_notes_windows.go 와 같은 규칙).
type comXLExtra struct{}

func openXLExtraOS() (xlExtra, error) {
	if err := withExcel(func(*ole.IDispatch) error { return nil }); err != nil {
		return nil, err
	}
	return comXLExtra{}, nil
}

func (comXLExtra) Close() {}

// withBook 은 이름의 통장과 (sheet 가 있으면) 그 시트를 f 에 건넨다.
func withBook(book, sheet string, f func(wb, ws *ole.IDispatch) error) error {
	return withExcel(func(app *ole.IDispatch) error {
		wb, err := bookOf(app, book)
		if err != nil {
			return err
		}
		defer wb.Release()
		var ws *ole.IDispatch
		if sheet != "" {
			if ws, err = sheetOf(wb, sheet); err != nil {
				return err
			}
			defer ws.Release()
		}
		return f(wb, ws)
	})
}

func xrange(ws *ole.IDispatch, address string) (*ole.IDispatch, error) {
	v, err := oleutil.GetProperty(ws, "Range", address)
	if err != nil {
		return nil, fmt.Errorf("주소 %q 를 Excel 이 못 읽습니다", address)
	}
	return v.ToIDispatch(), nil
}

func xcount(d *ole.IDispatch) int   { return intOf(oleutil.MustGetProperty(d, "Count").Value()) }
func xname(d *ole.IDispatch) string { return oleutil.MustGetProperty(d, "Name").ToString() }

func (comXLExtra) FilePath(book string) (out string, err error) {
	err = withBook(book, "", func(wb, _ *ole.IDispatch) error {
		if strings.TrimSpace(oleutil.MustGetProperty(wb, "Path").ToString()) != "" {
			out = oleutil.MustGetProperty(wb, "FullName").ToString()
		}
		return nil
	})
	return out, err
}

// ExportPDF 는 ExportAsFixedFormat(xlTypePDF=0, 경로) — 통장 또는 시트 하나. 통장의 경로·저장 상태는 안 바뀐다.
func (comXLExtra) ExportPDF(book, sheet, path string) error {
	return withBook(book, sheet, func(wb, ws *ole.IDispatch) error {
		target := wb
		if ws != nil {
			target = ws
		}
		_, err := oleutil.CallMethod(target, "ExportAsFixedFormat", 0, path)
		return err
	})
}

func (comXLExtra) GoalSeek(book, sheet, cell string, goal float64, changing string) (g xlGoal, err error) {
	err = withBook(book, sheet, func(_, ws *ole.IDispatch) error {
		t, e := xrange(ws, cell)
		if e != nil {
			return e
		}
		defer t.Release()
		c, e := xrange(ws, changing)
		if e != nil {
			return e
		}
		defer c.Release()
		if hf, _ := oleutil.MustGetProperty(t, "HasFormula").Value().(bool); !hf {
			return fmt.Errorf("%s 에는 수식이 없습니다 — cell 은 결과를 내는 수식 칸이어야 합니다", cell)
		}
		if hf, _ := oleutil.MustGetProperty(c, "HasFormula").Value().(bool); hf {
			return fmt.Errorf("%s 는 수식 칸입니다 — changing 은 값을 넣을 수 있는 입력 칸이어야 합니다", changing)
		}
		g.ValueWas = oleutil.MustGetProperty(t, "Value2").Value()
		g.ChangingWas = oleutil.MustGetProperty(c, "Value2").Value()
		g.TargetFormula = oleutil.MustGetProperty(t, "Formula").ToString()
		r, e := oleutil.CallMethod(t, "GoalSeek", goal, c)
		if e != nil {
			return fmt.Errorf("목표값 찾기가 실패했습니다(%v)", e)
		}
		g.Found, _ = r.Value().(bool)
		g.Value = oleutil.MustGetProperty(t, "Value2").Value()
		g.Changing = oleutil.MustGetProperty(c, "Value2").Value()
		return nil
	})
	return g, err
}

// bgr 은 #RRGGBB 를 Excel 의 색 정수(BGR)로.
func bgr(hex string) int {
	v, _ := strconv.ParseInt(strings.TrimPrefix(hex, "#"), 16, 32)
	r, g, b := (v>>16)&0xFF, (v>>8)&0xFF, v&0xFF
	return int(r | g<<8 | b<<16)
}

func (comXLExtra) AddSparklines(book, sheet, location, source, kind, color string, markers bool) (n int, err error) {
	err = withBook(book, sheet, func(_, ws *ole.IDispatch) error {
		loc, e := xrange(ws, location)
		if e != nil {
			return e
		}
		defer loc.Release()
		src := source
		if !strings.Contains(src, "!") {
			src = "'" + strings.ReplaceAll(sheet, "'", "''") + "'!" + src // 시트 이름 없는 원본은 이 시트다
		}
		groups := oleutil.MustGetProperty(loc, "SparklineGroups").ToIDispatch()
		defer groups.Release()
		typ := map[string]int{"line": 1, "column": 2, "win_loss": 3}[kind] // xlSparkLine·xlSparkColumn·xlSparkColumnStacked100
		v, e := oleutil.CallMethod(groups, "Add", typ, src)
		if e != nil {
			return fmt.Errorf("스파크라인을 못 넣었습니다(%v)", e)
		}
		g := v.ToIDispatch()
		defer g.Release()
		if color != "" {
			sc := oleutil.MustGetProperty(g, "SeriesColor").ToIDispatch()
			oleutil.MustPutProperty(sc, "Color", bgr(color))
			sc.Release()
		}
		if markers {
			pts := oleutil.MustGetProperty(g, "Points").ToIDispatch()
			mk := oleutil.MustGetProperty(pts, "Markers").ToIDispatch()
			oleutil.MustPutProperty(mk, "Visible", true)
			mk.Release()
			pts.Release()
		}
		n = xcount(loc)
		return nil
	})
	return n, err
}

func (comXLExtra) RemoveSparklines(book, sheet, location string) (n int, err error) {
	err = withBook(book, sheet, func(_, ws *ole.IDispatch) error {
		loc, e := xrange(ws, location)
		if e != nil {
			return e
		}
		defer loc.Release()
		groups := oleutil.MustGetProperty(loc, "SparklineGroups").ToIDispatch()
		defer groups.Release()
		n = xcount(groups)
		if n > 0 {
			if _, e := oleutil.CallMethod(groups, "Clear"); e != nil {
				return fmt.Errorf("스파크라인을 못 지웠습니다(%v)", e)
			}
		}
		return nil
	})
	return n, err
}

// sourceOf 는 이름의 표(ListObject) 또는 피벗을 통장 전체에서 찾는다. 열 이름도 같이 준다.
func sourceOf(wb *ole.IDispatch, name string) (obj *ole.IDispatch, kind string, fields []string, err error) {
	wss := oleutil.MustGetProperty(wb, "Worksheets").ToIDispatch()
	defer wss.Release()
	var seen []string
	for i := 1; i <= xcount(wss); i++ {
		ws := item(wss, i)
		los := oleutil.MustGetProperty(ws, "ListObjects").ToIDispatch()
		for j := 1; j <= xcount(los); j++ {
			lo := item(los, j)
			n := xname(lo)
			if obj == nil && strings.EqualFold(n, name) {
				obj, kind = lo, "table"
				cols := oleutil.MustGetProperty(lo, "ListColumns").ToIDispatch()
				for k := 1; k <= xcount(cols); k++ {
					c := item(cols, k)
					fields = append(fields, xname(c))
					c.Release()
				}
				cols.Release()
				continue
			}
			seen = append(seen, n)
			lo.Release()
		}
		los.Release()
		pts := oleutil.MustCallMethod(ws, "PivotTables").ToIDispatch()
		for j := 1; j <= xcount(pts); j++ {
			pt := oleutil.MustCallMethod(pts, "Item", j).ToIDispatch()
			n := xname(pt)
			if obj == nil && strings.EqualFold(n, name) {
				obj, kind = pt, "pivot"
				pfs := oleutil.MustCallMethod(pt, "PivotFields").ToIDispatch()
				for k := 1; k <= xcount(pfs); k++ {
					f := oleutil.MustCallMethod(pfs, "Item", k).ToIDispatch()
					fields = append(fields, xname(f))
					f.Release()
				}
				pfs.Release()
				continue
			}
			seen = append(seen, n)
			pt.Release()
		}
		pts.Release()
		ws.Release()
	}
	if obj == nil {
		have := "(없음)"
		if len(seen) > 0 {
			have = strings.Join(seen, ", ")
		}
		return nil, "", nil, fmt.Errorf("표나 피벗 %q 이 없습니다 — 있는 것: %s", name, have)
	}
	return obj, kind, fields, nil
}

func (comXLExtra) AddSlicer(book, source, field, sheet, name string, left, top, width, height float64) (s xlSlicer, err error) {
	err = withBook(book, sheet, func(wb, ws *ole.IDispatch) error {
		src, kind, fields, e := sourceOf(wb, source)
		if e != nil {
			return e
		}
		defer src.Release()
		ok := false
		for _, f := range fields {
			if strings.EqualFold(f, field) {
				field, ok = f, true
			}
		}
		if !ok {
			return fmt.Errorf("%s 에 %q 열이 없습니다 — 있는 것: %s", source, field, strings.Join(fields, ", "))
		}
		caches := oleutil.MustGetProperty(wb, "SlicerCaches").ToIDispatch()
		defer caches.Release()
		cv, e := oleutil.CallMethod(caches, "Add2", src, field)
		if e != nil {
			return fmt.Errorf("슬라이서 캐시를 못 만들었습니다(%v) — 그 열에 이미 슬라이서가 있으면 Excel 이 거절합니다", e)
		}
		cache := cv.ToIDispatch()
		defer cache.Release()
		sls := oleutil.MustGetProperty(cache, "Slicers").ToIDispatch()
		defer sls.Release()
		v, e := oleutil.CallMethod(sls, "Add", ws)
		if e != nil {
			_, _ = oleutil.CallMethod(cache, "Delete")
			return fmt.Errorf("슬라이서를 못 놓았습니다(%v)", e)
		}
		sl := v.ToIDispatch()
		defer sl.Release()
		if name != "" {
			if _, e := oleutil.PutProperty(sl, "Name", name); e != nil {
				return fmt.Errorf("슬라이서는 놓았는데 이름 %q 를 못 붙였습니다(%v) — 같은 이름이 있으면 Excel 이 거절합니다", name, e)
			}
		}
		for k, v := range map[string]float64{"Left": left, "Top": top, "Width": width, "Height": height} {
			if v > 0 {
				oleutil.MustPutProperty(sl, k, v)
			}
		}
		s = xlSlicer{Name: xname(sl), Caption: oleutil.MustGetProperty(sl, "Caption").ToString(), Sheet: xname(ws), Kind: kind}
		return nil
	})
	return s, err
}

func (comXLExtra) RemoveSlicer(book, name string) error {
	return withBook(book, "", func(wb, _ *ole.IDispatch) error {
		caches := oleutil.MustGetProperty(wb, "SlicerCaches").ToIDispatch()
		defer caches.Release()
		var seen []string
		for i := 1; i <= xcount(caches); i++ {
			cache := item(caches, i)
			sls := oleutil.MustGetProperty(cache, "Slicers").ToIDispatch()
			for j := 1; j <= xcount(sls); j++ {
				sl := item(sls, j)
				n := xname(sl)
				if strings.EqualFold(n, name) {
					// 지우기 **전에** 센다 — 마지막 슬라이서를 지우면 Excel 이 캐시를 같이 치우고, 그 뒤로 이 Slicers 를 만지면
					// 「Object required」(0x800A01A8)로 죽는다. 지우는 것은 됐는데 답이 실패가 됐다(실물 2021, 2026-09-27 시나리오 XL-2).
					last := xcount(sls) == 1
					oleutil.MustCallMethod(sl, "Delete")
					sl.Release()
					if last {
						// 슬라이서가 안 남은 캐시는 통장에 보이지 않는 짐으로 남을 수 있다 — Excel 이 이미 치웠으면 이 호출은 헛돈다.
						_, _ = oleutil.CallMethod(cache, "Delete")
					}
					sls.Release() // Release 는 참조만 놓는다 — 멤버를 부르지 않으므로 치워진 객체여도 안전하다
					cache.Release()
					return nil
				}
				seen = append(seen, n)
				sl.Release()
			}
			sls.Release()
			cache.Release()
		}
		if len(seen) == 0 {
			return fmt.Errorf("이 통장에는 슬라이서가 없습니다")
		}
		return fmt.Errorf("슬라이서 %q 이 없습니다 — 있는 것: %s", name, strings.Join(seen, ", "))
	})
}
