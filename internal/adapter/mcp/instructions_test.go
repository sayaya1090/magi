package mcp

import "testing"

func TestInstructionsOwnerOverridesGlobalAndDisappearsOnClose(t *testing.T) {
	global := &Client{done: make(chan struct{}), instructions: "global"}
	local := &Client{done: make(chan struct{}), instructions: "local"}
	other := &Client{done: make(chan struct{}), instructions: "other"}
	m := NewManager(&namesSink{})
	m.servers = map[string]*serverConn{
		"slack":                 {name: "slack", client: global, tools: []string{"mcp__slack__read"}},
		serverKey("slack", "a"): {name: "slack", owner: "a", client: local, tools: []string{"mcp__slack__read"}},
		serverKey("slack", "b"): {name: "slack", owner: "b", client: other, tools: []string{"mcp__slack__read"}},
	}
	visible := map[string]bool{"mcp__slack__read": true}
	if got := m.Instructions("a", visible); len(got) != 1 || got["slack"] != "local" {
		t.Fatal(got)
	}
	if got := m.Instructions("c", visible); got["slack"] != "global" {
		t.Fatal(got)
	}
	if got := m.Instructions("a", map[string]bool{}); len(got) != 0 {
		t.Fatal(got)
	}
	close(local.done)
	if got := m.Instructions("a", visible); len(got) != 0 {
		t.Fatal("closed client guidance retained", got)
	}
}
