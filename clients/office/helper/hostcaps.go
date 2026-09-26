package office

import "fmt"

// 이 호스트가 못 하는 도구는 모델에게 **안 보인다.**
//
// ⚠ 앞의 판은 도구 목록(`tools/list`)을 고정 표 그대로 냈다. 작업창은 열릴 때 이 호스트가 어느 API 까지 되는지 재서 헬퍼에
// 알려 왔는데(`/api/caps`, serve.go) 그 값은 상태 화면에만 쓰였다 — 그래서 2021 의 Word 에서 모델은 메모·각주·도형 도구를
// 보고, 부르고, 「WordApi 1.4 이 필요한데 이 호스트에는 없습니다」를 받은 뒤에야 알았다. 호출 한 번이 헛되고, 모델은 다른
// 길(스크립트로 파일을 직접 고치기)로 돌아가려 든다. 사용자 지적 2026-09-26: 「불가능한 것은 모델에게 안 보이게 알아서 빠지지?」
//
// 가르는 규칙은 셋이다.
//   - 창이 **잰** 값만 쓴다. 안 쟀으면 아무것도 숨기지 않는다 — 모르는 것을 「못 한다」로 읽지 않는다.
//   - 도구 **전체**가 요구하는 것만 본다. `edit_table{merge}` 처럼 인자 하나에 걸린 요구는 도구를 숨기지 않는다(나머지는 된다).
//   - 헬퍼가 다른 길로 대신할 수 있으면 숨기지 않는다 — Windows 의 Word 는 COM 으로(word_com.go), Excel 의 메모는 노트로(xl_notes.go).
//
// PowerPoint 는 거르지 않는다: 2021 에서 손은 작업창이 아니라 COM 어댑터라, 작업창이 잰 값은 손이 무엇을 하는지와 상관이 없다.
//
// ⚠ 알려진 틈: 데몬은 도구 목록을 **붙을 때**(AttachMCP) 받아 간다. 창이 잰 값이 그보다 늦게 오면 그 붙음 동안은 거르지 않은
// 목록이 남는다 — 다음 붙음(대화 바꾸기·데몬 재기동)에서 맞춰진다. 창은 화면을 그리면서 곧바로 보내므로 대개 먼저 온다.

// apiNeed 는 도구 하나가 요구하는 요구 집합과 판.
type apiNeed struct{ Set, Version string }

// wordToolNeeds 는 WordHand.js 의 #need 가운데 도구 전체에 걸린 것 — word_com_test 가 소스를 읽어 대조한다.
var wordToolNeeds = map[string]apiNeed{
	"add_bookmark": {"WordApi", "1.4"}, "add_comment": {"WordApi", "1.4"}, "delete_bookmark": {"WordApi", "1.4"},
	"drop_suggestion": {"WordApi", "1.4"}, "read_comments": {"WordApi", "1.4"}, "read_suggestions": {"WordApi", "1.4"},
	"reply_comment": {"WordApi", "1.4"}, "resolve_comment": {"WordApi", "1.4"}, "set_track_changes": {"WordApi", "1.4"},
	"suggest":         {"WordApi", "1.4"},
	"delete_footnote": {"WordApi", "1.5"}, "insert_field": {"WordApi", "1.5"}, "insert_footnote": {"WordApi", "1.5"},
	"read_footnotes": {"WordApi", "1.5"}, "set_style_format": {"WordApi", "1.5"},
	"read_tracked_changes": {"WordApi", "1.6"}, "review_changes": {"WordApi", "1.6"},
	"set_page_setup": {"WordApiDesktop", "1.1"},
	"delete_shape":   {"WordApiDesktop", "1.2"}, "format_shape": {"WordApiDesktop", "1.2"},
	"insert_shape": {"WordApiDesktop", "1.2"}, "list_shapes": {"WordApiDesktop", "1.2"},
}

// xlToolNeeds 는 ExcelHand.js 의 #need 가운데 도구 전체에 걸린 것.
var xlToolNeeds = map[string]apiNeed{
	"add_comment": {"ExcelApi", "1.10"}, "read_comments": {"ExcelApi", "1.10"}, "resolve_comment": {"ExcelApi", "1.10"},
	"insert_sheets_from_file": {"ExcelApi", "1.13"}, "refresh_pivot": {"ExcelApi", "1.3"},
	"read_suggestions": {"ExcelApi", "1.4"}, "read_tags": {"ExcelApi", "1.4"}, "set_tag": {"ExcelApi", "1.4"}, "suggest": {"ExcelApi", "1.4"},
	"add_conditional_format": {"ExcelApi", "1.6"}, "read_conditional_formats": {"ExcelApi", "1.6"},
	"copy_sheet": {"ExcelApi", "1.7"}, "freeze_panes": {"ExcelApi", "1.7"}, "protect_workbook": {"ExcelApi", "1.7"},
	"render_range": {"ExcelApi", "1.7"}, "set_cell_style": {"ExcelApi", "1.7"}, "set_hyperlink": {"ExcelApi", "1.7"},
	"set_tab_color": {"ExcelApi", "1.7"}, "set_workbook_properties": {"ExcelApi", "1.7"},
	"add_pivot": {"ExcelApi", "1.8"}, "read_validation": {"ExcelApi", "1.8"}, "set_sheet_view": {"ExcelApi", "1.8"},
	"set_validation": {"ExcelApi", "1.8"},
	"add_image":      {"ExcelApi", "1.9"}, "copy_range": {"ExcelApi", "1.9"}, "fill_range": {"ExcelApi", "1.9"},
	"filter_table": {"ExcelApi", "1.9"}, "find": {"ExcelApi", "1.9"}, "remove_duplicates": {"ExcelApi", "1.9"},
	"replace_all": {"ExcelApi", "1.9"}, "set_page_setup": {"ExcelApi", "1.9"},
}

func (a *App) toolNeeds() map[string]apiNeed {
	switch a.Key {
	case "word":
		return wordToolNeeds
	case "xl":
		return xlToolNeeds
	}
	return nil
}

// coveredHere 는 이 기계에서 헬퍼가 다른 길로 그 도구를 대신할 수 있는가.
func (a *App) coveredHere(name string) bool {
	switch a.Key {
	case "word":
		return a.Fallback != nil && wordComOnThisOS && wordComTools[name]
	case "xl":
		return a.Fallback != nil && xlNotesOnThisOS && xlNoteTools[name]
	}
	return false
}

// hostLacks 는 창이 **잰** 값이 그 요구를 못 채운다고 말하는가. 안 쟀거나 그 집합이 목록에 없으면 거짓 — 모르는 것은 숨길 근거가 아니다.
func hostLacks(caps map[string]any, need apiNeed) bool {
	if caps == nil {
		return false
	}
	if measured, _ := caps["measured"].(bool); !measured {
		return false
	}
	sets, _ := caps["sets"].([]map[string]any)
	if sets == nil {
		if raw, ok := caps["sets"].([]any); ok {
			for _, x := range raw {
				if m, ok := x.(map[string]any); ok {
					sets = append(sets, m)
				}
			}
		}
	}
	for _, s := range sets {
		if fmt.Sprint(s["name"]) == need.Set && fmt.Sprint(s["version"]) == need.Version {
			ok, _ := s["ok"].(bool)
			return !ok
		}
	}
	return false
}

// hiddenHere 는 이 도구를 목록에서 뺄 것인가 — 위 규칙 셋.
func (a *App) hiddenHere(name string, caps map[string]any) bool {
	need, ok := a.toolNeeds()[name]
	if !ok {
		return false
	}
	return hostLacks(caps, need) && !a.coveredHere(name)
}
