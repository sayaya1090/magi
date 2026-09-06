//go:build windows

package office

import (
	"fmt"
	"runtime"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// comNoter 는 떠 있는 Excel 을 COM 으로 잡아 노트를 다룬다. 호출마다 OS 스레드 하나를 잠그고 STA 로 초기화한다 —
// Excel 의 COM 은 아파트먼트가 다르면 조용히 이상해진다.
type comNoter struct{}

func openXLNoterOS() (xlNoter, error) {
	// 잡히는지만 본다 — 안 떠 있으면 여기서 이유를 댄다.
	if err := withExcel(func(*ole.IDispatch) error { return nil }); err != nil {
		return nil, err
	}
	return comNoter{}, nil
}

func (comNoter) Close() {}

func withExcel(f func(app *ole.IDispatch) error) (err error) {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("Excel COM: %v", r)
			}
		}()
		if e := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); e != nil {
			done <- e
			return
		}
		defer ole.CoUninitialize()
		unk, e := oleutil.GetActiveObject("Excel.Application")
		if e != nil {
			done <- fmt.Errorf("떠 있는 Excel 을 COM 으로 못 잡았습니다(%v) — Excel 이 켜져 있고 통장이 열려 있어야 합니다", e)
			return
		}
		defer unk.Release()
		app, e := unk.QueryInterface(ole.IID_IDispatch)
		if e != nil {
			done <- e
			return
		}
		defer app.Release()
		done <- f(app)
	}()
	return <-done
}

// item 은 컬렉션의 i 번째 — Excel 의 Item 은 컬렉션마다 속성이기도 메서드이기도 하다(Comments.Item 은 속성 호출이 「구성원이 없습니다」로 죽었다, 실물 2026-09-07).
func item(coll *ole.IDispatch, i int) *ole.IDispatch {
	if v, err := oleutil.GetProperty(coll, "Item", i); err == nil {
		return v.ToIDispatch()
	}
	return oleutil.MustCallMethod(coll, "Item", i).ToIDispatch()
}

