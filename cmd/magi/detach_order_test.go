package main

import (
	"os"
	"strings"
	"testing"
)

// The hand-off must come before anything that binds.
//
// `--detach` means: start the successor, say whether it came up, exit. The process doing that must
// open nothing, because whatever it opens the successor cannot have. That was written down as a
// comment and the comment was half true — it was true of the daemon socket and false of the
// plugins, which load about ninety lines earlier and which serve: the three CLI backends each run
// a loopback shim on a port they may pin.
//
// What that cost, measured 2026-09-02: with the claudecode shim pinned to 58411, `--detach` left
// the daemon listening on an automatic 58116 while the address recorded for the console's provider
// picker said 58411 — a port whose only holder had just exited. The plugin's own guard against a
// stale record could not catch it: it asks whether the recorded address answers, and the
// predecessor was still draining, so it did.
//
// A comment cannot hold an ordering. This can: the two lines are in one file, and their order is
// the whole rule.
func TestTheDetachHandOffComesBeforeAnythingBinds(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("main.go unreadable, so this guard is measuring nothing: %v", err)
	}
	text := string(src)
	hand := strings.Index(text, "if *detachMode {")
	binds := strings.Index(text, "host := startPluginHost(")
	if hand < 0 || binds < 0 {
		t.Fatalf("the guard cannot find what it names (hand-off %d, plugin host %d) — one of them "+
			"was renamed and this test went quiet rather than red", hand, binds)
	}
	if hand > binds {
		t.Errorf("the --detach hand-off (offset %d) runs AFTER the plugin host starts (offset %d): "+
			"this process loads plugins that bind ports, then hands the workspace to a successor "+
			"that cannot have them", hand, binds)
	}
}
