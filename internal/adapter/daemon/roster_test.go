package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/cluster"
)

// The roster door answers both halves and keeps them tellable-apart: this machine's rows are
// measurements (session id, dial), other machines' are sightings (age, not commandable).
func TestRosterCarriesBothHalvesTellablyApart(t *testing.T) {
	home := t.TempDir()

	// A published local companion. The socket file exists but nothing listens, which is a real
	// state (a daemon that died without cleanup) and must still be listed — Live says the rest.
	sock := filepath.Join(home, "daemon-alpha.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(sock, "/w/alpha", "s_alpha", Identity{Name: "alpha", Role: "builds"}); err != nil {
		t.Fatal(err)
	}
	if err := NoteState(sock, "waiting"); err != nil {
		t.Fatal(err)
	}

	// A sighting of another machine's companion, aged half a minute.
	seen := time.Now().Add(-30 * time.Second)
	if err := writeMembers(home, []cluster.Member{{
		Host: "otherbox", Socket: "/tmp/daemon-beta.sock", Name: "beta",
		State: "working", Seen: seen,
	}}); err != nil {
		t.Fatal(err)
	}

	rows := buildRoster(home, time.Now())
	if len(rows) != 2 {
		t.Fatalf("one measurement and one sighting expected, got %+v", rows)
	}
	var local, sighted *RosterRow
	for i := range rows {
		if rows[i].Sighting {
			sighted = &rows[i]
		} else {
			local = &rows[i]
		}
	}
	if local == nil || sighted == nil {
		t.Fatalf("the two halves must both arrive, got %+v", rows)
	}

	// The local row is a measurement: it carries the conversation a client can subscribe to, the
	// pinned state vocabulary, and no age — the read just happened.
	if local.Session != "s_alpha" {
		t.Errorf("a local row carries the session id (the transcript entry point), got %q", local.Session)
	}
	if local.State != "waiting" {
		t.Errorf("state must pass through the pinned vocabulary untouched, got %q", local.State)
	}
	if local.AgeSeconds != 0 || local.Live {
		t.Errorf("nothing listens on this socket: age 0 and live=false expected, got %+v", *local)
	}
	if local.Name != "alpha" || local.Workdir != "/w/alpha" {
		t.Errorf("identity did not survive the trip: %+v", *local)
	}

	// The sighting is not a measurement: no session, marked, and its age is the fact's age.
	if sighted.Session != "" {
		t.Error("a sighting must not invent a session id nobody can subscribe to")
	}
	if sighted.AgeSeconds < 25 || sighted.AgeSeconds > 60 {
		t.Errorf("a 30s-old sighting should say so, got %d", sighted.AgeSeconds)
	}
	if sighted.Host != "otherbox" || sighted.State != "working" {
		t.Errorf("the sighting's own facts must arrive as told: %+v", *sighted)
	}
}

// An empty fleet is an answer, and a daemon with no home says why instead of answering "alone".
func TestRosterEmptyAndHomeless(t *testing.T) {
	resp := answerRoster(t.TempDir())
	if !resp.OK || resp.Roster == nil || len(resp.Roster) != 0 {
		t.Fatalf("an empty fleet answers an empty list, never null: %+v", resp)
	}
	if resp := answerRoster(""); resp.OK || resp.Err == "" {
		t.Fatalf("no home is 'cannot say', not 'you are alone': %+v", resp)
	}
}
