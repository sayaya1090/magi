package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// sink is a tool registry that only remembers names, which is all these tests ask about.
type namesSink struct{ names map[string]bool }

func (s *namesSink) Register(t port.Tool) {
	if s.names == nil {
		s.names = map[string]bool{}
	}
	s.names[t.Name()] = true
}
func (s *namesSink) Unregister(name string) { delete(s.names, name) }

// mcpHTTP is the smallest server that completes a handshake and offers one tool.
func mcpHTTP(t *testing.T, tool string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",` +
				`"capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"` + tool +
				`","description":"d","inputSchema":{"type":"object"}}]}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

// The door answers with what it attached, not with an ack: the caller needs to know what it may
// ask for now, and "ok" cannot tell "attached and offers three tools" from "attached and offers
// none".
func TestAttachAnswersWithTheToolsItBrought(t *testing.T) {
	srv := mcpHTTP(t, "render")
	defer srv.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	names, err := m.Attach(context.Background(), "ppt", srv.URL, nil)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(names) != 1 || names[0] != "mcp__ppt__render" {
		t.Fatalf("attached %v — the namespaced name is what the model calls", names)
	}
	if !sink.names["mcp__ppt__render"] {
		t.Error("the tool is not in the registry the model reads")
	}
}

// A name is held while its server is attached — the defence that was unreachable while every name
// came from a config map, and became reachable the day something outside started attaching.
func TestTheSameNameCannotBeAttachedTwice(t *testing.T) {
	a, b := mcpHTTP(t, "one"), mcpHTTP(t, "two")
	defer a.Close()
	defer b.Close()
	m := NewManager(&namesSink{})
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt", a.URL, nil); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	_, err := m.Attach(context.Background(), "ppt", b.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "already attached") {
		t.Fatalf("second attach said %v — a taken name must be refused by name", err)
	}
}

// Detach is the other half of the door: a helper that crashed and came back sends it first,
// because the dead registration is holding its name.
func TestDetachFreesTheNameAndTheTools(t *testing.T) {
	srv := mcpHTTP(t, "render")
	defer srv.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt", srv.URL, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !m.Detach("ppt") {
		t.Fatal("detach said there was nothing attached")
	}
	if sink.names["mcp__ppt__render"] {
		t.Error("the tool is still advertised after its server was detached")
	}
	if m.Detach("ppt") {
		t.Error("detaching twice reported a removal the second time")
	}
	if _, err := m.Attach(context.Background(), "ppt", srv.URL, nil); err != nil {
		t.Fatalf("re-attach after detach: %v — that is the reconnect path", err)
	}
}

// A helper that crashes cannot send detach; that is the normal path, not the exception. Three
// calls in a row that reach nobody take its tools out of the list.
func TestAServerThatStopsAnsweringIsDropped(t *testing.T) {
	srv := mcpHTTP(t, "render")
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt", srv.URL, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	srv.Close() // the helper dies without a word

	tool, ok := sink.names["mcp__ppt__render"], true
	if !tool {
		t.Fatal("nothing was registered to begin with")
	}
	m.mu.Lock()
	sc := m.servers["ppt"]
	m.mu.Unlock()
	if sc == nil {
		t.Fatal("no server record")
	}
	for i := 0; i < unreachableStreak && ok; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = sc.client.CallTool(ctx, "render", json.RawMessage(`{}`))
		cancel()
	}
	// Remove runs from the goroutine watching Done(); give it a moment to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, still := m.servers["ppt"]
		m.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m.mu.Lock()
	_, still := m.servers["ppt"]
	m.mu.Unlock()
	if still {
		t.Error("a server nobody can reach is still attached")
	}
	if sink.names["mcp__ppt__render"] {
		t.Error("its tools are still advertised to the model — a hand that is not there")
	}
}
