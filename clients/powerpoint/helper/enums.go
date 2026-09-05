package main

import (
	"fmt"
	"strings"
)

// Office.js 열거형 — **값 목록은 여기 한 자리**에 둔다(bullets.go 와 같은 이유: 설명의 예시가
// 열거형에 없는 이름이어서 배치 전체가 `InvalidArgument` 한 단어로 죽었다, 2026-09-05).
// 출처는 learn.microsoft.com 의 각 enum 페이지(2026-06-17 판)이고, 필요한 API set 을 옆에 적는다.
// 여기 적는 것은 **쓸 수 있는 값**이다 — `AutoSizeMixed` 처럼 읽기에만 나오는 것은 뺀다.

// PowerPoint.ShapeFontUnderlineStyle (1.4)
var underlines = []string{
	"None", "Single", "Double", "Heavy", "Dotted", "DottedHeavy", "Dash", "DashHeavy", "DashLong",
	"DashLongHeavy", "DotDash", "DotDashHeavy", "DotDotDash", "DotDotDashHeavy", "Wavy", "WavyHeavy", "WavyDouble",
}

// PowerPoint.TextVerticalAlignment (1.4) — 표 셀의 세로 정렬도 같은 열거형이다.
var valigns = []string{"Top", "Middle", "Bottom", "TopCentered", "MiddleCentered", "BottomCentered"}

// PowerPoint.ShapeAutoSize (1.4)
var autosizes = []string{"AutoSizeNone", "AutoSizeShapeToFitText", "AutoSizeTextToFitShape"}

// PowerPoint.ShapeZOrder (1.8)
var zorders = []string{"BringToFront", "BringForward", "SendBackward", "SendToBack"}

// PowerPoint.ShapeLineDashStyle (1.4) — 도형 테두리와 표 셀 테두리가 같은 열거형이다.
var lineDashes = []string{
	"Solid", "Dash", "DashDot", "DashDotDot", "LongDash", "LongDashDot", "LongDashDotDot",
	"RoundDot", "SquareDot", "SystemDash", "SystemDashDot", "SystemDot",
}

// PowerPoint.ConnectorType (1.4)
var connectors = []string{"Straight", "Elbow", "Curve"}

// PowerPoint.TableStyle (1.9) — 74개. 앞의 둘이 「스타일 없음」이다.
var tableStyles = func() []string {
	out := []string{"NoStyleNoGrid", "NoStyleTableGrid"}
	for _, fam := range []string{"ThemedStyle1", "ThemedStyle2"} {
		for i := 1; i <= 6; i++ {
			out = append(out, fmt.Sprintf("%sAccent%d", fam, i))
		}
	}
	for _, fam := range []string{"LightStyle1", "LightStyle2", "LightStyle3", "MediumStyle1", "MediumStyle2", "MediumStyle3", "MediumStyle4", "DarkStyle1"} {
		out = append(out, fam)
		for i := 1; i <= 6; i++ {
			out = append(out, fmt.Sprintf("%sAccent%d", fam, i))
		}
	}
	out = append(out, "DarkStyle2", "DarkStyle2Accent1", "DarkStyle2Accent2", "DarkStyle2Accent3")
	return out
}()

// valueEnums 는 칸 이름 → 받는 값. 인자 나무의 **어느 깊이에든** 온다(`checkEnums` 가 걸어서 거른다).
var valueEnums = map[string][]string{
	"bullet_type":  bulletTypes,
	"bullet_style": bulletStyles,
	"underline":    underlines,
	"valign":       valigns,
	"autosize":     autosizes,
	"z_order":      zorders,
	"line_dash":    lineDashes,
	"connector":    connectors,
	"table_style":  tableStyles,
}

func enumRefusal(toolName, where, key, got string) string {
	switch key {
	case "bullet_type", "bullet_style":
		return bulletRefusal(toolName, where, key, got)
	}
	allowed := valueEnums[key]
	shown := allowed
	if len(shown) > 8 {
		shown = shown[:8]
	}
	more := ""
	if len(allowed) > len(shown) {
		more = fmt.Sprintf(" … %d in all", len(allowed))
	}
	return fmt.Sprintf("%s: %s = %q is not one of %s%s. Nothing was changed — this call did not run.",
		toolName, where, got, strings.Join(quoted(shown), ", "), more)
}
