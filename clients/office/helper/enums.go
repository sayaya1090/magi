package office

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

func enumRefusal(app *App, toolName, where, key, got string) string {
	allowed := app.ValueEnums[key]
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
func checkEnums(app *App, toolName string, args map[string]any) error {
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
				if allowed, watched := app.ValueEnums[k]; watched && !app.EnumExempt[toolName][k] {
					if s, isStr := x[k].(string); isStr && !contains(allowed, s) {
						return argError{app.refusal(toolName, joinPath(path, k), k, s)}
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
