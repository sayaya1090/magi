package main

import (
	"fmt"
	"sort"
	"strings"
)

// Word.js 의 열거형 — 값을 인자 이름으로 잰다(파워포인트·엑셀 판 enums.go 와 같은 기전).
//
// **예시 값은 계약이다.** 설명문에 적은 값을 모델이 그대로 쓰므로, 여기 목록과 설명문의 예시가 어긋나면
// 「InvalidArgument」 한 단어로 죽는다. Word.js 이름 그대로인 것(Alignment, UnderlineType, BuiltInStyleName,
// ChangeTrackingMode, HeaderFooterType)과 우리가 지어낸 소문자 어휘(kind·at·which·what)는 손이 Word.js 로 옮긴다.

var aligns = []string{"Left", "Centered", "Right", "Justified"}
var underlines = []string{"None", "Single", "Double", "Dotted", "Dashed", "Wave", "Thick"}
var highlights = []string{"Yellow", "BrightGreen", "Turquoise", "Pink", "Blue", "Red", "DarkBlue", "Teal", "Green", "Violet",
	"DarkRed", "DarkYellow", "Gray50", "Gray25", "Black", "none"}
var atWhere = []string{"start", "end"}
var listKinds = []string{"bulleted", "numbered"}
var breakKinds = []string{"page", "section", "line"}
var headerFooter = []string{"header", "footer"}
var headerKinds = []string{"Primary", "FirstPage", "EvenPages"}
var trackModes = []string{"Off", "TrackAll", "TrackMineOnly"}
var reviewWhats = []string{"accept", "reject"}

// builtinStyles 는 Word.BuiltInStyleName 중 문단 스타일 — 언어와 무관한 이름이다(한국어 Word 의 「제목 1」도 Heading1).
var builtinStyles = []string{"Normal", "Title", "Subtitle", "Heading1", "Heading2", "Heading3", "Heading4", "Heading5", "Heading6",
	"Heading7", "Heading8", "Heading9", "Quote", "IntenseQuote", "ListParagraph", "Caption", "NoSpacing", "TocHeading",
	"Toc1", "Toc2", "Toc3", "Emphasis", "Strong", "SubtleEmphasis", "IntenseEmphasis", "SubtleReference", "IntenseReference", "BookTitle"}

// tableStyles 는 Word.BuiltInStyleName 의 표 스타일 — Grid/List Table 1~7 × 기본+Accent1~6, 그리고 Plain·Grid.
var tableStyles = func() []string {
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

// valueEnums 는 인자 이름 → 허용 값. 이름이 같으면 어느 도구에서든 같은 열거다 — 그래서 뜻이 다른 칸은
// 이름을 다르게 지었다(kind 는 insert_list·set_list 의 목록 종류와 insert_break·set_header_footer 의 종류가
// 갈려 합쳐 둔다; suggest 의 what 은 문장이라 예외).
var valueEnums = map[string][]string{
	"align":       aligns,
	"underline":   underlines,
	"highlight":   highlights,
	"at":          atWhere,
	"kind":        append(append(append([]string{}, listKinds...), breakKinds...), headerKinds...),
	"which":       headerFooter,
	"mode":        trackModes,
	"what":        reviewWhats,
	"builtin":     builtinStyles,
	"table_style": tableStyles,
}

// enumExempt 는 이름은 같지만 뜻이 다른 칸 — suggest 의 `what` 은 제안 한 문장이지 review_changes 의 what 이 아니다
// (엑셀 판에서 실물로 겪은 자리).
var enumExempt = map[string]map[string]bool{
	"suggest": {"what": true},
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
