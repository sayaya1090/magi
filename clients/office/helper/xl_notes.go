package office

import (
	"context"
	"fmt"
	"strings"
)

// 엑셀 2021(볼륨 판)은 요구 집합 1.10 을 ✓ 라 하면서 스레드 메모 API 를 안 준다 — Office.js 도 COM(VBA)도
// 「이 버전에서는 스레드 주석 개체 모델을 지원하지 않는다」(실물 2026-09-07). 그 판에서 프로그램이 셀에 남길 수
// 있는 것은 **노트(옛 메모)** 뿐이고, 그것도 COM 으로만 된다(Office.js 의 노트 API 는 1.18). 그래서 창의 손이
// NotImplemented 로 죽으면 헬퍼가 열린 Excel 을 COM 으로 잡아 노트로 대신한다 — Windows 에서만, 메모 도구 셋만.
//
// 365 의 메모와 다른 것을 숨기지 않는다: 답글은 노트 글 뒤에 덧붙고, 해결 표시는 없다(지우기만 된다). 답이 그것을
// `kind: "note"` 와 문장으로 말한다 — 사람이 통장에서 보는 것이 풍선 노트지 대화 카드가 아니다.

// xlNote 는 셀 하나의 노트.
type xlNote struct {
	Sheet   string `json:"sheet"`
	Address string `json:"address"`
	Author  string `json:"author"`
	Text    string `json:"text"`
}

// xlNoter 는 Excel 의 노트에 닿는 구멍 — Windows 에서는 COM(xl_notes_windows.go), 시험에서는 가짜.
// book 은 통장 이름(창의 list_sheets 가 준 것) — 헬퍼는 이름 없이는 안 부른다(다른 Excel 인스턴스를 잡을 수 있다).
type xlNoter interface {
	// Notes 는 시트의 노트 전부 — sheet "" 이면 통장의 모든 시트.
	Notes(book, sheet string) ([]xlNote, error)
	// Add 는 노트를 만든다. 이미 있으면 글을 덧붙이고 replied 를 켠다(답글 대신).
	Add(book, sheet, address, text string) (note xlNote, replied bool, err error)
	// Delete 는 노트를 지운다. 없으면 false.
	Delete(book, sheet, address string) (bool, error)
	Close()
}

// openXLNoter 는 플랫폼이 정한다 — 시험은 가짜로 바꿔 끼운다.
var openXLNoter = openXLNoterOS

var xlNoteTools = map[string]bool{"add_comment": true, "read_comments": true, "resolve_comment": true}

const xlNoteWhy = "이 Excel 판은 스레드 메모 API 를 안 줍니다(볼륨 판 2021) — 노트(옛 메모)로 대신했습니다. 답글은 노트 글 뒤에 붙고 해결 표시는 없습니다"

