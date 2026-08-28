package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// sink is a tool registry that only remembers names, which is all these tests ask about. Locked
// because a manager registers from whatever goroutine attached and unregisters from the one
// watching the server die — which is the arrangement under test.
type namesSink struct {
	mu    sync.Mutex
	named map[string]bool
}

func (s *namesSink) Register(t port.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.named == nil {
		s.named = map[string]bool{}
	}
	s.named[t.Name()] = true
}

func (s *namesSink) Unregister(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.named, name)
}

func (s *namesSink) has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.named[name]
}

func (s *namesSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.named)
}

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
	if !sink.has("mcp__ppt__render") {
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
	if removed, err := m.Detach("ppt"); err != nil || !removed {
		t.Fatalf("detach said removed=%v err=%v — it attached this one itself", removed, err)
	}
	if sink.has("mcp__ppt__render") {
		t.Error("the tool is still advertised after its server was detached")
	}
	if removed, err := m.Detach("ppt"); removed || err != nil {
		t.Errorf("detaching twice said removed=%v err=%v — already clean is an answer, not a failure", removed, err)
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

	tool, ok := sink.has("mcp__ppt__render"), true
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
	if sink.has("mcp__ppt__render") {
		t.Error("its tools are still advertised to the model — a hand that is not there")
	}
}

// Two callers attaching the same name at the same instant. The name used to be checked and then
// released for the length of a handshake — up to thirty seconds — so both passed the check and both
// succeeded: one connection stayed out of the map, never closed, its tools registered under names
// the other also claimed.
func TestTwoAttachesRacingForOneNameLeaveOneServer(t *testing.T) {
	a, b := mcpHTTP(t, "one"), mcpHTTP(t, "two")
	defer a.Close()
	defer b.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, url := range []string{a.URL, b.URL} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = m.Attach(context.Background(), "ppt", url, nil)
		}()
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of 2 attaches succeeded under one name: %v", won, errs)
	}
	if got := sink.count(); got != 1 {
		t.Errorf("%d tools registered — the loser left its own behind, and nothing can remove them", got)
	}
}

// The reservation is released when the handshake fails. Holding it would burn the name for the life
// of the daemon over one server that was not listening: worse than the collision it prevents.
func TestANameIsNotBurnedByAnAttachThatFailed(t *testing.T) {
	dead := mcpHTTP(t, "render")
	dead.Close() // nobody home
	live := mcpHTTP(t, "render")
	defer live.Close()

	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt", dead.URL, nil); err == nil {
		t.Fatal("attaching to a closed server succeeded")
	}
	if _, err := m.Attach(context.Background(), "ppt", live.URL, nil); err != nil {
		t.Fatalf("the name was still held after a failed attach: %v", err)
	}
}

// Two names that are one namespace. Tool names are sanitised — "ppt.one" and "ppt_one" both become
// mcp__ppt_one__* — so the second used to take the first's tool names silently, and detaching it
// left the first in the map claiming to be attached with nothing registered.
func TestTwoNamesThatSanitiseToOneAreRefused(t *testing.T) {
	a, b := mcpHTTP(t, "render"), mcpHTTP(t, "render")
	defer a.Close()
	defer b.Close()
	m := NewManager(&namesSink{})
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt.one", a.URL, nil); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	_, err := m.Attach(context.Background(), "ppt_one", b.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("second attach said %v — one namespace, one server", err)
	}
	// …and the name it was refused under is still the first one's to give up.
	if removed, err := m.Detach("ppt.one"); err != nil || !removed {
		t.Errorf("detach of the original name said removed=%v err=%v", removed, err)
	}
}

// The door removes what the door attached. A server the operator declared in config is not a
// caller's to take away: nothing here can put it back until the daemon restarts.
func TestTheDoorDoesNotRemoveAConfigServer(t *testing.T) {
	srv := mcpHTTP(t, "render")
	defer srv.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	if err := m.AddHTTP(context.Background(), "ppt", srv.URL, nil); err != nil {
		t.Fatalf("config attach: %v", err)
	}
	removed, err := m.Detach("ppt")
	if removed || err == nil {
		t.Fatalf("the door removed a config server (removed=%v err=%v)", removed, err)
	}
	if !sink.has("mcp__ppt__render") {
		t.Error("its tools went away anyway")
	}
	// But the lifetime net still holds it: a config server nobody can reach is as dead as any other.
	m.Remove("ppt")
	if sink.has("mcp__ppt__render") {
		t.Error("Remove left a config server's tools registered — that is the leak the net exists for")
	}
}

