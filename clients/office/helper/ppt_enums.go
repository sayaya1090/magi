package office

import (
	"fmt"
)

var pptUnderlines = []string{
	"None", "Single", "Double", "Heavy", "Dotted", "DottedHeavy", "Dash", "DashHeavy", "DashLong",
	"DashLongHeavy", "DotDash", "DotDashHeavy", "DotDotDash", "DotDotDashHeavy", "Wavy", "WavyHeavy", "WavyDouble",
}

// PowerPoint.TextVerticalAlignment (1.4) — 표 셀의 세로 정렬도 같은 열거형이다.
var pptValigns = []string{"Top", "Middle", "Bottom", "TopCentered", "MiddleCentered", "BottomCentered"}

// PowerPoint.ShapeAutoSize (1.4)
var pptAutosizes = []string{"AutoSizeNone", "AutoSizeShapeToFitText", "AutoSizeTextToFitShape"}

// PowerPoint.ShapeZOrder (1.8)
var pptZorders = []string{"BringToFront", "BringForward", "SendBackward", "SendToBack"}

// PowerPoint.ShapeLineDashStyle (1.4) — 도형 테두리와 표 셀 테두리가 같은 열거형이다.
var pptLineDashes = []string{
	"Solid", "Dash", "DashDot", "DashDotDot", "LongDash", "LongDashDot", "LongDashDotDot",
	"RoundDot", "SquareDot", "SystemDash", "SystemDashDot", "SystemDot",
}

// PowerPoint.ConnectorType (1.4)
var pptConnectors = []string{"Straight", "Elbow", "Curve"}

// PowerPoint.TableStyle (1.9) — 74개. 앞의 둘이 「스타일 없음」이다.
var pptTableStyles = func() []string {
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

// pptValueEnums 는 칸 이름 → 받는 값. 인자 나무의 **어느 깊이에든** 온다(`checkEnums` 가 걸어서 거른다).
var pptValueEnums = map[string][]string{
	"bullet_type":  pptBulletTypes,
	"bullet_style": pptBulletStyles,
	"underline":    pptUnderlines,
	"valign":       pptValigns,
	"autosize":     pptAutosizes,
	"z_order":      pptZorders,
	"line_dash":    pptLineDashes,
	"connector":    pptConnectors,
	"table_style":  pptTableStyles,
}
