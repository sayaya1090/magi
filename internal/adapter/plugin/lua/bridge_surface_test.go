package lua

import (
	"strings"
	"testing"

	glua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/port"
)

// The two ambient bridge calls: a plugin asks where it is and what it stands on, and the answers
// are the env's, verbatim.
func TestBridgeAmbientAnswers(t *testing.T) {
	L := glua.NewState()
	defer L.Close()
	p := &plugin{env: port.ToolEnv{Workdir: "/w/here"}}
	if n := p.bridgeWorkdir(L); n != 1 || L.Get(-1).String() != "/w/here" {
		t.Fatalf("workdir: n=%d got=%q", n, L.Get(-1).String())
	}
	L.Pop(1)
	if n := p.bridgePlatform(L); n != 1 || strings.TrimSpace(L.Get(-1).String()) == "" {
		t.Fatalf("platform answers the OS when no host overrides: %q", L.Get(-1).String())
	}
}

// hasHook answers without blocking: a plugin busy in a minutes-long handler is assumed to HAVE
// handlers rather than making the asker wait on its mutex.
func TestHasHookAnswersWithoutBlocking(t *testing.T) {
	p := &plugin{hooks: map[string][]*glua.LFunction{"turn_finished": {nil}}}
	if !p.hasHook("turn_finished") || p.hasHook("user_message") {
		t.Fatal("registered means has, unregistered means has-not")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.hasHook("anything") {
		t.Fatal("a held mutex is a busy handler, and busy is assumed subscribed")
	}
}
