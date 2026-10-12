package browser_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/clients/appbridge"
	clientbrowser "github.com/sayaya1090/magi/clients/browser"
	"github.com/sayaya1090/magi/clients/browser/headless"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	browsertool "github.com/sayaya1090/magi/internal/adapter/tool/browser"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type liveEngine struct {
	*app.App
	wd string
}

func (e liveEngine) SessionsHere(ctx context.Context) ([]session.SessionMeta, error) {
	return e.ListSessions(ctx, e.wd)
}
func (e liveEngine) NewSession(ctx context.Context) (session.SessionID, error) {
	return e.NewSessionKeeping(ctx, "")
}
func (e liveEngine) NewSessionKeeping(ctx context.Context, purpose string) (session.SessionID, error) {
	sid, err := e.CreateSession(ctx, command.CreateSession{Workdir: e.wd, For: purpose})
	if err == nil {
		e.SetModel(sid, "test-model")
	}
	return sid, err
}

// Real daemon, appbridge, browser proxy and isolated Chromium; no model/account.
func TestLiveProxyAndDelegatedSessionUseIndependentProfiles(t *testing.T) {
	if os.Getenv("MAGI_HEADLESS_LIVE") != "1" {
		t.Skip("set MAGI_HEADLESS_LIVE=1 for full headless integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "magi-browser-live-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	store, err := jsonl.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := builtin.NewRegistry()
	registry.Register(&browsertool.Access{ConfigDir: dir})
	engine := app.New(store, nil, registry, bus.New(), nil, app.Config{ClientRole: "coding"})
	defer engine.Close(context.Background())
	manager := mcp.NewManager(registry)
	engine.UseToolServers(manager)
	defer manager.Close()
	socket := daemon.SocketPath(dir, dir)
	listener, err := daemon.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go listener.Serve(ctx, liveEngine{engine, dir})
	source, err := engine.CreateSession(ctx, command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	engine.SetModel(source, "test-model")
	unpublished, err := daemon.Publish(socket, dir, string(source), daemon.Identity{Name: "fixture", Role: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer unpublished()
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<p>Before</p><input aria-label="name"><button onclick="document.querySelector('p').textContent='After'">Go</button>`))
	}))
	defer fixture.Close()
	backend, err := headless.New(ctx, headless.Config{AllowedOrigins: []string{fixture.URL}, InitialURL: fixture.URL, AllowActions: true})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	bridge, err := appbridge.New(appbridge.Config{App: "browser", ConfigDir: dir, Token: "0123456789abcdef", Tools: clientbrowser.Tools(), Call: backend.Call, Release: backend.Release, Socket: func(context.Context, string) (string, error) { return socket, nil }})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(bridge.Mux)
	defer server.Close()
	bridge.Config.Addr = strings.TrimPrefix(server.URL, "http://")
	parent, err := bridge.Connect(ctx, "headless-tests")
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Disconnect(context.Background(), parent.ID)
	cleanup, err := browsertool.Publish(dir, "headless", browsertool.Runtime{URL: server.URL, Token: bridge.Token})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	access := &browsertool.Access{ConfigDir: dir, Socket: func(port.ToolEnv) string { return socket }}
	invoke := func(sid session.SessionID, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r, e := access.Execute(ctx, raw, port.ToolEnv{SessionID: sid, Workdir: dir})
		if e != nil || r.IsError {
			t.Fatalf("browser call %s: %s %v", raw, r.Content, e)
		}
		var out map[string]any
		if e = json.Unmarshal(r.Content, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	invoke(source, map[string]any{"action": "connect"})
	// Lazy proxy must not attach full schemas to the requesting session.
	tool, ok := registry.Get("mcp__browser_runtime__read_page")
	if !ok || port.VisibleToSession(tool, string(source)) {
		t.Fatal("browser tool schemas leaked into requester")
	}
	read := func(sid session.SessionID) map[string]any {
		return invoke(sid, map[string]any{"action": "call", "tool": "read_page", "args": map[string]any{"tabId": 1}})
	}
	snap := read(source)
	clicked := false
	for _, raw := range snap["elements"].([]any) {
		el := raw.(map[string]any)
		if el["label"] == "Go" {
			invoke(source, map[string]any{"action": "call", "tool": "click", "args": map[string]any{"tabId": 1, "snapshot": snap["snapshot"], "elementId": el["elementId"]}})
			clicked = true
		}
	}
	if !clicked || !strings.Contains(read(source)["text"].(string), "After") {
		t.Fatal("click did not update fixture")
	}
	side, err := engine.CreateSession(ctx, command.CreateSession{Workdir: dir, Actor: event.Actor{Kind: event.ActorSystem, ID: app.HandoffActorID("coding requester")}})
	if err != nil {
		t.Fatal(err)
	}
	engine.SetModel(side, "test-model")
	invoke(side, map[string]any{"action": "connect"})
	if !strings.Contains(read(side)["text"].(string), "Before") {
		t.Fatal("delegated session shared requester profile")
	}
	if !strings.Contains(read(source)["text"].(string), "After") {
		t.Fatal("delegated session changed requester page")
	}
	invoke(side, map[string]any{"action": "release"})
	invoke(source, map[string]any{"action": "release"})
}
