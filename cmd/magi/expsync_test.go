package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/adapter/identity"
	"github.com/sayaya1090/magi/internal/port"
)

// Only the two replicable shapes cross: a wiki revision and a memory, structurally checked.
// Everything a constructed path could reach — a traversal, a dot-file, the skills dir, the git
// dir — is refused before any filesystem call happens.
func TestExpSyncAdmitsOnlyReplicableShapes(t *testing.T) {
	for _, ok := range []string{
		"wiki/revisions/auth_flow/0001-melchior-a3f2.md",
		"memories/mem-9c1d.md",
	} {
		if !expSyncPathOK(ok) {
			t.Errorf("refused a replicable path: %s", ok)
		}
	}
	for _, bad := range []string{
		"../secrets.md", "wiki/revisions/../../x.md", "wiki/revisions/a/../x.md",
		"memories/../config.toml", "skills/skill-x.md", "wiki/pages/auth_flow.md",
		"wiki/revisions/a/.hidden.md", "memories/mem.txt", "wiki/revisions/a/b/c.md",
		"memories/", "wiki/revisions/a/0001\\x.md",
	} {
		if expSyncPathOK(bad) {
			t.Errorf("admitted a non-replicable path: %s", bad)
		}
	}
}

// Absorb is a set union with immutability: a path already held is never rewritten, whatever the
// incoming content claims — the names are content-addressed, so an overwrite could only ever
// replace a fact with an impostor.
func TestExpSyncAbsorbNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := "wiki/revisions/ports/0001-m-abc.md"
	if n := expSyncAbsorb(dir, map[string]string{p: "original"}); n != 1 {
		t.Fatalf("first absorb: %d", n)
	}
	if n := expSyncAbsorb(dir, map[string]string{p: "impostor"}); n != 0 {
		t.Fatalf("second absorb wrote: %d", n)
	}
	b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if string(b) != "original" {
		t.Fatalf("the held file was rewritten: %q", b)
	}
}

// Two machines that each learned something converge over the live TLS door: after one sweep each
// holds both revisions, and each store computes the SAME current page from the united set.
func TestExpSyncConvergesTwoMachinesOverTheDoor(t *testing.T) {
	server := shortTempDir(t)
	caller := shortTempDir(t)

	// Each machine's team tier learns one different fact about the same page.
	sdir := filepath.Join(server, "teams", "frontend", "experience")
	cdir := filepath.Join(caller, "teams", "frontend", "experience")
	ctx := context.Background()
	if err := expgit.New(sdir).Propose(ctx, port.Contribution{Source: "melchior",
		Wiki: []port.WikiEdit{{Page: "auth flow", Text: "gateway fronts it"}}}); err != nil {
		t.Fatal(err)
	}
	if err := expgit.New(cdir).Propose(ctx, port.Contribution{Source: "casper",
		Wiki: []port.WikiEdit{{Page: "auth flow", Text: "gateway fronts it; the SIDECAR refreshes tokens"}}}); err != nil {
		t.Fatal(err)
	}

	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go fleetServe(cctx, addr, server, "buildbox", &strings.Builder{})
	waitFor(t, addr)
	serverID, err := identity.Load(server, "buildbox")
	if err != nil {
		t.Fatal(err)
	}
	callerID, err := identity.Load(caller, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Admit(caller, identity.Peer{Fingerprint: serverID.Fingerprint(), Label: "buildbox", Addr: addr}); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Admit(server, identity.Peer{Fingerprint: callerID.Fingerprint(), Label: "laptop"}); err != nil {
		t.Fatal(err)
	}
	peer, ok := peerFor(caller, "buildbox")
	if !ok {
		t.Fatal("peer not found")
	}

	got, err := expSyncWithPeer(context.Background(), caller, "laptop", peer, "frontend", cdir)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got == 0 {
		t.Fatal("the caller absorbed nothing — the server's revision never crossed")
	}
	for _, dir := range []string{sdir, cdir} {
		pages, err := expgit.New(dir).WikiSearch(ctx, "auth flow", 1)
		if err != nil || len(pages) == 0 {
			t.Fatalf("%s: %v %d", dir, err, len(pages))
		}
		if !strings.Contains(pages[0].Body, "SIDECAR") {
			t.Errorf("%s did not converge to the newer revision: %q", dir, pages[0].Body)
		}
	}
	// And a second sweep is quiet: the sets are equal, nothing crosses.
	if again, err := expSyncWithPeer(context.Background(), caller, "laptop", peer, "frontend", cdir); err != nil || again != 0 {
		t.Fatalf("a converged pair must exchange nothing, got %d files err=%v", again, err)
	}
}

// A team with no footprint here is refused: a machine no companion of that team runs on holds no
// replica, so a stranger with a valid certificate cannot seed directories by naming teams.
func TestExpSyncRefusesAnUnknownTeam(t *testing.T) {
	server := shortTempDir(t)
	caller := shortTempDir(t)
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freePort(t)
	go fleetServe(cctx, addr, server, "buildbox", &strings.Builder{})
	waitFor(t, addr)
	serverID, _ := identity.Load(server, "buildbox")
	callerID, _ := identity.Load(caller, "laptop")
	if _, err := identity.Admit(caller, identity.Peer{Fingerprint: serverID.Fingerprint(), Label: "buildbox", Addr: addr}); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Admit(server, identity.Peer{Fingerprint: callerID.Fingerprint(), Label: "laptop"}); err != nil {
		t.Fatal(err)
	}
	peer, _ := peerFor(caller, "buildbox")
	dir := filepath.Join(caller, "teams", "ghosts", "experience")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := expSyncWithPeer(context.Background(), caller, "laptop", peer, "ghosts", dir); err == nil ||
		!strings.Contains(err.Error(), "no companion") {
		t.Fatalf("an unknown team must be refused by name, got %v", err)
	}
}
