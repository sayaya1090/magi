package office

import (
	"fmt"
)

var wordAligns = []string{"Left", "Centered", "Right", "Justified"}
var wordUnderlines = []string{"None", "Single", "Double", "Dotted", "Dashed", "Wave", "Thick"}
var wordHighlights = []string{"Yellow", "BrightGreen", "Turquoise", "Pink", "Blue", "Red", "DarkBlue", "Teal", "Green", "Violet",
	"DarkRed", "DarkYellow", "Gray50", "Gray25", "Black", "none"}
var wordAtWhere = []string{"start", "end"}
var wordListKinds = []string{"bulleted", "numbered"}
var wordBreakKinds = []string{"page", "section", "line"}
var wordHeaderFooter = []string{"header", "footer"}
var wordHeaderKinds = []string{"Primary", "FirstPage", "EvenPages"}
var wordTrackModes = []string{"Off", "TrackAll", "TrackMineOnly"}
var wordReviewWhats = []string{"accept", "reject"}

// wordBuiltinStyles 는 Word.BuiltInStyleName 중 문단 스타일 — 언어와 무관한 이름이다(한국어 Word 의 「제목 1」도 Heading1).
var wordBuiltinStyles = []string{"Normal", "Title", "Subtitle", "Heading1", "Heading2", "Heading3", "Heading4", "Heading5", "Heading6",
	"Heading7", "Heading8", "Heading9", "Quote", "IntenseQuote", "ListParagraph", "Caption", "NoSpacing", "TocHeading",
	"Toc1", "Toc2", "Toc3", "Emphasis", "Strong", "SubtleEmphasis", "IntenseEmphasis", "SubtleReference", "IntenseReference", "BookTitle"}

// wordTableStyles 는 Word.BuiltInStyleName 의 표 스타일 — Grid/List Table 1~7 × 기본+Accent1~6, 그리고 Plain·Grid.
var wordTableStyles = func() []string {
	out := []string{"TableGrid", "TableGridLight", "PlainTable1", "PlainTable2", "PlainTable3", "PlainTable4", "PlainTable5"}
	for _, fam := range []string{"GridTable", "ListTable"} {
		for i, tail := range []string{"1Light", "2", "3", "4", "5Dark", "6Colorful", "7Colorful"} {
			_ = i
			base := fam + tail
			out = append(out, base)
			for a := 1; a <= 6; a++ {
				out = append(out, fmt.Sprintf("%s_Accent%d", base, a))
			}
		}
	}
	return out
}()

// wordValueEnums 는 인자 이름 → 허용 값. 이름이 같으면 어느 도구에서든 같은 열거다 — 그래서 뜻이 다른 칸은
// 이름을 다르게 지었다(kind 는 insert_list·set_list 의 목록 종류와 insert_break·set_header_footer 의 종류가
// 갈려 합쳐 둔다; suggest 의 what 은 문장이라 예외).
var wordValueEnums = map[string][]string{
	"align":       wordAligns,
	"underline":   wordUnderlines,
	"highlight":   wordHighlights,
	"at":          wordAtWhere,
	"kind":        append(append(append([]string{}, wordListKinds...), wordBreakKinds...), wordHeaderKinds...),
	"which":       wordHeaderFooter,
	"mode":        wordTrackModes,
	"what":        wordReviewWhats,
	"builtin":     wordBuiltinStyles,
	"table_style": wordTableStyles,
}

// wordEnumExempt 는 이름은 같지만 뜻이 다른 칸 — suggest 의 `what` 은 제안 한 문장이지 review_changes 의 what 이 아니다
// (엑셀 판에서 실물로 겪은 자리).
var wordEnumExempt = map[string]map[string]bool{
	"suggest": {"what": true},
}
