package main

import (
	"fmt"
	"sort"
	"strings"
)

// 글머리 기호의 **이름 목록은 여기 한 자리**에 둔다 — 도구 셋(format_shape·apply_style·add_slides)이
// 같은 두 칸(`bullet_type`·`bullet_style`)을 받고, 셋 다 값이 틀리면 호스트가 배치 전체를
// `InvalidArgument` 한 단어로 되돌린다.
//
// # 왜 서버가 거른다
//
// 앞 판본은 「목록은 늙으니 안 적는다 — 호스트가 거절한다」였다. 그런데 그 설명에 적어 둔
// 예시 `bulletChromaDot` 은 이 열거형에 **없는 이름**이었고, 모델은 예시를 그대로 썼다. 호스트는
// 슬라이드 7장짜리 add_slides 를 통째로 `InvalidArgument` 로 돌려보냈고, 결과에는 어느 칸이
// 틀렸는지가 없었다(2026-09-05 실물, 세션 s_98eb88…). 늙는 목록보다 **이름 없는 거절**이 더
// 비쌌다. 목록은 API set 1.10 기준으로 굳히고 출처를 적어 둔다:
// https://learn.microsoft.com/javascript/api/powerpoint/powerpoint.bulletstyle (2026-04-24 판)
//
// # 이 열거형이 무엇이 아닌가
//
// `BulletStyle` 은 **번호 매김**의 모양이다 — 점·대시·체크 같은 기호 글머리를 고르는 칸이
// 아니다. Office.js 에는 그 문이 없고, 기호 글머리는 `bullet: true` 로 켜면 레이아웃이 정한 기호가
// 나온다. 그 사실을 거절문에 적는다 — 모델이 「점 모양」을 여기서 찾다가 같은 벽을 또 치지 않게.
var bulletTypes = []string{"None", "Numbered", "Unnumbered"}

var bulletStyles = []string{
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

// bulletEnums 는 칸 이름 → 받는 값. 두 칸은 인자 나무의 **어느 깊이에든** 온다 — `format_shape`
// 는 맨 위에, `apply_style` 은 `title`/`body`/`all` 안에, `add_slides` 는 `slides[i]` 안에.
var bulletEnums = map[string][]string{"bullet_type": bulletTypes, "bullet_style": bulletStyles}

// checkBullets 는 인자 나무를 끝까지 걸어 두 칸의 값을 거른다. 이름이 틀리면 **어느 칸·어느 값**인지와
// 대신 쓸 것을 한 문장으로 돌려준다. 문자열이 아닌 값은 여기서 안 본다(타입은 checkType 의 몫).
func checkBullets(toolName string, args map[string]any) error {
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
				if allowed, watched := bulletEnums[k]; watched {
					if s, isStr := x[k].(string); isStr && !contains(allowed, s) {
						return argError{bulletRefusal(toolName, joinPath(path, k), k, s)}
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

func bulletRefusal(toolName, where, key, got string) string {
	if key == "bullet_type" {
		return fmt.Sprintf("%s: %s = %q is not a bullet type — it is one of %s. Nothing was changed — this call did not run.",
			toolName, where, got, strings.Join(quoted(bulletTypes), ", "))
	}
	return fmt.Sprintf("%s: %s = %q is not a PowerPoint bullet style. This enum names NUMBERING styles only (%d of them, e.g. %s); "+
		"there is no door for a dot, dash or check glyph — for a plain bullet send bullet:true and leave bullet_style out. "+
		"Nothing was changed — this call did not run.",
		toolName, where, got, len(bulletStyles), strings.Join(quoted([]string{"ArabicNumeralPeriod", "AlphabetLowercasePeriod", "RomanUppercasePeriod"}), ", "))
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
