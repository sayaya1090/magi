package office

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

// Excel 의 COM 전용 도구 여섯 — PDF, 목표값 찾기, 스파크라인 두, 슬라이서 두. 창(Office.js)에는 PDF·목표값·스파크라인의 길이가
// 아예 없고, 슬라이서는 ExcelApi 1.10 이라 2021 에서 막혀 있다(com_local.go 머리 주석).

// xlGoal 은 목표값 찾기의 앞뒤.
type xlGoal struct {
	Found                       bool
	ValueWas, Value             any
	ChangingWas, Changing       any
	TargetFormula, ChangingAddr string
}

// xlSlicer 는 만든 슬라이서.
type xlSlicer struct {
	Name, Caption, Sheet, Kind string
}

// xlExtra 는 COM 전용 도구가 닿는 구멍 — Windows 에서는 COM(xl_extra_windows.go), 시험에서는 가짜. book 은 창의 list_sheets 가 준 통장 이름.
type xlExtra interface {
	FilePath(book string) (string, error)
	// ExportPDF — sheet 가 비면 통장 전체.
	ExportPDF(book, sheet, path string) error
	GoalSeek(book, sheet, cell string, goal float64, changing string) (xlGoal, error)
	// AddSparklines 는 location 의 칸마다 source 의 한 줄(또는 한 열)을 작은 차트로. 돌려주는 값은 칸 수.
	AddSparklines(book, sheet, location, source, kind, color string, markers bool) (int, error)
	// RemoveSparklines 는 location 과 겹치는 스파크라인을 지운다. 돌려주는 값은 지운 무리 수(0 이면 없었다).
	RemoveSparklines(book, sheet, location string) (int, error)
	AddSlicer(book, source, field, sheet, name string, left, top, width, height float64) (xlSlicer, error)
	// RemoveSlicer — 없으면 있는 이름들을 대며 오류.
	RemoveSlicer(book, name string) error
	Close()
}

var openXLExtra = openXLExtraOS

var xlCellRef = regexp.MustCompile(`^\$?[A-Za-z]{1,3}\$?[0-9]{1,7}$`)
var xlHexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// xlLocal 은 헬퍼가 답하는 Excel 도구 하나 — 창에게 통장 이름만 묻고 COM 으로 한다(xl_notes.go 와 같은 이유: 이름 없이는 안 간다).
func xlLocal(name string) func(Hand, string, map[string]any) (HandResult, error) {
	return func(hand Hand, where string, args map[string]any) (HandResult, error) {
		if !comLocalOnThisOS {
			return HandResult{}, fmt.Errorf("%s 은 Windows 의 Excel 에서만 됩니다(COM) — 이 컴퓨터에는 그 길이 없습니다", name)
		}
		ctx, cancel := comLocalContext()
		defer cancel()
		ls, err := hand.Call(ctx, where, "list_sheets", map[string]any{})
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: 어느 통장인지 창이 답하지 못했습니다(%v)", name, err)
		}
		book, _ := ls.Result["workbook"].(string)
		active, _ := ls.Result["active"].(string)
		if strings.TrimSpace(book) == "" {
			return HandResult{}, fmt.Errorf("%s: 창이 통장 이름을 안 줘서 어느 통장인지 모릅니다", name)
		}
		doc := where
		if ls.Document != "" {
			doc = ls.Document
		}
		x, err := openXLExtra()
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: %v", name, err)
		}
		defer x.Close()
		res, changed, err := xlExtraRun(x, book, active, ls.Label, name, args)
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: %v", name, err)
		}
		res["via"] = "com"
		return HandResult{Document: doc, Label: ls.Label, Result: res, Changed: changed}, nil
	}
}

func xlNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case interface{ Float64() (float64, error) }: // json.Number
		f, err := x.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscan(strings.TrimSpace(x), &f)
		return f, err == nil
	}
	return 0, false
}