// xlNotesFallback 은 창의 손이 NotImplemented 로 거절한 메모 도구를 COM 노트로 대신한다.
// 두 번째 값이 false 면 이 길이 아니다 — 원래 오류가 그대로 간다.
func xlNotesFallback(ctx context.Context, hand Hand, where, name string, args map[string]any, handErr string) (HandResult, bool, error) {
	if !xlNoteTools[name] || !strings.Contains(handErr, "NotImplemented") {
		return HandResult{}, false, nil
	}
	n, err := openXLNoter()
	if err != nil {
		return HandResult{}, true, fmt.Errorf("%s: 이 Excel 판은 스레드 메모 API 를 안 주고(NotImplemented), 노트로 대신할 길도 이 머신에는 없습니다(%v). Microsoft 365 의 Excel 에서는 됩니다", name, err)
	}
	defer n.Close()

	// **통장 이름 없이는 안 간다.** COM 의 GetActiveObject 가 잡는 Excel 이 창의 것과 다른 인스턴스일 수 있고(관리자 Excel, 둘째 Excel),
	// 「열린 통장이 하나」는 그 인스턴스 안의 얘기다 — 이름이 있어야 같은 통장이다(리뷰 2026-09-07). 2021 은 1.7 이라 창이 늘 준다.
	ls, lerr := hand.Call(ctx, where, "list_sheets", map[string]any{})
	if lerr != nil {
		return HandResult{}, true, fmt.Errorf("%s: 이 Excel 판은 스레드 메모 API 를 안 줘 노트로 대신하려 했는데, 어느 통장인지 창이 답하지 못했습니다(%v)", name, lerr)
	}
	book, _ := ls.Result["workbook"].(string)
	active, _ := ls.Result["active"].(string)
	if strings.TrimSpace(book) == "" {
		return HandResult{}, true, fmt.Errorf("%s: 이 Excel 판은 스레드 메모 API 를 안 줘 노트로 대신하려 했는데, 창이 통장 이름을 안 줘서 어느 통장인지 모릅니다", name)
	}
	doc, label := where, ls.Label
	if ls.Document != "" {
		doc = ls.Document
	}
	sheet := xlStr(args["sheet"])
	if sheet == "" {
		sheet = xlStr(args["worksheet"])
	}
	address := xlStr(args["address"])
	if address == "" {
		address = xlStr(args["range"])
	}
	envelope := func(result map[string]any, changed ...string) (HandResult, bool, error) {
		result["kind"] = "note"
		result["note"] = xlNoteWhy
		return HandResult{Document: doc, Label: label, Result: result, Changed: changed}, true, nil
	}

	switch name {
	case "read_comments":
		notes, err := n.Notes(book, sheet)
		if err != nil {
			return HandResult{}, true, err
		}
		rows := make([]any, 0, len(notes))
		for _, x := range notes {
			rows = append(rows, map[string]any{"sheet": x.Sheet, "address": x.Address, "author": x.Author, "text": x.Text, "kind": "note", "replies": []any{}, "resolved": nil})
		}
		return envelope(map[string]any{"comments": rows, "count": len(rows), "sheet": sheet})
	case "add_comment":
		text := xlStr(args["text"])
		if address == "" || text == "" {
			return HandResult{}, true, fmt.Errorf("add_comment: address 와 text 가 있어야 합니다")
		}
		if strings.Contains(address, ":") {
			return HandResult{}, true, fmt.Errorf("add_comment: 노트는 셀 하나에 붙습니다 — %s 가 아니라 셀 하나를 주세요", address)
		}
		if sheet == "" {
			sheet = active
		}
		note, replied, err := n.Add(book, sheet, address, text)
		if err != nil {
			return HandResult{}, true, err
		}
		said := fmt.Sprintf("%s!%s 에 노트를 넣었습니다(스레드 메모 대신 — 이 Excel 판은 메모 API 가 없습니다)", note.Sheet, note.Address)
		if replied {
			said = fmt.Sprintf("%s!%s 의 노트에 글을 덧붙였습니다(답글 대신 — 노트는 스레드가 없습니다)", note.Sheet, note.Address)
		}
		return envelope(map[string]any{"sheet": note.Sheet, "address": note.Address, "author": note.Author, "text": note.Text, "replied": replied}, said)
	default: // resolve_comment
		if address == "" || strings.Contains(address, ":") {
			return HandResult{}, true, fmt.Errorf("resolve_comment: 셀 하나의 address 가 있어야 합니다(받은 것: %q)", address)
		}
		if sheet == "" {
			sheet = active
		}
		if del, _ := args["delete"].(bool); !del {
			return HandResult{}, true, fmt.Errorf("resolve_comment: %s!%s 는 노트라 해결 표시가 없습니다(이 Excel 판은 스레드 메모 API 가 없어 노트로 대신합니다) — delete:true 로 지우거나 그대로 두세요", sheet, address)
		}
		gone, err := n.Delete(book, sheet, address)
		if err != nil {
			return HandResult{}, true, err
		}
		if !gone {
			return HandResult{}, true, fmt.Errorf("resolve_comment: %s!%s 에는 노트가 없습니다 — read_comments 가 자리를 줍니다", sheet, address)
		}
		return envelope(map[string]any{"sheet": sheet, "address": address, "deleted": true}, fmt.Sprintf("%s!%s 의 노트를 지웠습니다", sheet, address))
	}
}

func xlStr(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
