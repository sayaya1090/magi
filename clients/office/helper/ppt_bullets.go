package office

import (
	"fmt"
	"strings"
)

var pptBulletTypes = []string{"None", "Numbered", "Unnumbered"}

var pptBulletStyles = []string{
	"AlphabetLowercaseParenthesesBoth", "AlphabetLowercaseParenthesisRight", "AlphabetLowercasePeriod",
	"AlphabetUppercaseParenthesesBoth", "AlphabetUppercaseParenthesisRight", "AlphabetUppercasePeriod",
	"ArabicAbjadDash", "ArabicAlphabetDash", "ArabicDoubleBytePeriod", "ArabicDoubleBytePlain",
	"ArabicNumeralParenthesesBoth", "ArabicNumeralParenthesisRight", "ArabicNumeralPeriod", "ArabicNumeralPlain",
	"CircleNumberDoubleBytePlain", "CircleNumberWideDoubleByteBlackPlain", "CircleNumberWideDoubleByteWhitePlain",
	"HebrewAlphabetDash", "HindiAlphabet1Period", "HindiAlphabetPeriod", "HindiNumeralParenthesisRight", "HindiNumeralPeriod",
	"KanjiKoreanPeriod", "KanjiKoreanPlain", "KanjiSimplifiedChineseDoubleBytePeriod",
	"RomanLowercaseParenthesesBoth", "RomanLowercaseParenthesisRight", "RomanLowercasePeriod",
	"RomanUppercaseParenthesesBoth", "RomanUppercaseParenthesisRight", "RomanUppercasePeriod",
	"SimplifiedChinesePeriod", "SimplifiedChinesePlain",
	"ThaiAlphabetParenthesesBoth", "ThaiAlphabetParenthesisRight", "ThaiAlphabetPeriod",
	"ThaiNumeralParenthesesBoth", "ThaiNumeralParenthesisRight", "ThaiNumeralPeriod",
	"TraditionalChinesePeriod", "TraditionalChinesePlain",
}

// checkEnums 는 인자 나무를 끝까지 걸어 열거형 칸(enums.go `pptValueEnums`)의 값을 거른다 — 두 글머리
// 칸은 `format_shape` 맨 위에, `apply_style` 의 `title`/`body`/`all` 안에, `add_slides` 의 `slides[i]`
// 안에 온다. 이름이 틀리면 **어느 칸·어느 값**인지와 대신 쓸 것을 한 문장으로 돌려준다. 문자열이
// 아닌 값은 여기서 안 본다(타입은 checkType 의 몫).
func pptBulletRefusal(toolName, where, key, got string) string {
	if key == "bullet_type" {
		return fmt.Sprintf("%s: %s = %q is not a bullet type — it is one of %s. Nothing was changed — this call did not run.",
			toolName, where, got, strings.Join(quoted(pptBulletTypes), ", "))
	}
	return fmt.Sprintf("%s: %s = %q is not a PowerPoint bullet style. This enum names NUMBERING styles only (%d of them, e.g. %s); "+
		"there is no door for a dot, dash or check glyph — for a plain bullet send bullet:true and leave bullet_style out. "+
		"Nothing was changed — this call did not run.",
		toolName, where, got, len(pptBulletStyles), strings.Join(quoted([]string{"ArabicNumeralPeriod", "AlphabetLowercasePeriod", "RomanUppercasePeriod"}), ", "))
}
