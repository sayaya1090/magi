package main

import (
	"strings"

	"github.com/sayaya1090/magi/internal/webassets"
)

//go:generate go run gen_icons.go

// 스프라이트는 공용 패키지(internal/webassets)에서 관리됩니다. 신구 콘솔이 일관된 벡터 이미지를 사용하도록 보장하며,
// 스프라이트가 미포함된 빌드 환경에서도 빈 문자열로 안전하게 처리됩니다.

// spriteMarker is where the sprite goes: immediately inside <body>, so a <use> anywhere below it
// resolves. Left in the markup when there is no sprite, it would be a comment nobody reads, so it
// is replaced either way.
const spriteMarker = "<!--ICON-SPRITE-->"

// withSprite puts the sprite into the assembled page, or takes the marker out.
func withSprite(page string) string {
	return strings.Replace(page, spriteMarker, webassets.Sprite, 1)
}
