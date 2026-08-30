package main

import (
	"strings"

	"github.com/sayaya1090/magi/internal/webassets"
)

//go:generate go run gen_icons.go

// 스프라이트는 공용 자리(internal/webassets)에 있다 — 두 콘솔이 같은 그림을 그려야 하고,
// 옛 콘솔이 사라져도 새 콘솔이 그것을 잃지 않아야 한다. 빈 값도 정상이다(위 주석 참고).

// spriteMarker is where the sprite goes: immediately inside <body>, so a <use> anywhere below it
// resolves. Left in the markup when there is no sprite, it would be a comment nobody reads, so it
// is replaced either way.
const spriteMarker = "<!--ICON-SPRITE-->"

// withSprite puts the sprite into the assembled page, or takes the marker out.
func withSprite(page string) string {
	return strings.Replace(page, spriteMarker, webassets.Sprite, 1)
}
