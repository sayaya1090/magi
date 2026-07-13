// Package plugins embeds the plugins bundled with the magi binary, so a
// brew/install.sh user (who never sees the repository) can enable them with a
// config switch instead of cloning anything. The binary materializes an enabled
// embedded plugin into <config>/plugins-embedded/<name>/ at startup — always
// overwritten, so the on-disk copy tracks the binary's version and is not a
// user-editing surface (drop a same-named plugin into <config>/plugins/ to
// take over; the user copy wins).
//
// FORKS: to bundle your own plugin, this is the ONLY file to touch —
// add your plugin directory next to engram/, give it a //go:embed var
// (embedding every file it needs, subdirectories included), and register it
// in Embedded below. Users then enable it with [plugins.<name>] enabled = true.
package plugins

import "embed"

// engram is the self-improvement observer plugin (see plugins/engram/README.md).
//
//go:embed all:engram
var engram embed.FS

// EmbeddedPlugin is one bundled plugin: its files (under "<name>/") and
// whether it loads when the config says nothing. An explicit
// [plugins.<name>] enabled = true|false always wins; DefaultOn only decides
// the unset case. MAGI_EMBEDDED_PLUGINS=off disables all of them regardless
// (automation/bench runs that must not change measured behavior).
type EmbeddedPlugin struct {
	FS        embed.FS
	DefaultOn bool
}

// Embedded maps each bundled plugin's name to its definition.
var Embedded = map[string]EmbeddedPlugin{
	"engram": {FS: engram, DefaultOn: true},
}
