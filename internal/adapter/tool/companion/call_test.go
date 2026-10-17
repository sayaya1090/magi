package companion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type policyPeer struct {
	heard
	cfg, wd string
}

func (p *policyPeer) CompanionOperation(ctx context.Context, action, id string, args json.RawMessage) (json.RawMessage, *session.ToolResult, error) {
	if action == "call" {
		r, e := companionpolicy.Call(ctx, p.cfg, p.wd, id, args)
		return nil, &r, e
	}
	c, e := companionpolicy.Load(p.cfg, p.wd)
	if e != nil {
		return nil, nil, e
	}
	if action == "describe" {
		op, e := companionpolicy.Find(c, id)
		if e != nil {
			return nil, nil, e
		}
		b, e := json.Marshal(op)
		return b, nil, e
	}
	b, e := json.Marshal(companionpolicy.Summaries(c))
	return b, nil, e
}
func TestCompanionCallDiscoversThenCallsWithoutHandingWork(t *testing.T) {
	tm := newTeam(t)
	peer := &policyPeer{cfg: tm.cfgDir}
	sock := tm.member("cap-peer", "gwt", "GWT plugin", peer)
	peer.wd = tm.workdirOf(sock)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "source"), []byte("unchanged 123"), 0600)
	path, e := companionpolicy.Path(peer.cfg, peer.wd)
	if e != nil {
		t.Fatal(e)
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(fmt.Sprintf("version=1\n[[operations]]\nid='source'\ndescription='Read source'\nkind='read_file'\nroot=%q", filepath.ToSlash(root))), 0600)
	tool := companion.Call{Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Reach: func(_ context.Context, _, socket string) (*daemon.Client, error) { return daemon.Dial(socket) }}
	for _, action := range []string{`{"who":"gwt","action":"list"}`, `{"who":"gwt","action":"describe","id":"source"}`, `{"who":"gwt","action":"call","id":"source","arguments":{"path":"source"}}`} {
		r, e := tool.Execute(context.Background(), json.RawMessage(action), port.ToolEnv{})
		if e != nil || r.IsError {
			t.Fatal(action, string(r.Content), e)
		}
		if strings.Contains(action, `"action":"call"`) && !strings.Contains(string(r.Content), "unchanged 123") {
			t.Fatal(string(r.Content))
		}
	}
	result, e := tool.Execute(context.Background(), json.RawMessage(`{"who":"gwt","action":"call","id":"source","arguments":{"path":"../secret"}}`), port.ToolEnv{})
	if e != nil || !result.IsError {
		t.Fatal(result, e)
	}
	if peer.given != 0 {
		t.Fatal("direct operation was delegated to the model")
	}
	os.Remove(path)
	r, e := tool.Execute(context.Background(), json.RawMessage(`{"who":"gwt","action":"call","id":"source","arguments":{"path":"source"}}`), port.ToolEnv{})
	if e != nil || !r.IsError {
		t.Fatal("revocation not propagated", r, e)
	}
}
func TestCompanionCallNeverFallsBackOnOldPeer(t *testing.T) {
	tm := newTeam(t)
	peer := &heard{}
	tm.member("old-peer", "old", "old companion", peer)
	tool := companion.Call{Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Reach: func(_ context.Context, _, socket string) (*daemon.Client, error) { return daemon.Dial(socket) }}
	r, e := tool.Execute(context.Background(), json.RawMessage(`{"who":"old","action":"list"}`), port.ToolEnv{})
	if e != nil || !r.IsError {
		t.Fatal(r, e)
	}
	if peer.given != 0 {
		t.Fatal("unsupported contract silently delegated work")
	}
}

type blockedPolicyPeer struct {
	heard
	entered chan struct{}
}

func (p *blockedPolicyPeer) CompanionOperation(ctx context.Context, _, _ string, _ json.RawMessage) (json.RawMessage, *session.ToolResult, error) {
	close(p.entered)
	<-ctx.Done()
	return nil, nil, ctx.Err()
}
func TestCompanionCallCancellationClosesLocalTransport(t *testing.T) {
	tm := newTeam(t)
	peer := &blockedPolicyPeer{entered: make(chan struct{})}
	tm.member("blocked-policy", "blocked", "slow peer", peer)
	tool := companion.Call{Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Reach: func(_ context.Context, _, socket string) (*daemon.Client, error) { return daemon.Dial(socket) }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan session.ToolResult, 1)
	go func() {
		r, _ := tool.Execute(ctx, json.RawMessage(`{"who":"blocked","action":"list"}`), port.ToolEnv{})
		done <- r
	}()
	select {
	case <-peer.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached peer")
	}
	cancel()
	select {
	case r := <-done:
		if !r.IsError {
			t.Fatal("cancelled request succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local transport ignored cancellation")
	}
}