func xlExtraRun(x xlExtra, book, active, label, name string, args map[string]any) (map[string]any, []string, error) {
	sheet := xlStr(args["sheet"])
	if sheet == "" {
		sheet = xlStr(args["worksheet"])
	}
	address := xlStr(args["address"])
	if address == "" {
		address = xlStr(args["range"])
	}
	useSheet := sheet
	if useSheet == "" {
		useSheet = active
	}
	switch name {
	case "export_pdf":
		saved, err := x.FilePath(book)
		if err != nil {
			return nil, nil, err
		}
		base := label
		if base == "" {
			base = book
		}
		if sheet != "" {
			// 시트 하나면 통장 이름 뒤에 시트 이름을 붙인다 — 통장 전체의 PDF 를 덮지 않게.
			tail := " - " + unsafeFileChars.ReplaceAllString(sheet, "_")
			base += tail
			if saved != "" {
				saved = strings.TrimSuffix(saved, filepath.Ext(saved)) + tail + filepath.Ext(saved)
			}
		}
		path, err := pdfTarget(saved, base, xlStr(args["path"]), comBool(args, "overwrite"))
		if err != nil {
			return nil, nil, err
		}
		if err := x.ExportPDF(book, sheet, path); err != nil {
			return nil, nil, fmt.Errorf("%v — 인쇄할 것이 없는 시트는 Excel 이 PDF 로 안 냅니다", err)
		}
		what := "통장 전체"
		if sheet != "" {
			what = "시트 " + sheet
		}
		return pdfDone(path, map[string]any{"what": what})

	case "goal_seek":
		cell := xlStr(args["cell"])
		changing := xlStr(args["changing"])
		goal, ok := xlNum(args["goal"])
		if cell == "" || changing == "" || !ok {
			return nil, nil, fmt.Errorf("cell(수식이 든 칸)·goal(맞출 값)·changing(바꿀 칸) 이 다 있어야 합니다 — 예: cell B10, goal 1000, changing B2")
		}
		if !xlCellRef.MatchString(cell) || !xlCellRef.MatchString(changing) {
			return nil, nil, fmt.Errorf("cell 과 changing 은 칸 하나의 주소입니다(시트 이름 없이, sheet 로 고름) — %q, %q", cell, changing)
		}
		if strings.EqualFold(strings.ReplaceAll(cell, "$", ""), strings.ReplaceAll(changing, "$", "")) {
			return nil, nil, fmt.Errorf("cell 과 changing 이 같은 칸입니다")
		}
		g, err := x.GoalSeek(book, useSheet, cell, goal, changing)
		if err != nil {
			return nil, nil, err
		}
		got, _ := xlNum(g.Value)
		found := g.Found && math.Abs(got-goal) <= 0.001*math.Max(1, math.Abs(goal))
		out := map[string]any{"sheet": useSheet, "cell": cell, "goal": goal, "found": found, "value": g.Value, "value_was": g.ValueWas,
			"changing": changing, "changing_value": g.Changing, "changing_was": g.ChangingWas}
		if !found {
			return out, []string{fmt.Sprintf("목표값을 못 찾았습니다 — %s!%s 가 %v 가 되는 %s 값이 없거나 Excel 이 수렴하지 못했습니다. %s 는 지금 %v(원래 %v)입니다 — 되돌리려면 그 값을 다시 쓰세요",
				useSheet, cell, goal, changing, changing, g.Changing, g.ChangingWas)}, nil
		}
		return out, []string{fmt.Sprintf("%s!%s 를 %v 로 맞추려면 %s = %v (원래 %v) — %s 에 그 값을 넣었습니다", useSheet, cell, goal, changing, g.Changing, g.ChangingWas, changing)}, nil

	case "add_sparklines":
		source := xlStr(args["source"])
		if address == "" || source == "" {
			return nil, nil, fmt.Errorf("address(그릴 칸들)와 source(데이터 범위)가 있어야 합니다 — 예: address F2:F5, source B2:E5")
		}
		kind := strings.ToLower(xlStr(args["kind"]))
		if kind == "" {
			kind = "line"
		}
		if kind != "line" && kind != "column" && kind != "win_loss" {
			return nil, nil, fmt.Errorf("kind 는 line, column, win_loss 중 하나 — %q", kind)
		}
		color := xlStr(args["color"])
		if color != "" && !xlHexColor.MatchString(color) {
			return nil, nil, fmt.Errorf("color 는 #RRGGBB — %q", color)
		}
		markers := comBool(args, "markers")
		if markers && kind != "line" {
			return nil, nil, fmt.Errorf("markers 는 line 에만 있습니다")
		}
		n, err := x.AddSparklines(book, useSheet, address, source, kind, color, markers)
		if err != nil {
			return nil, nil, fmt.Errorf("%v — address 의 칸 수가 source 의 줄(또는 열) 수와 같아야 합니다", err)
		}
		return map[string]any{"sheet": useSheet, "address": address, "source": source, "kind": kind, "cells": n},
			[]string{fmt.Sprintf("%s!%s 에 %s 스파크라인 %d개 — 데이터 %s", useSheet, address, map[string]string{"line": "꺾은선", "column": "열", "win_loss": "승패"}[kind], n, source)}, nil

	case "remove_sparklines":
		if address == "" {
			return nil, nil, fmt.Errorf("address 가 없습니다 — 스파크라인이 있는 칸")
		}
		n, err := x.RemoveSparklines(book, useSheet, address)
		if err != nil {
			return nil, nil, err
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("%s!%s 에는 스파크라인이 없습니다", useSheet, address)
		}
		return map[string]any{"sheet": useSheet, "address": address, "groups": n}, []string{fmt.Sprintf("%s!%s 의 스파크라인을 지웠습니다(무리 %d개) — 칸의 값은 그대로입니다", useSheet, address, n)}, nil

	case "add_slicer":
		source, field := xlStr(args["source"]), xlStr(args["field"])
		if source == "" || field == "" {
			return nil, nil, fmt.Errorf("source(표 또는 피벗 이름)와 field(거를 열 이름)가 있어야 합니다")
		}
		num := func(k string) float64 { f, _ := xlNum(args[k]); return f }
		s, err := x.AddSlicer(book, source, field, useSheet, xlStr(args["name"]), num("left"), num("top"), num("width"), num("height"))
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"slicer": s.Name, "caption": s.Caption, "sheet": s.Sheet, "source": source, "source_kind": s.Kind, "field": field},
			[]string{fmt.Sprintf("시트 %s 에 슬라이서 「%s」 — %s %s 의 %s 열을 단추로 거릅니다(누르면 그 %s 가 걸러집니다)", s.Sheet, s.Name, map[string]string{"table": "표", "pivot": "피벗"}[s.Kind], source, field, map[string]string{"table": "표", "pivot": "피벗"}[s.Kind])}, nil

	case "remove_slicer":
		n := xlStr(args["name"])
		if n == "" {
			return nil, nil, fmt.Errorf("name 이 없습니다 — add_slicer 가 준 슬라이서 이름")
		}
		if err := x.RemoveSlicer(book, n); err != nil {
			return nil, nil, err
		}
		return map[string]any{"removed": n}, []string{fmt.Sprintf("슬라이서 「%s」 를 지웠습니다 — 걸러 두었던 것은 풀리지 않을 수 있으니 filter_table 로 확인하세요", n)}, nil
	}
	return nil, nil, fmt.Errorf("모르는 도구 %s", name)
}

