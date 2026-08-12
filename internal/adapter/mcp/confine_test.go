package mcp

import (
	"context"
	"strings"
	"testing"
)

// A stdio server is started inside the sandbox when there is one.
//
// It is a program a config file names and the daemon keeps alive for its whole life — the last
// child spawned with nothing around it while the bash tool beside it was wrapped. A trusted
// workspace can supply one, so "what the repo declared" and "what runs unconfined" were the same
// set.
func TestAStdioServerIsStartedThroughTheSandbox(t *testing.T) {
	m := NewManager(nil)
	var saw []string
	m.Confine = func(argv []string) ([]string, bool) {
		saw = argv
		// Wrap it in something that will fail to start, which is all this test needs: the
		// question is what argv was BUILT, not whether a fake server speaks the protocol.
		return append([]string{"/nonexistent/jail"}, argv...), true
	}
	err := m.AddStdio(context.Background(), "theirs", "node", []string{"server.js"}, nil)
	if err == nil {
		t.Fatal("the wrapped command started, which this test cannot have meant")
	}
	if strings.Join(saw, " ") != "node server.js" {
		t.Errorf("the sandbox was offered %v, not the server's own argv", saw)
	}
	if !strings.Contains(err.Error(), "jail") {
		t.Errorf("the server was started outside the wrapper: %v", err)
	}
}

// With no sandbox on this machine, it starts as it always did.
func TestAStdioServerStartsUnwrappedWhenThereIsNothingToWrapWith(t *testing.T) {
	m := NewManager(nil)
	m.Confine = func(argv []string) ([]string, bool) { return nil, false }
	err := m.AddStdio(context.Background(), "theirs", "/nonexistent/server", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/server") {
		t.Errorf("it did not try the server's own command: %v", err)
	}
}
