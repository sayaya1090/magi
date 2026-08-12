package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/identity"
)

// A crossing over TLS reaches an admitted machine, and only an admitted one.
//
// The whole point of checking in the handshake is that a stranger learns nothing: not a route, not
// a version, not which companions are here. So the test is not "it answered no" — it is "it never
// got as far as being answered".
func TestTheFleetDoorOverTLSAdmitsOnlyWhatWasAdmitted(t *testing.T) {
	// Short paths: a companion's socket lives in its own config directory, and a unix address is
	// capped at ~104 bytes.
	server := shortTempDir(t) // the machine being reached
	caller := shortTempDir(t) // the machine reaching out

	// A companion on the server side, and a fake daemon behind its socket.
	sock := filepath.Join(server, "daemon-api.sock")
	reached, mu := fakeDaemon(t, sock)
	stop, err := daemon.Publish(sock, t.TempDir(), "s1", daemon.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go fleetServe(ctx, addr, server, "buildbox", &strings.Builder{})
	waitFor(t, addr)

	serverID, err := identity.Load(server, "buildbox")
	if err != nil {
		t.Fatal(err)
	}
	callerID, err := identity.Load(caller, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	// The caller knows where the server is and what it looks like…
	if _, err := identity.Admit(caller, identity.Peer{
		Fingerprint: serverID.Fingerprint(), Label: "buildbox", Addr: addr}); err != nil {
		t.Fatal(err)
	}
	peer, ok := peerFor(caller, "buildbox")
	if !ok {
		t.Fatal("the admitted machine cannot be found by name")
	}

	// …but the server has not admitted the caller yet, so the handshake ends there.
	dial, err := fleetTo(context.Background(), caller, "laptop", peer, sock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Over(dial).About(); err == nil {
		t.Error("a machine nobody admitted was served")
	}
	mu.Lock()
	before := len(*reached)
	mu.Unlock()
	if before != 0 {
		t.Errorf("the unadmitted caller reached the daemon %d times", before)
	}

	// Admitted, the same crossing works.
	if _, err := identity.Admit(server, identity.Peer{
		Fingerprint: callerID.Fingerprint(), Label: "laptop"}); err != nil {
		t.Fatal(err)
	}
	dial2, err := fleetTo(context.Background(), caller, "laptop", peer, sock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Over(dial2).About(); err != nil {
		t.Fatalf("an admitted machine was refused: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*reached) == 0 {
		t.Fatal("nothing reached the daemon")
	}
	if got := strings.Join((*reached)[len(*reached)-1], ","); got != "about" {
		t.Errorf("what arrived was %q", got)
	}
}

// And the same three methods, over this pipe too: the rule is the door's, not the transport's.
func TestTheTLSDoorRefusesTheSameMethods(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"submit", "shell", "set-model", "watch"} {
		if doorAllows(m) {
			t.Errorf("%s is carried", m)
		}
	}
	// The handler refuses before it looks for a companion, so a refused method cannot be used to
	// probe which sockets exist.
	body, _ := json.Marshal(map[string]string{"socket": "/nope.sock", "method": "submit"})
	req := httptest.NewRequest(http.MethodPost, fleetPath, bytes.NewReader(body))
	w := httptest.NewRecorder()
	fleetHandle(w, req, dir)
	var resp daemon.Response
	if json.Unmarshal(w.Body.Bytes(), &resp) != nil || !strings.Contains(resp.Err, "fleet door carries") {
		t.Errorf("the refusal was %q", w.Body.String())
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitFor(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the fleet door never came up")
}
