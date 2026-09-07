package office

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// 「옆에 열어 둔 저 덱 서식 그대로 따라 줘」 — 그 일을 **도구가 직접 한다.**
//
// # 왜 산문으로는 안 됐나
//
// 안내는 원래 `describe_style` 의 설명 안에 있었다(*"TO CARRY A LOOK FROM ANOTHER DECK, read the
// other one…"*). 그런데 그 글은 **그 도구를 부르기로 이미 정한 모델만** 읽는다 — 열라고 만든 문
// 안쪽에 붙인 안내문이다.
//
// 실물에서 그대로 났다(2026-09-08, LTSC 2021 · COM 손 · 덱 둘). 사람이 「옆 source.pptx 의 서식을
// 그대로 따라서 이 문서에 다른 내용으로 3장」이라고 시켰고, 모델은:
//
//	list_documents          ← 옆 덱을 찾았다
//	list_layouts{source}    ← 옆 덱을 읽기까지 했다
//	add_slides              ← 그런데 서식은 안 읽고 만들었다
//	apply_style{ea_font:"본고딕"}   ← **원본에 없는 글꼴을 지어냈다**(원본은 맑은 고딕)
//
// 카운슬이 「source 의 서식을 따랐다는 증거가 없다」로 **두 번** 되돌려서야 `describe_style{source}` 를
// 읽고 맞췄다. 카운슬을 끈 판이었으면 **틀린 서식 그대로 착지했을 것이다.**
//
// # 그래서 인자 하나를 준다
//
// 손은 이 일을 못 한다 — 손은 덱마다 따로 붙어서, 한 호출이 A 를 읽고 B 에 쓸 수가 없다. 할 수 있는
// 자리는 허브를 쥔 **헬퍼**뿐이다. 그림 바이트를 헬퍼가 읽어 실어 주는 길(`WantsImage`)과 같은 자리에
// 놓았고, 채워 넣는 값은 **두 손이 이미 받는 칸**이라 손은 한 줄도 안 바뀐다.
//
// 규칙 하나: **부른 쪽이 준 값이 이긴다.** 이 인자는 「빈 자리를 저 덱으로 채워라」이지 「내 말을
// 덮어써라」가 아니다.

// matchDocumentArg 는 「저 문서의 서식을 따라라」를 말하는 인자 이름.
const matchDocumentArg = "match_document"

// matchDocumentProp 는 그 인자의 스키마 한 칸.
func matchDocumentProp(noun string) property {
	return property{
		Name: matchDocumentArg,
		Type: "string",
		Desc: "Another " + noun + "'s key from list_documents — take THAT " + noun + "'s title and body typeface, size and " +
			"colour and use them here. This is how \"make this one look like that one\" is done, and it is the ONLY way that " +
			"reads real numbers: a rendered picture cannot tell you a font name, a point size or a hex colour. " +
			"The helper reads that " + noun + "'s describe_style and fills in ONLY the fields you did not set yourself, so " +
			"anything you pass wins. ⚠ It carries the LATIN typeface, size and colour. describe_style cannot read the " +
			"East-Asian face, so ea_font is NEVER guessed from another " + noun + " — on a Korean deck set it yourself, or leave it alone.",
	}
}

// matchThemeDocumentProp 는 같은 인자의 **테마 색** 쪽 설명. 옮기는 것이 달라 글이 따로다.
func matchThemeDocumentProp(noun string) property {
	return property{
		Name: matchDocumentArg,
		Type: "string",
		Desc: "Another " + noun + "'s key from list_documents — read ITS theme colours and paint them here. " +
			"Together with apply_style's match_document this is what \"make this " + noun + " look like that one\" means: " +
			"that one carries the type, this one carries the palette. `colors` you pass yourself win, and you can omit " +
			"`colors` entirely when you use this. The SAME `scope` is read from the other " + noun + " as the one you are " +
			"writing, so pass scope:\"master\" on both sides to restyle a whole deck rather than a single slide.",
	}
}

// styleRoles 는 서식을 옮기는 자리와, 옮기는 칸.
var (
	styleRoles  = []string{"title", "body"}
	styleFields = []string{"font", "size", "color"}
	// themeColorNames 는 테마 색 열둘. **정해진 순서로 걷는다** — 맵을 그냥 돌면 답의 줄이 호출마다
	// 달라지고, 그 줄은 카운슬이 읽는 증거다.
	themeColorNames = []string{
		"dark1", "dark2", "light1", "light2",
		"accent1", "accent2", "accent3", "accent4", "accent5", "accent6",
		"hyperlink", "followedHyperlink",
	}
)

// carryFrom 은 이 도구가 옆 문서에서 **무엇을** 가져와야 하는지 고른다. 도구마다 읽는 것이 다르다 —
// 글꼴·크기·색은 `describe_style`, 테마 색 열둘은 `read_theme_colors` 다. 둘은 서로를 안 대신한다:
// 테마 색만 바꾼 덱은 **여전히 기본 테마의 서체**이고(그게 「아직 파워포인트 템플릿 같다」의 뜻이다),
// 서체만 맞춘 덱은 강조색이 남의 것이다.
func carryFrom(ctx context.Context, hand Hand, toolName, from string, args map[string]any) ([]string, error) {
	if toolName == "set_theme_colors" {
		return carryThemeColors(ctx, hand, from, args)
	}
	return carryStyle(ctx, hand, from, args)
}

