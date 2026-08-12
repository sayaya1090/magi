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

// An invitation joins two machines in one exchange, and closes behind itself.
//
// The four steps it replaces — read a fingerprint here, carry it, admit it there, write down an
// address — are four chances to stop, and the last one is the one people skip. What it must not
// cost is the property that makes admission worth anything: the joining machine's key is taken
// from the certificate it presented, so it cannot claim somebody else's.
func TestAnInvitationAdmitsBothSidesAndThenCloses(t *testing.T) {
	server, caller := shortTempDir(t), shortTempDir(t)
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

	// Before the invitation, the caller cannot even finish a handshake.
	var early strings.Builder
	if code := fleetJoin(caller, "laptop", addr, "not-a-real-token", serverID.Fingerprint(), &early); code == 0 {
		t.Error("a join with no invitation succeeded")
	}

	token, err := identity.Mint(server, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	if code := fleetJoin(caller, "laptop", addr, token, serverID.Fingerprint(), &said); code != 0 {
		t.Fatalf("join: %s", said.String())
	}
	// Both directions, from one command.
	if _, ok := identity.AdmittedPeer(server, callerID.Fingerprint()); !ok {
		t.Error("the machine that invited did not admit the one that joined")
	}
	p, ok := identity.AdmittedPeer(caller, serverID.Fingerprint())
	if !ok {
		t.Fatal("the machine that joined did not record the one that invited")
	}
	if p.Addr != addr {
		t.Errorf("it recorded the address as %q", p.Addr)
	}
	// And the window shut: the invitation is spent, so the next stranger is back to failing the
	// handshake.
	if identity.Inviting(server) {
		t.Error("the invitation is still open after being taken")
	}
	other := shortTempDir(t)
	if _, err := identity.Load(other, "stranger"); err != nil {
		t.Fatal(err)
	}
	var late strings.Builder
	if code := fleetJoin(other, "stranger", addr, token, serverID.Fingerprint(), &late); code == 0 {
		t.Error("the spent invitation let a second machine in")
	}
}

// A join is pinned to the fingerprint the invitation named, before the secret is sent.
//
// The invitation is a bearer secret: handed to the wrong machine it would both leak and admit that
// machine. So the address is not trusted — what answers it has to be what the inviter printed.
func TestAJoinWillNotHandTheSecretToTheWrongMachine(t *testing.T) {
	server, caller := shortTempDir(t), shortTempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go fleetServe(ctx, addr, server, "buildbox", &strings.Builder{})
	waitFor(t, addr)
	token, err := identity.Mint(server, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	// Somebody else's fingerprint on the command line: the connection ends before the POST.
	code := fleetJoin(caller, "laptop", addr, token,
		"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", &said)
	if code == 0 {
		t.Fatal("the join went ahead against a machine it was not told to expect")
	}
	if identity.Inviting(server) == false {
		t.Error("the invitation was spent by a join that never got that far")
	}
}

// A stranger is stopped by the handshake, and during a join window by the route.
//
// The two are different and the difference is the design. With nothing open, an unadmitted party
// gets a TLS failure: no route, no version, no words of ours at all. While somebody is inviting,
// the handshake has to accept an unknown certificate — that is the only way the one route that
// takes an invitation can be reached — so everything ELSE re-checks admission and says so in the
// protocol. A test that only asserts "it failed" cannot tell those apart, and both mutations of
// this pair passed one that did.
func TestAStrangerIsStoppedByTheHandshakeUnlessSomebodyIsInviting(t *testing.T) {
	server, caller := shortTempDir(t), shortTempDir(t)
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
	if _, err := identity.Load(caller, "laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Admit(caller, identity.Peer{
		Fingerprint: serverID.Fingerprint(), Label: "buildbox", Addr: addr}); err != nil {
		t.Fatal(err)
	}
	peer, _ := peerFor(caller, "buildbox")

	try := func() string {
		dial, derr := fleetTo(context.Background(), caller, "laptop", peer, sock)
		if derr != nil {
			t.Fatal(derr)
		}
		_, aerr := daemon.Over(dial).About()
		if aerr == nil {
			t.Fatal("a machine nobody admitted was served")
		}
		return aerr.Error()
	}

	// Nothing open: the connection itself is refused, so what comes back is TLS's complaint and
	// not ours. A JSON refusal here would mean a stranger had reached a route.
	shut := try()
	if strings.Contains(shut, "has not admitted you") {
		t.Errorf("a stranger reached the route with no invitation open: %q", shut)
	}
	if !strings.Contains(shut, "certificate") && !strings.Contains(shut, "tls") &&
		!strings.Contains(shut, "handshake") && !strings.Contains(shut, "EOF") {
		t.Errorf("the refusal does not look like a handshake failure: %q", shut)
	}

	// Somebody is inviting: the handshake must now accept an unknown certificate, so the ROUTE is
	// what refuses — in the protocol, because by then there is a client waiting on a JSON line.
	if _, err := identity.Mint(server, "somebody-else"); err != nil {
		t.Fatal(err)
	}
	open := try()
	if !strings.Contains(open, "has not admitted you") {
		t.Errorf("during a join window the route did not refuse an unadmitted caller: %q", open)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*reached) != 0 {
		t.Errorf("a stranger reached the companion %d times", len(*reached))
	}
}
