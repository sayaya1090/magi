package main

import (
	"fmt"
	"sort"
	"strings"
)

// Excel.js 의 열거형 — 값을 인자 이름으로 잰다(파워포인트 판 enums.go 와 같은 기전).
//
// **예시 값은 계약이다.** 설명문에 적은 값을 모델이 그대로 쓰므로, 여기 목록과 설명문의 예시가 어긋나면
// 「InvalidArgument」 한 단어로 죽는다. 목록은 Excel.js 의 이름 그대로고(ChartType, HorizontalAlignment,
// BorderLineStyle, SheetVisibility, ChartLegendPosition, BuiltInTableStyle), 우리가 지어낸 소문자 어휘
// (cf_kind, validation_kind, shift, what)는 손이 Excel.js 로 옮긴다.

var chartTypes = []string{
	"ColumnClustered", "ColumnStacked", "ColumnStacked100", "BarClustered", "BarStacked", "BarStacked100",
	"Line", "LineMarkers", "Pie", "Doughnut", "Area", "AreaStacked", "XYScatter", "XYScatterLines",
	"Radar", "Waterfall", "Treemap", "Sunburst", "Funnel", "Histogram", "BoxWhisker",
}
var seriesBys = []string{"Columns", "Rows"}

// chartAliases 는 사람 말로 부르는 차트 이름 — 손(handCore.CHART_ALIASES)이 Excel.js 이름으로 옮긴다. 스키마에는
// 정본 21개만 광고하고 검사만 이것도 받는다. 실물(2026-09-06)에서 「막대」가 여기서 막혀 차트가 하나도 안 그려졌다.
var chartAliases = []string{
	"bar", "column", "막대", "세로막대", "hbar", "가로막대", "line", "꺾은선", "선", "pie", "원", "파이", "doughnut", "도넛",
	"area", "영역", "scatter", "분산", "radar", "방사형", "stacked", "누적", "waterfall", "폭포",
}

// enumExempt 는 이름은 같지만 뜻이 다른 칸 — suggest 의 `what` 은 제안 한 문장이지 clear_range 의 `what` 이 아니다.
// 실물(2026-09-06)에서 suggest 가 전부 「what 은 all/contents/…」로 거절됐다.
var enumExempt = map[string]map[string]bool{
	"suggest": {"what": true},
}
var aligns = []string{"General", "Left", "Center", "Right", "Fill", "Justify", "CenterAcrossSelection", "Distributed"}
var valigns = []string{"Top", "Center", "Bottom", "Justify", "Distributed"}
var borderStyles = []string{"Continuous", "Dash", "DashDot", "DashDotDot", "Dot", "Double", "SlantDashDot", "None"}
var clearWhats = []string{"all", "contents", "formats", "hyperlinks"}
var insertShifts = []string{"down", "right"}
var deleteShifts = []string{"up", "left"}
var autofitWhats = []string{"columns", "rows", "both"}
var visibilities = []string{"Visible", "Hidden", "VeryHidden"}
var legendPositions = []string{"Right", "Left", "Top", "Bottom", "Corner", "none"}
var cfKinds = []string{"cell_value", "color_scale", "data_bar", "icon_set", "contains_text", "top_bottom", "custom"}
var cfOperators = []string{"Between", "NotBetween", "EqualTo", "NotEqualTo", "GreaterThan", "LessThan", "GreaterThanOrEqualTo", "LessThanOrEqualTo",
	"Contains", "NotContains", "BeginsWith", "EndsWith"}
var validationKinds = []string{"list", "whole_number", "decimal", "date", "time", "text_length", "custom"}
var validationOperators = []string{"Between", "NotBetween", "EqualTo", "NotEqualTo", "GreaterThan", "LessThan", "GreaterThanOrEqualTo", "LessThanOrEqualTo"}

// tableStyles 는 Excel.BuiltInTableStyle — Light 1~21, Medium 1~28, Dark 1~11.
var tableStyles = func() []string {
	var out []string
	for i := 1; i <= 21; i++ {
		out = append(out, fmt.Sprintf("TableStyleLight%d", i))
	}
	for i := 1; i <= 28; i++ {
		out = append(out, fmt.Sprintf("TableStyleMedium%d", i))
	}
	for i := 1; i <= 11; i++ {
		out = append(out, fmt.Sprintf("TableStyleDark%d", i))
	}
	return out
}()

// valueEnums 는 인자 이름 → 허용 값. 이름이 같으면 어느 도구에서든 같은 열거다 — 그래서 뜻이 다른 칸은
// 이름을 다르게 지었다(cf_kind / validation_kind / chart_type, insert 의 shift 와 delete 의 shift 는 값이
// 갈려 합쳐 둔다).
var valueEnums = map[string][]string{
	"chart_type":          append(append([]string{}, chartTypes...), chartAliases...),
	"series_by":           seriesBys,
	"align":               aligns,
	"valign":              valigns,
	"border_style":        borderStyles,
	"what":                append(append([]string{}, clearWhats...), autofitWhats...),
	"shift":               append(append([]string{}, insertShifts...), deleteShifts...),
	"visibility":          visibilities,
	"legend":              legendPositions,
	"cf_kind":             cfKinds,
	"validation_kind":     validationKinds,
	"table_style":         tableStyles,
	"operator":            cfOperators, // 유효성의 연산자는 이것의 앞 여덟 개 — 부분집합이라 같이 잰다
	"validation_operator": validationOperators,
}

func enumRefusal(toolName, where, key, got string) string {
	allowed := valueEnums[key]
	shown := allowed
	if len(shown) > 8 {
		shown = shown[:8]
	}
	more := ""
	if len(allowed) > len(shown) {
		more = fmt.Sprintf(" … %d in all", len(allowed))
	}
	return fmt.Sprintf("%s: %s = %q is not one of the values %s accepts (%s%s). Nothing was changed — this call did not run.",
		toolName, where, got, key, strings.Join(quoted(shown), ", "), more)
}

// checkEnums 는 인자 트리를 깊이 관계없이 훑어 열거 칸의 값을 잰다.
func checkEnums(toolName string, args map[string]any) error {
	var walk func(path string, v any) error
	walk = func(path string, v any) error {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if allowed, watched := valueEnums[k]; watched && !enumExempt[toolName][k] {
					if s, isStr := x[k].(string); isStr && !contains(allowed, s) {
						return argError{enumRefusal(toolName, joinPath(path, k), k, s)}
					}
					continue
				}
				if err := walk(joinPath(path, k), x[k]); err != nil {
					return err
				}
			}
		case []any:
			for i, e := range x {
				if err := walk(fmt.Sprintf("%s[%d]", path, i), e); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk("", args)
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