// carryThemeColors 는 `from` 문서의 테마 색을 읽어 이 호출의 `colors` 에 채운다.
//
// **같은 층을 읽는다.** 이 호출이 `scope:"master"` 면 저쪽도 master 를 읽는다 — 층이 어긋나면 딴 값을
// 가져다 놓고 「따랐습니다」라고 적게 된다.
func carryThemeColors(ctx context.Context, hand Hand, from string, args map[string]any) ([]string, error) {
	if hand == nil {
		return nil, errors.New("this helper has no hub, so it cannot read another document's theme")
	}
	ask := map[string]any{}
	if scope, ok := args["scope"].(string); ok && strings.TrimSpace(scope) != "" {
		ask["scope"] = scope
	}
	res, err := hand.Call(ctx, from, "read_theme_colors", ask)
	if err != nil {
		return nil, fmt.Errorf("could not read the theme colours of %s: %w — list_documents shows which documents are open and what their keys are", from, err)
	}
	label := res.Label
	if label == "" {
		label = from
	}
	theme, _ := res.Result["theme"].(map[string]any)
	if len(theme) == 0 {
		return []string{fmt.Sprintf("「%s」의 테마 색을 못 읽었습니다 — 색을 직접 주세요.", label)}, nil
	}

	mine, _ := args["colors"].(map[string]any)
	if mine == nil {
		mine = map[string]any{}
	}
	var took []string
	for _, n := range themeColorNames {
		v, has := theme[n]
		if !has || v == nil {
			continue
		}
		if _, already := mine[n]; already {
			continue // 부른 쪽이 준 값이 이긴다
		}
		mine[n] = v
		took = append(took, fmt.Sprintf("%s %v", n, v))
	}
	if len(took) == 0 {
		return []string{fmt.Sprintf("「%s」에서 새로 가져올 테마 색이 없었습니다 — 준 값이 이미 열둘을 덮었습니다.", label)}, nil
	}
	args["colors"] = mine
	return []string{fmt.Sprintf("「%s」의 테마 색 %d개를 따랐습니다 — %s", label, len(took), strings.Join(took, " · "))}, nil
}

// carryStyle 은 `from` 문서의 서식을 읽어 이 호출의 `title`·`body` 에 채운다.
//
// 답으로 **사람이 읽는 줄**을 돌려준다 — 결과의 `changed` 에 실려, 카운슬이 「저 덱을 따랐다」의 증거로
// 읽을 수 있는 유일한 칸이 그것이다(실측에서 카운슬이 거절한 사유가 정확히 「증거 없음」이었다).
func carryStyle(ctx context.Context, hand Hand, from string, args map[string]any) ([]string, error) {
	if hand == nil {
		return nil, errors.New("this helper has no hub, so it cannot read another document's style")
	}
	res, err := hand.Call(ctx, from, "describe_style", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("could not read the style of %s: %w — list_documents shows which documents are open and what their keys are", from, err)
	}
	label := res.Label
	if label == "" {
		label = from
	}

	took := map[string][]string{}
	for _, role := range styleRoles {
		src, ok := res.Result[role].(map[string]any)
		if !ok {
			continue
		}
		mine, _ := args[role].(map[string]any)
		if mine == nil {
			mine = map[string]any{}
		}
		for _, f := range styleFields {
			v, has := src[f]
			if !has || v == nil {
				continue
			}
			if _, already := mine[f]; already {
				continue // 부른 쪽이 준 값이 이긴다
			}
			mine[f] = v
			took[role] = append(took[role], fmt.Sprintf("%s %v", f, v))
		}
		if len(mine) > 0 {
			args[role] = mine
		}
	}

	if len(took) == 0 {
		// **없는 것을 지어내지 않는다.** 덱에 따라갈 버릇이 없으면 describe_style 자신이 그렇게 답한다.
		return []string{fmt.Sprintf("「%s」에서 따라갈 서식을 못 찾았습니다 — 그 문서에 일관된 버릇이 없거나 읽히지 않았습니다. 값을 직접 주세요.", label)}, nil
	}

	var parts []string
	for _, role := range styleRoles {
		if got := took[role]; len(got) > 0 {
			parts = append(parts, role+" "+strings.Join(got, " "))
		}
	}
	notes := []string{fmt.Sprintf("「%s」의 서식을 따랐습니다 — %s", label, strings.Join(parts, " · "))}
	if _, set := args["ea_font"]; !set {
		// 실측에서 모델이 지어낸 칸이 정확히 여기다. **안 채운다는 것을 소리 내어 적는다** —
		// 조용히 비워 두면 다음 모델이 또 지어낸다.
		notes = append(notes, "한글 글꼴(ea_font)은 안 옮겼습니다 — describe_style 이 못 읽는 칸이라 지어내지 않습니다.")
	}
	return notes, nil
}

// styleSourceOf 는 이 호출이 다른 문서의 서식을 따라야 하는가 — 따라야 하면 그 문서 키를 답한다.
func styleSourceOf(name string, args map[string]any, tools ...string) string {
	found := false
	for _, t := range tools {
		if t == name {
			found = true
			break
		}
	}
	if !found {
		return ""
	}
	s, _ := args[matchDocumentArg].(string)
	return strings.TrimSpace(s)
}
