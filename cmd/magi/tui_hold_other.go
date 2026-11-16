//go:build windows

package main

// holdTUI on Windows cannot prove no other terminal is alive, so a standalone TUI recovers nothing
// and opens a new conversation, as before.
func holdTUI(string) (release func(), sole bool, share func()) {
	noop := func() {}
	return noop, false, noop
}
