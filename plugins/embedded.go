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

// The three CLI backend plugins (EXTENDING §3.7.1): a coding-agent CLI as the model backend,
// through a loopback shim. Bundled OFF, and the reason is what they do when on — take over the
// LLM base URL. A user who merely upgraded magi while having claude installed must not find
// their companion silently rebased onto a different backend; flipping
// [plugins.claudecode] enabled = true is the sentence that says "yes, that one".
//
//go:embed all:antigravity
var antigravity embed.FS

//go:embed all:claudecode
var claudecode embed.FS

//go:embed all:codex
var codex embed.FS

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
	"engram":      {FS: engram, DefaultOn: true},
	"antigravity": {FS: antigravity, DefaultOn: false},
	"claudecode":  {FS: claudecode, DefaultOn: false},
	"codex":       {FS: codex, DefaultOn: false},
}