// bookOf 는 이름의 통장, 이름이 없으면 열린 것이 하나일 때 그것.
func bookOf(app *ole.IDispatch, name string) (*ole.IDispatch, error) {
	wbs := oleutil.MustGetProperty(app, "Workbooks").ToIDispatch()
	defer wbs.Release()
	count := int(oleutil.MustGetProperty(wbs, "Count").Val)
	var names []string
	for i := 1; i <= count; i++ {
		wb := item(wbs, i)
		n := oleutil.MustGetProperty(wb, "Name").ToString()
		if name != "" && strings.EqualFold(n, name) {
			return wb, nil
		}
		names = append(names, n)
		wb.Release()
	}
	if name == "" && count == 1 {
		return item(wbs, 1), nil
	}
	if name == "" {
		return nil, fmt.Errorf("열린 통장이 %d개라 어느 것인지 모릅니다(%s) — 창의 list_sheets 가 통장 이름을 줘야 합니다", count, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("COM 으로 잡은 Excel 에 통장 %q 이 없습니다(열린 것: %s)", name, strings.Join(names, ", "))
}

func sheetOf(wb *ole.IDispatch, name string) (*ole.IDispatch, error) {
	if name == "" {
		return oleutil.MustGetProperty(wb, "ActiveSheet").ToIDispatch(), nil
	}
	wss := oleutil.MustGetProperty(wb, "Worksheets").ToIDispatch()
	defer wss.Release()
	v, err := oleutil.GetProperty(wss, "Item", name)
	if err != nil {
		return nil, fmt.Errorf("시트 %q 이 없습니다", name)
	}
	return v.ToIDispatch(), nil
}

func noteAt(ws *ole.IDispatch, address string) (rng, cm *ole.IDispatch, err error) {
	v, err := oleutil.GetProperty(ws, "Range", address)
	if err != nil {
		return nil, nil, fmt.Errorf("주소 %q 를 Excel 이 못 읽습니다", address)
	}
	rng = v.ToIDispatch()
	c, err := oleutil.GetProperty(rng, "Comment")
	if err != nil {
		return rng, nil, nil
	}
	cm = c.ToIDispatch()
	return rng, cm, nil
}

func sheetNotes(ws *ole.IDispatch) []xlNote {
	sheetName := oleutil.MustGetProperty(ws, "Name").ToString()
	cms := oleutil.MustGetProperty(ws, "Comments").ToIDispatch()
	defer cms.Release()
	count := int(oleutil.MustGetProperty(cms, "Count").Val)
	out := make([]xlNote, 0, count)
	for i := 1; i <= count; i++ {
		c := item(cms, i)
		parent := oleutil.MustGetProperty(c, "Parent").ToIDispatch()
		addr := oleutil.MustGetProperty(parent, "Address", false, false).ToString()
		parent.Release()
		out = append(out, xlNote{Sheet: sheetName, Address: addr, Author: oleutil.MustGetProperty(c, "Author").ToString(), Text: oleutil.MustCallMethod(c, "Text").ToString()})
		c.Release()
	}
	return out
}

func (comNoter) Notes(book, sheet string) (out []xlNote, err error) {
	err = withExcel(func(app *ole.IDispatch) error {
		wb, e := bookOf(app, book)
		if e != nil {
			return e
		}
		defer wb.Release()
		if sheet != "" {
			ws, e := sheetOf(wb, sheet)
			if e != nil {
				return e
			}
			defer ws.Release()
			out = sheetNotes(ws)
			return nil
		}
		wss := oleutil.MustGetProperty(wb, "Worksheets").ToIDispatch()
		defer wss.Release()
		count := int(oleutil.MustGetProperty(wss, "Count").Val)
		for i := 1; i <= count; i++ {
			ws := item(wss, i)
			out = append(out, sheetNotes(ws)...)
			ws.Release()
		}
		return nil
	})
	return out, err
}

func (comNoter) Add(book, sheet, address, text string) (note xlNote, replied bool, err error) {
	err = withExcel(func(app *ole.IDispatch) error {
		wb, e := bookOf(app, book)
		if e != nil {
			return e
		}
		defer wb.Release()
		ws, e := sheetOf(wb, sheet)
		if e != nil {
			return e
		}
		defer ws.Release()
		rng, cm, e := noteAt(ws, address)
		if e != nil {
			return e
		}
		defer rng.Release()
		if cm != nil {
			defer cm.Release()
			old := oleutil.MustCallMethod(cm, "Text").ToString()
			joined := strings.TrimRight(old, "\n") + "\n" + text
			if _, e := oleutil.CallMethod(cm, "Text", joined); e != nil {
				return fmt.Errorf("노트에 글을 못 덧붙였습니다: %v", e)
			}
			replied = true
		} else {
			v, e := oleutil.CallMethod(rng, "AddComment", text)
			if e != nil {
				return fmt.Errorf("노트를 못 넣었습니다: %v", e)
			}
			cm = v.ToIDispatch()
			defer cm.Release()
		}
		note = xlNote{
			Sheet:   oleutil.MustGetProperty(ws, "Name").ToString(),
			Address: oleutil.MustGetProperty(rng, "Address", false, false).ToString(),
			Author:  oleutil.MustGetProperty(cm, "Author").ToString(),
			Text:    oleutil.MustCallMethod(cm, "Text").ToString(),
		}
		return nil
	})
	return note, replied, err
}

func (comNoter) Delete(book, sheet, address string) (gone bool, err error) {
	err = withExcel(func(app *ole.IDispatch) error {
		wb, e := bookOf(app, book)
		if e != nil {
			return e
		}
		defer wb.Release()
		ws, e := sheetOf(wb, sheet)
		if e != nil {
			return e
		}
		defer ws.Release()
		rng, cm, e := noteAt(ws, address)
		if e != nil {
			return e
		}
		defer rng.Release()
		if cm == nil {
			return nil
		}
		defer cm.Release()
		if _, e := oleutil.CallMethod(cm, "Delete"); e != nil {
			return fmt.Errorf("노트를 못 지웠습니다: %v", e)
		}
		gone = true
		return nil
	})
	return gone, err
}
