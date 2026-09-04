package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeToolServers records what it was asked for and answers what it was told to.
var _ port.ToolServers = (*fakeToolServers)(nil)

type fakeToolServers struct {
	attached []string // name|url|header count, one per Attach
	names    []string
	attachEr error
	detached []string
	had      bool
	detachEr error
}

func (f *fakeToolServers) Attach(_ context.Context, owner, name, url string, headers map[string]string) ([]string, error) {
	// 주인까지 적는다 — 안 적으면 「누구 것으로 붙였는가」가 시험 밖으로 새고, 그 값이 바로
	// 이 인자가 생긴 이유다.
	f.attached = append(f.attached, owner+"|"+name+"|"+url+"|"+strings.Join(sortedKeys(headers), ","))
	return f.names, f.attachEr
}

func (f *fakeToolServers) Detach(owner, name string) (bool, error) {
	f.detached = append(f.detached, name)
	return f.had, f.detachEr
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A build that attaches no tool servers says so, rather than reporting the shape of an answer.
//
// Detach is the half that matters: its "no" already means "there was nothing to remove", which a
// caller cleaning up after a crash reads as "already clean" and stops. A build with no door at all
// answering the same way would have that caller believe it had tidied something it never could.
func TestABuildWithNoDoorSaysSoRatherThanAnsweringNo(t *testing.T) {
	a := &App{tools: builtin.Default()}
	if _, err := a.AttachToolServer(context.Background(), "", "editor", "http://localhost:1/mcp", nil); err == nil {
		t.Error("a build that attaches nothing accepted an attach")
	}
	had, err := a.DetachToolServer("", "editor")
	if err == nil {
		t.Error("a build that attaches nothing accepted a detach")
	}
	if had {
		t.Error("it also claimed there had been a server to remove")
	}
}

// UseToolServers is the door itself, opened once at wiring time. Before it the App refuses; after
// it the same call reaches the manager.
func TestTheDoorIsWhatWiringHandsOver(t *testing.T) {
	a := &App{tools: builtin.Default()}
	srv := &fakeToolServers{names: []string{"mcp__editor__open"}}
	a.UseToolServers(srv)

	got, err := a.AttachToolServer(context.Background(), "", "editor", "http://localhost:9/mcp",
		map[string]string{"Authorization": "Bearer x"})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// The names are the answer, not an ack: they are what the caller may ask for now.
	if len(got) != 1 || got[0] != "mcp__editor__open" {
		t.Errorf("the attach answered %v", got)
	}
	// The owner rides along with them: empty here, which is the whole daemon and what every caller
	// meant before conversations could own a server (port.ToolServers).
	if len(srv.attached) != 1 || srv.attached[0] != "|editor|http://localhost:9/mcp|Authorization" {
		t.Errorf("the manager was asked for %v — the owner, the name, the url and the headers are "+
			"the whole of what this door passes on", srv.attached)
	}
}

// The owner is passed on, not dropped. Without this the door could take the argument and ignore it,
// and every conversation would keep seeing every other conversation's tools — which is the whole of
// what this parameter exists to stop (internal/adapter/mcp/SESSION_SCOPE.md).
func TestTheOwnerReachesTheManager(t *testing.T) {
	srv := &fakeToolServers{names: []string{"mcp__ppt__list_slides"}}
	a := &App{tools: builtin.Default()}
	a.UseToolServers(srv)
	if _, err := a.AttachToolServer(context.Background(), "sess-b", "ppt", "http://localhost:9/mcp", nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(srv.attached) != 1 || !strings.HasPrefix(srv.attached[0], "sess-b|ppt|") {
		t.Errorf("주인이 매니저까지 안 갔다: %v", srv.attached)
	}
}

// A server may not take a name the companion already answers to. MCP tools are namespaced, so such
// a server cannot shadow a builtin by advertising one — but a server CALLED `read` reads oddly in
// every list that names it, and the refusal has to happen before the manager is asked.
func TestAServerMayNotBeCalledWhatAToolIsCalled(t *testing.T) {
	a := &App{tools: builtin.Default()}
	srv := &fakeToolServers{}
	a.UseToolServers(srv)
	taken := a.ToolNames()
	if len(taken) == 0 {
		t.Fatal("the companion has no tools, so this proves nothing")
	}
	for _, name := range []string{taken[0], taken[len(taken)-1]} {
		if _, err := a.AttachToolServer(context.Background(), "", name, "http://localhost:9/mcp", nil); err == nil {
			t.Errorf("a server was allowed to be called %q, which is a tool this companion has", name)
		}
	}
	if len(srv.attached) != 0 {
		t.Errorf("the manager was asked anyway: %v", srv.attached)
	}
	// A name nothing answers to goes through.
	if _, err := a.AttachToolServer(context.Background(), "", "slides", "http://localhost:9/mcp", nil); err != nil {
		t.Errorf("a free name was refused: %v", err)
	}
}

// Detach reports the two facts separately, because "no" and "refused" are different things to a
// caller reconnecting: nothing to clean up, versus a server that is not yours to take away.
func TestDetachSaysWhetherThereWasOneAndWhetherItWasAllowed(t *testing.T) {
	refused := errors.New("the operator declared this one")
	for _, c := range []struct {
		name    string
		srv     *fakeToolServers
		had     bool
		wantErr bool
	}{
		{"nothing to remove", &fakeToolServers{had: false}, false, false},
		{"removed", &fakeToolServers{had: true}, true, false},
		{"there and refused", &fakeToolServers{had: true, detachEr: refused}, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := &App{tools: builtin.Default()}
			a.UseToolServers(c.srv)
			had, err := a.DetachToolServer("", "editor")
			if had != c.had {
				t.Errorf("there was one: %v", had)
			}
			if (err != nil) != c.wantErr {
				t.Errorf("the refusal came back as %v", err)
			}
			if len(c.srv.detached) != 1 || c.srv.detached[0] != "editor" {
				t.Errorf("the manager was asked to remove %v", c.srv.detached)
			}
		})
	}
}