// xlComLocalTools 는 Excel 카탈로그에 붙는 COM 전용 도구들 — Windows 가 아니면 hostcaps 가 목록에서 뺀다.
func xlComLocalTools(declare string) []tool {
	sheet := sheetProp
	return []tool{
		{
			Name: "export_pdf",
			Desc: "Save the workbook — or one sheet — as a PDF file (the workbook itself is untouched). Without path it goes next " +
				"to the workbook, or into the person's Documents folder if it was never saved. Page layout comes from " +
				"set_page_setup. Refuses to overwrite an existing file unless overwrite is true. Windows desktop Excel only." + declare,
			Props: []property{
				{Name: "sheet", Type: "string", Topic: true, Desc: "Only this sheet (default: the whole workbook)."},
				{Name: "path", Type: "string", Desc: "Full path ending in .pdf."},
				{Name: "overwrite", Type: "boolean", Desc: "Replace an existing file."},
			},
			Local: xlLocal("export_pdf"),
		},
		{
			Name: "goal_seek",
			Desc: "Goal Seek (목표값 찾기): change one input cell until a formula cell reaches a value — 「이익이 1000 이 되려면 매출이 " +
				"얼마?」. The input cell is left at the answer; the result says what it was before. Windows desktop Excel only." + declare,
			Props: []property{
				sheet,
				{Name: "cell", Type: "string", Desc: "The formula cell to hit the goal, e.g. B10. Required."},
				{Name: "goal", Type: "number", Desc: "The value it should become. Required."},
				{Name: "changing", Type: "string", Desc: "The input cell Excel may change (a constant, not a formula), e.g. B2. Required."},
			},
			Required: []string{"cell", "goal", "changing"},
			Local:    xlLocal("goal_seek"),
		},
		{
			Name: "add_sparklines",
			Desc: "Tiny in-cell charts (스파크라인): one per cell of address, each drawing one row (or column) of source. " +
				"address has as many cells as source has rows, e.g. address F2:F5 for source B2:E5. Windows desktop Excel only." + declare,
			Props: []property{
				sheet,
				{Name: "address", Type: "string", Desc: "Cells to draw in, one row or one column. Required."},
				{Name: "source", Type: "string", Desc: "Data range (may be Sheet!A1:D4). Required."},
				{Name: "kind", Type: "string", Desc: "line (default), column, win_loss.", Enum: []string{"line", "column", "win_loss"}},
				{Name: "color", Type: "string", Desc: "#RRGGBB."},
				{Name: "markers", Type: "boolean", Desc: "Dots on each point (line only)."},
			},
			Required: []string{"address", "source"},
			Local:    xlLocal("add_sparklines"),
		},
		{
			Name:     "remove_sparklines",
			Desc:     "Remove the sparklines in these cells (the cell values stay). Windows desktop Excel only." + declare,
			Props:    []property{sheet, {Name: "address", Type: "string", Desc: "Cells with sparklines. Required."}},
			Required: []string{"address"},
			Local:    xlLocal("remove_sparklines"),
		},
		{
			Name: "add_slicer",
			Desc: "A slicer (슬라이서): filter buttons for one column of a table or one field of a PivotTable, placed on a sheet. " +
				"The person clicks the buttons to filter. Refuses with the real column names if field is not one. Windows desktop Excel only." + declare,
			Props: []property{
				{Name: "source", Type: "string", Desc: "Table name or PivotTable name. Required."},
				{Name: "field", Type: "string", Desc: "Column (table) or field (pivot) name. Required."},
				{Name: "sheet", Type: "string", Topic: true, Desc: "Sheet to put the slicer on (default: the one the person is looking at)."},
				{Name: "name", Type: "string", Desc: "Slicer name (default: Excel's)."},
				{Name: "left", Type: "number", Desc: "Points."},
				{Name: "top", Type: "number", Desc: "Points."},
				{Name: "width", Type: "number", Desc: "Points."},
				{Name: "height", Type: "number", Desc: "Points."},
			},
			Required: []string{"source", "field"},
			Local:    xlLocal("add_slicer"),
		},
		{
			Name:     "remove_slicer",
			Desc:     "Remove a slicer by name (from add_slicer). Windows desktop Excel only." + declare,
			Props:    []property{{Name: "name", Type: "string", Desc: "Slicer name. Required."}},
			Required: []string{"name"},
			Local:    xlLocal("remove_slicer"),
		},
	}
}
