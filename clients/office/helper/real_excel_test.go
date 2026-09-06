package office

import (
	"encoding/json"
	"strings"
	"testing"
)

// 실물 Excel 에 도구 61개를 처음 대 본 날(2026-09-06) 헬퍼 층에서 막힌 셋 — 손까지 가 보지도 못했다.
func TestWhatTheFirstRealWorkbookTaught(t *testing.T) {
	byName := map[string]tool{}
	for _, one := range XL.Catalogue(false) {
		byName[one.Name] = one
	}
	// 하나 — 한국어 차트 이름은 손이 옮기는데 헬퍼가 먼저 막았다.
	if _, err := validateArgs(XL, byName["add_chart"], json.RawMessage(`{"source":"A1:B6","chart_type":"막대"}`)); err != nil {
		t.Errorf("「막대」가 헬퍼에서 막혔다: %v", err)
	}
	if _, err := validateArgs(XL, byName["add_chart"], json.RawMessage(`{"source":"A1:B6","chart_type":"bubble3d"}`)); err == nil {
		t.Error("모르는 차트 종류가 통과했다")
	}
	// 둘 — suggest 의 what 은 문장이지 clear_range 의 what 이 아니다.
	if _, err := validateArgs(XL, byName["suggest"], json.RawMessage(`{"what":"천 단위 구분","fix":{"tool":"set_number_format","args":{"format":"#,##0"}}}`)); err != nil {
		t.Errorf("suggest 의 what 이 열거형에 걸렸다: %v", err)
	}
	if _, err := validateArgs(XL, byName["clear_range"], json.RawMessage(`{"address":"A1","what":"천 단위"}`)); err == nil {
		t.Error("clear_range 의 what 은 여전히 열거형이어야 한다")
	}
	// 셋 — rows 가 목록인 도구에서 「1부터」 검사가 목록을 거절했다.
	if _, err := validateArgs(XL, byName["add_table_rows"], json.RawMessage(`{"table":"t","rows":[["a",1]]}`)); err != nil {
		t.Errorf("행 배열이 「1부터」에 걸렸다: %v", err)
	}
	if _, err := validateArgs(XL, byName["add_pivot"], json.RawMessage(`{"source":"A1:C6","destination":"H2","rows":["분기"],"values":[{"field":"매출"}]}`)); err != nil {
		t.Errorf("피벗의 행 필드 목록이 「1부터」에 걸렸다: %v", err)
	}
	if _, err := validateArgs(XL, byName["freeze_panes"], json.RawMessage(`{"rows":0}`)); err == nil || !strings.Contains(err.Error(), "starts at 1") {
		t.Errorf("수로 온 rows 0 은 여전히 거절해야 한다: %v", err)
	}
	// 넷 — fill 은 fill_range 에서만 채우기 방식이다. 색으로 받는 두 도구가 그 열거에 걸려 죽었다(2021 실물 2026-09-07).
	for _, c := range []struct{ tool, args string }{
		{"format_range", `{"address":"A1:D1","bold":true,"fill":"#1E3A8A"}`},
		{"add_conditional_format", `{"address":"B2:B6","cf_kind":"cell_value","operator":"GreaterThan","value":"5","fill":"#FEF3C7"}`},
	} {
		if _, err := validateArgs(XL, byName[c.tool], json.RawMessage(c.args)); err != nil {
			t.Errorf("%s 의 색 fill 이 fill_range 의 열거에 걸렸다: %v", c.tool, err)
		}
	}
	if _, err := validateArgs(XL, byName["fill_range"], json.RawMessage(`{"address":"D2","to":"D2:D5","fill":"#FEF3C7"}`)); err == nil {
		t.Error("fill_range 의 fill 은 여전히 열거형이어야 한다")
	}
}
