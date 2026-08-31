package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A companion's runtime facts only exist in the running process, and the roster row is where a
// console meets them.
//
// The approval mode it is on, the backend its requests go to, the model the conversation is on:
// none of these are in the published file, and List gets them by asking the door. The row built
// from that listing dropped them — so a console reading the roster (which is every console: the
// light listing prefers this door and only falls back to the file when nobody answers) drew those
// fields empty, and then offered a control to CHANGE a value it had never shown. Measured live:
// approval and model blank on a daemon that answered "allow" and "gpt-oss:120b-cloud" to a probe.
type runtimeEngine struct{ controllingEngine }

func (*runtimeEngine) ModelOf(session.SessionID) string { return "qwen3-coder" }

func TestRosterRowCarriesTheRuntimeFacts(t *testing.T) {
	// shortRoot, not t.TempDir(): a unix socket path caps near 100 bytes on darwin and the test
	// tempdir is most of that on its own — and a hard-coded /tmp is what kept this whole package
	// from running on Windows at all.
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-self.sock")
	d, lerr := Listen(sock)
	if lerr != nil {
		t.Fatal(lerr)
	}
	t.Cleanup(d.Stop)
	eng := &runtimeEngine{}
	eng.SetPermission("allow")
	go func() { _ = d.Serve(context.Background(), eng) }()
	if _, err := Publish(sock, "/w/self", "s_self", Identity{Name: "self"}); err != nil {
		t.Fatal(err)
	}

	rows, err := ProbeRoster(sock)
	if err != nil {
		t.Fatalf("the door did not answer: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the daemon should list itself, got %+v", rows)
	}
	got := rows[0]
	if got.Permission != "allow" {
		t.Errorf("the approval mode the engine is on must ride the row, got %q", got.Permission)
	}
	if got.Model != "qwen3-coder" {
		t.Errorf("the model this conversation is on must ride the row, got %q", got.Model)
	}
	if got.Backend != "http://127.0.0.1:1/v1" {
		t.Errorf("the backend its requests go to must ride the row, got %q", got.Backend)
	}
}
