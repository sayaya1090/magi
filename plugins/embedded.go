// Package plugins embeds the plugins bundled with the magi binary, so a
// brew/install.sh user (who never sees the repository) can enable them with a
// config switch instead of cloning anything. The binary materializes an enabled
// embedded plugin into <config>/plugins-embedded/<name>/ at startup — always
// overwritten, so the on-disk copy tracks the binary's version and is not a
// user-editing surface (drop a same-named plugin into <config>/plugins/ to
// take over; the user copy wins).
package plugins

import "embed"

// Engram is the self-improvement observer plugin (see plugins/engram/README.md).
// Disabled by default — it spends sidecar LLM tokens and writes knowledge files
// into the workspace — and enabled with:
//
//	[plugins.engram]
//	enabled = true
//
//go:embed engram/plugin.toml engram/init.lua
var Engram embed.FS
