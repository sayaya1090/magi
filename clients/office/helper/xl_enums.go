package office

import (
	"fmt"
)

var xlChartTypes = []string{
	"ColumnClustered", "ColumnStacked", "ColumnStacked100", "BarClustered", "BarStacked", "BarStacked100",
	"Line", "LineMarkers", "Pie", "Doughnut", "Area", "AreaStacked", "XYScatter", "XYScatterLines",
	"Radar", "Waterfall", "Treemap", "Sunburst", "Funnel", "Histogram", "BoxWhisker",
}
var xlSeriesBys = []string{"Columns", "Rows"}

// xlChartAliases 는 사람 말로 부르는 차트 이름 — 손(handCore.CHART_ALIASES)이 Excel.js 이름으로 옮긴다. 스키마에는
// 정본 21개만 광고하고 검사만 이것도 받는다. 실물(2026-09-06)에서 「막대」가 여기서 막혀 차트가 하나도 안 그려졌다.
var xlChartAliases = []string{
	"bar", "column", "막대", "세로막대", "hbar", "가로막대", "line", "꺾은선", "선", "pie", "원", "파이", "doughnut", "도넛",
	"area", "영역", "scatter", "분산", "radar", "방사형", "stacked", "누적", "waterfall", "폭포",
}

// xlEnumExempt 는 이름은 같지만 뜻이 다른 칸 — suggest 의 `what` 은 제안 한 문장이지 clear_range 의 `what` 이 아니다.
// 실물(2026-09-06)에서 suggest 가 전부 「what 은 all/contents/…」로 거절됐다.
var xlEnumExempt = map[string]map[string]bool{
	"suggest": {"what": true},
}
var xlAligns = []string{"General", "Left", "Center", "Right", "Fill", "Justify", "CenterAcrossSelection", "Distributed"}
var xlValigns = []string{"Top", "Center", "Bottom", "Justify", "Distributed"}
var xlBorderStyles = []string{"Continuous", "Dash", "DashDot", "DashDotDot", "Dot", "Double", "SlantDashDot", "None"}
var xlClearWhats = []string{"all", "contents", "formats", "hyperlinks"}
var xlInsertShifts = []string{"down", "right"}
var xlDeleteShifts = []string{"up", "left"}
var xlAutofitWhats = []string{"columns", "rows", "both"}
var xlVisibilities = []string{"Visible", "Hidden", "VeryHidden"}
var xlLegendPositions = []string{"Right", "Left", "Top", "Bottom", "Corner", "none"}
var xlCfKinds = []string{"cell_value", "color_scale", "data_bar", "icon_set", "contains_text", "top_bottom", "custom"}
var xlCfOperators = []string{"Between", "NotBetween", "EqualTo", "NotEqualTo", "GreaterThan", "LessThan", "GreaterThanOrEqualTo", "LessThanOrEqualTo",
	"Contains", "NotContains", "BeginsWith", "EndsWith"}
var xlValidationKinds = []string{"list", "whole_number", "decimal", "date", "time", "text_length", "custom"}
var xlValidationOperators = []string{"Between", "NotBetween", "EqualTo", "NotEqualTo", "GreaterThan", "LessThan", "GreaterThanOrEqualTo", "LessThanOrEqualTo"}

// xlTableStyles 는 Excel.BuiltInTableStyle — Light 1~21, Medium 1~28, Dark 1~11.
var xlTableStyles = func() []string {
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

// xlValueEnums 는 인자 이름 → 허용 값. 이름이 같으면 어느 도구에서든 같은 열거다 — 그래서 뜻이 다른 칸은
// 이름을 다르게 지었다(cf_kind / validation_kind / chart_type, insert 의 shift 와 delete 의 shift 는 값이
// 갈려 합쳐 둔다).
// xlCopyModes·xlFillKinds 는 copy_range·fill_range 의 종류(Excel.RangeCopyType·AutoFillType 을 사람 말로).
var xlCopyModes = []string{"all", "values", "formulas", "formats"}
var xlFillKinds = []string{"default", "copy", "series", "formats", "values"}

var xlValueEnums = map[string][]string{
	"mode":                xlCopyModes,
	"fill":                xlFillKinds,
	"chart_type":          append(append([]string{}, xlChartTypes...), xlChartAliases...),
	"series_by":           xlSeriesBys,
	"align":               xlAligns,
	"valign":              xlValigns,
	"border_style":        xlBorderStyles,
	"what":                append(append([]string{}, xlClearWhats...), xlAutofitWhats...),
	"shift":               append(append([]string{}, xlInsertShifts...), xlDeleteShifts...),
	"visibility":          xlVisibilities,
	"legend":              xlLegendPositions,
	"cf_kind":             xlCfKinds,
	"validation_kind":     xlValidationKinds,
	"table_style":         xlTableStyles,
	"operator":            xlCfOperators, // 유효성의 연산자는 이것의 앞 여덟 개 — 부분집합이라 같이 잰다
	"validation_operator": xlValidationOperators,
}
