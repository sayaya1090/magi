package main

import "strings"

//go:generate go run gen_icons.go

// iconSprite is the block of <symbol> definitions the page draws its icons from, or empty.
//
// Empty is a supported state, not a failure. The art is Font Awesome Pro: its licence lets you use
// it in something you deploy and not republish it as files, so it is baked in at build time by
// gen_icons.go and is absent from any build that had no licence to hand — a contributor's, or a CI
// job without the token. Those builds draw the shapes the page has always drawn, which is why the
// page asks for an icon rather than assuming one (see icon() in page.js).
//
// Set from icons_gen.go's init when that file exists.
var iconSprite = ""

// spriteMarker is where the sprite goes: immediately inside <body>, so a <use> anywhere below it
// resolves. Left in the markup when there is no sprite, it would be a comment nobody reads, so it
// is replaced either way.
const spriteMarker = "<!--ICON-SPRITE-->"

// withSprite puts the sprite into the assembled page, or takes the marker out.
func withSprite(page string) string {
	return strings.Replace(page, spriteMarker, iconSprite, 1)
}