// mcpHTTPHeld is mcpHTTP whose tools/list waits, so a test can hold the reservation window — the
// stretch between claiming the name and publishing the server — open for as long as it likes.
func mcpHTTPHeld(t *testing.T, tool string, hold <-chan struct{}) *httptest.Server {
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
			<-hold
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"` + tool +
				`","description":"d","inputSchema":{"type":"object"}}]}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

// A detach that lands mid-handshake used to answer "removed" and then let the attach finish behind
// it: the caller — the crash-recovery case Detach's doc names — was told it was clean while the
// thing it wanted gone was still arriving, and one second later the map held it and its tools were
// advertised to the model. Either answer alone is defensible; the pair is a lie.
func TestDetachDuringAttachIsTrueOnlyIfTheAttachAlsoFoldsUp(t *testing.T) {
	hold := make(chan struct{})
	srv := mcpHTTPHeld(t, "render", hold)
	defer srv.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	attached := make(chan error, 1)
	go func() {
		_, err := m.Attach(context.Background(), "ppt", srv.URL, nil)
		attached <- err
	}()
	waitForServer(t, m, "ppt", true) // the reservation is in the map; the handshake is still out

	removed, err := m.Detach("ppt")
	if err != nil || !removed {
		t.Fatalf("detach of a name that is claimed: removed=%v err=%v, want true/nil", removed, err)
	}
	close(hold) // the handshake completes, into a map that no longer holds its reservation
	if err := <-attached; err == nil {
		t.Error("attach reported success after its name was detached out from under it — the caller " +
			"that asked for clean got clean, and the caller that asked for a server got one too")
	}
	m.mu.Lock()
	held := m.servers["ppt"]
	m.mu.Unlock()
	if held != nil {
		t.Errorf("detach said removed, then the attach landed anyway: %q is in the map", "ppt")
	}
	if sink.has("mcp__ppt__render") {
		t.Error("mcp__ppt__render is advertised to the model by a server nothing is holding")
	}
}

// A server's death must not reach past itself to its successor. Retiring the dead one closes its
// client, which wakes its lifetime net, and removing by NAME there deleted whatever had taken the
// name in the meantime — unregistering the replacement's tools right after Attach had handed those
// very names back to the caller as its answer.
//
// watch is called here rather than raced: the death that matters is the one that arrives late, and
// a goroutine racing a second attach can only be made likely.
func TestADeadServerDoesNotTakeItsReplacementWithIt(t *testing.T) {
	a := mcpHTTP(t, "render")
	defer a.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	if _, err := m.Attach(context.Background(), "ppt", a.URL, nil); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	dead := m.servers["ppt"]
	m.mu.Unlock()
	m.Remove("ppt") // the dead one is cleaned up and its client closed

	b := mcpHTTP(t, "render")
	defer b.Close()
	if _, err := m.Attach(context.Background(), "ppt", b.URL, nil); err != nil {
		t.Fatalf("replacement attach: %v", err)
	}
	if !sink.has("mcp__ppt__render") {
		t.Fatal("the replacement did not register its tool — this test would pass for the wrong reason")
	}

	m.watch("ppt", dead) // the dead one's net, arriving after the replacement took the name

	m.mu.Lock()
	held := m.servers["ppt"]
	m.mu.Unlock()
	if held == nil {
		t.Error("the dead server's cleanup deleted its replacement from the map")
	}
	if !sink.has("mcp__ppt__render") {
		t.Error("the dead server's cleanup unregistered the replacement's tool — Attach had already " +
			"answered with that name")
	}
}

// waitForServer waits until name is (or is not) claimed in the map.
func waitForServer(t *testing.T, m *Manager, name string, want bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		m.mu.Lock()
		_, got := m.servers[sanitizeToolPart(name)]
		m.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%q claimed=%v never became %v", name, !want, want)
}

// mcpHTTPRefusing holds tools/list open and then refuses it, so a test can keep a doomed attach's
// reservation alive for as long as it likes and choose the moment the failure path runs.
func mcpHTTPRefusing(t *testing.T, hold <-chan struct{}) *httptest.Server {
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
			<-hold
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-1,"message":"no"}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

// A failed attach releases the name so one unreachable server does not burn it for the life of the
// daemon. It must release ITS OWN claim and no other: released by "whatever is under the key and
// has no client yet", it reaches the reservation of the attach that took the name after this one
// lost it, and that second attach then fails too — refused on behalf of a server that was never in
// its way. Nothing removed it; it is told it was removed.
func TestAFailedAttachReleasesOnlyItsOwnClaim(t *testing.T) {
	doomedHold, replacementHold := make(chan struct{}), make(chan struct{})
	doomed := mcpHTTPRefusing(t, doomedHold)
	defer doomed.Close()
	replacement := mcpHTTPHeld(t, "render", replacementHold)
	defer replacement.Close()
	sink := &namesSink{}
	m := NewManager(sink)
	defer m.Close()

	firstDone := make(chan error, 1)
	go func() {
		_, err := m.Attach(context.Background(), "ppt", doomed.URL, nil)
		firstDone <- err
	}()
	waitForServer(t, m, "ppt", true)
	if removed, err := m.Detach("ppt"); !removed || err != nil {
		t.Fatalf("detach: removed=%v err=%v", removed, err)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := m.Attach(context.Background(), "ppt", replacement.URL, nil)
		secondDone <- err
	}()
	waitForServer(t, m, "ppt", true) // the second attach now holds the name

	close(doomedHold) // the first attach fails and runs its release
	if err := <-firstDone; err == nil {
		t.Fatal("the doomed attach reported success — this test would prove nothing")
	}
	close(replacementHold)
	if err := <-secondDone; err != nil {
		t.Errorf("the second attach was refused because the first one's failure released a claim "+
			"that was not its own: %v", err)
	}
	if !sink.has("mcp__ppt__render") {
		t.Error("mcp__ppt__render is not registered: the surviving attach never published")
	}
}
