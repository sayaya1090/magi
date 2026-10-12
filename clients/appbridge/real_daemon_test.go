package appbridge

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type integrationEngine struct {
	*app.App
	workdir string
}

func (e integrationEngine) SessionsHere(ctx context.Context) ([]session.SessionMeta, error) {
	return e.App.ListLiveSessions(ctx, e.workdir)
}
func (e integrationEngine) NewSession(ctx context.Context) (session.SessionID, error) {
	return e.NewSessionKeeping(ctx, "")
}
func (e integrationEngine) NewSessionKeeping(ctx context.Context, purpose string) (session.SessionID, error) {
	return e.App.CreateSession(ctx, command.CreateSession{Workdir: e.workdir, For: purpose})
}

// Real App + daemon + MCP transport; no model/provider/account is involved.
func TestBridgeAgainstActualDaemonAndMCPManager(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "magi-app-integration-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	store, err := jsonl.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := builtin.NewRegistry()
	engine := app.New(store, nil, registry, bus.New(), nil, app.Config{ClientRole: "mail", System: app.ClientRoleSystem("mail")})
	defer engine.Close(context.Background())
	manager := mcp.NewManager(registry)
	defer manager.Close()
	engine.UseToolServers(manager)
	socket := filepath.Join(dir, "d.sock")
	d, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer d.Close()
	go d.Serve(ctx, integrationEngine{App: engine, workdir: dir})
	bridge, err := New(Config{App: "mail", Token: "integration", Instructions: "selected mail only", Tools: []Tool{{Name: "mail_read", ReadOnly: true, InputSchema: map[string]any{"type": "object"}}}, Socket: func(context.Context, string) (string, error) { return socket, nil }})
	if err != nil {
		t.Fatal(err)
	}
	bridge.Config.Call = func(_ context.Context, target, name string, _ json.RawMessage) (any, error) {
		return map[string]string{"target": target, "tool": name}, nil
	}
	httpServer := httptest.NewServer(bridge.Mux)
	defer httpServer.Close()
	bridge.Config.Addr = strings.TrimPrefix(httpServer.URL, "http://")
	deadline, cancelDeadline := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDeadline()
	b, err := bridge.Connect(deadline, "account/INBOX/1/7")
	if err != nil {
		t.Fatal(err)
	}
	// A newly opened document has no events yet. It must still be subscribable.
	follower, err := daemon.DialWithin(socket, time.Second, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	caughtUp := false
	_ = follower.Follow(b.Session, 0, daemon.Tail{Each: func(event.Event) bool { return false }, CaughtUp: func() { caughtUp = true; follower.Close() }})
	follower.Close()
	if !caughtUp {
		t.Fatal("new document session was rejected before its first message")
	}
	visible := map[string]bool{"mcp__mail__mail_read": true}
	if manager.Instructions(b.Session, visible)["mail"] != "selected mail only" {
		t.Fatal("initialize guidance missing")
	}
	tool, found := registry.Get("mcp__mail__mail_read")
	if !found {
		t.Fatal("real MCP registration missing")
	}
	result, err := tool.Execute(deadline, json.RawMessage(`{}`), port.ToolEnv{SessionID: session.SessionID(b.Session)})
	if err != nil || result.IsError || !strings.Contains(string(result.Content), "account/INBOX/1/7") {
		t.Fatalf("actual MCP call failed: %#v %v", result, err)
	}
	foreign, err := tool.Execute(deadline, json.RawMessage(`{}`), port.ToolEnv{SessionID: "foreign"})
	if err != nil || !foreign.IsError {
		t.Fatalf("foreign session executed: %#v %v", foreign, err)
	}
	if _, err = bridge.Request(deadline, b.ID, Request{Method: "new"}); err != nil {
		t.Fatal(err)
	}
	next, err := bridge.binding(b.ID)
	if err != nil || next.Session == b.Session {
		t.Fatalf("new session: %#v %v", next, err)
	}
	if len(manager.Instructions(b.Session, visible)) != 0 || manager.Instructions(next.Session, visible)["mail"] != "selected mail only" {
		t.Fatal("MCP ownership did not follow session transition")
	}
	listed, err := bridge.Request(deadline, b.ID, Request{Method: "sessions"})
	if err != nil || !strings.Contains(string(listed), b.Session) || !strings.Contains(string(listed), next.Session) {
		t.Fatalf("document session list: %s %v", listed, err)
	}
	if _, err = bridge.Request(deadline, b.ID, Request{Method: "resume", Name: "foreign"}); err == nil {
		t.Fatal("foreign session selected")
	}
	if _, err = bridge.Request(deadline, b.ID, Request{Method: "resume", Name: b.Session}); err != nil {
		t.Fatal(err)
	}
	resumed, err := bridge.binding(b.ID)
	if err != nil || resumed.Session != b.Session {
		t.Fatalf("resume binding: %+v %v", resumed, err)
	}
	if len(manager.Instructions(next.Session, visible)) != 0 || manager.Instructions(b.Session, visible)["mail"] != "selected mail only" {
		t.Fatal("MCP did not return to resumed session")
	}
	if err = bridge.Disconnect(deadline, b.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.Instructions(b.Session, visible)) != 0 {
		t.Fatal("disconnected MCP retained")
	}
}

// External coding conversation uses the same browser backend without creating a browser turn.
func TestBrowserProxyAgainstActualExternalDaemon(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "magi-browser-external-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	store, err := jsonl.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := builtin.NewRegistry()
	engine := app.New(store, nil, registry, bus.New(), nil, app.Config{ClientRole: "coding"})
	defer engine.Close(context.Background())
	socket := filepath.Join(dir, "daemon-external.sock")
	d, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer d.Close()
	go d.Serve(ctx, integrationEngine{App: engine, workdir: dir})
	cl, err := daemon.DialWithin(socket, time.Second, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	sid, err := cl.NewSessionKeepingFor("coding target")
	if err != nil {
		t.Fatal(err)
	}
	engine.SetModel(session.SessionID(sid), "fixture-model")
	unpublish, err := daemon.Publish(socket, dir, sid, daemon.Identity{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer unpublish()
	bridge, err := New(Config{App: "browser", ConfigDir: dir, Token: "test", Tools: []Tool{{Name: "read", InputSchema: map[string]any{"type": "object"}}}, Call: func(_ context.Context, target, name string, _ json.RawMessage) (any, error) {
		return map[string]string{"target": target, "name": name}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	primary := &Binding{ID: "chrome", Session: "ui", Target: "allowed Chrome", socket: socket, pending: make(chan *toolCall, 8), lastPoll: time.Now()}
	bridge.bindings[primary.ID] = primary
	lease, _, err := bridge.AttachBrowser(ctx, primary.ID, socket, sid, true)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"lease": lease.ID, "socket": socket, "session": sid, "name": "read", "args": map[string]any{}})
	response := browserPost(bridge, "/api/browser/call", string(payload))
	if response.Code != 200 || !strings.Contains(response.Body.String(), lease.ID) {
		t.Fatal(response.Code, response.Body.String())
	}
	rows, err := cl.Sessions()
	if err != nil || len(rows) != 1 || rows[0].ID != sid {
		t.Fatal("proxy changed conversation", rows, err)
	}
	if err := bridge.Disconnect(ctx, lease.ID); err != nil {
		t.Fatal(err)
	}
}
