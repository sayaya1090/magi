package fleet

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// The two mapping halves must be lossless for what each half owns: a local row rebuilds the
// published record (so the screen through the door knows what it knew reading the file), and a
// sighting rebuilds the member with its Seen recovered from the age (so Fresh and the hub
// election read it exactly as they would have read the file).
func TestRosterRowsMapLosslessly(t *testing.T) {
	in := rosterInfo(daemon.RosterRow{
		Socket: "/tmp/daemon-a.sock", Workdir: "/w/a", Session: "s_a",
		Name: "alpha", Role: "builds", Team: "core", Hub: true, Can: 3,
		Does: []string{"build", "test"}, Waiting: 2, Handling: true,
		PID: 42, Addr: "10.0.0.5", Started: "2026-08-29T01:00:00Z",
		Host: "box", Account: "me", State: "waiting", Version: "v9",
	})
	if in.Session != "s_a" || in.PID != 42 || in.Started != "2026-08-29T01:00:00Z" ||
		in.Team != "core" || !in.Hub || in.State != "waiting" || in.Waiting != 2 || !in.Handling {
		t.Fatalf("the record did not survive the trip through the door: %+v", in)
	}
	// Live IS copied — the light list draws it as-answered — and stays safe on the full path
	// because Probe resets it before dialing. The mapping input above says nothing about Live,
	// so it reads false here; the copy itself is pinned by the integration test's live row.
	if in.Live {
		t.Fatal("this fixture's row did not claim liveness")
	}

	now := time.Now()
	m := rosterMember(daemon.RosterRow{
		Host: "otherbox", Socket: "/tmp/daemon-b.sock", Name: "beta", State: "working",
		By: "pk_other", Sighting: true, AgeSeconds: 40,
	}, now)
	if got := now.Sub(m.Seen); got < 39*time.Second || got > 41*time.Second {
		t.Fatalf("Seen must be recovered from the age, got %v ago", got)
	}
	if m.By != "pk_other" || m.Host != "otherbox" || m.State != "working" {
		t.Fatalf("the sighting's own facts must arrive as told: %+v", m)
	}
	if !m.Fresh(now) {
		t.Fatal("a 40s-old sighting is fresh, and the row must read that way rebuilt")
	}
}

// A directory with no sockets has no door to ask, and the caller must know to fall back — nil
// with ok=false, not an empty fleet asserted by nobody.
func TestRosterSourcesFallsBackWhenNobodyAnswers(t *testing.T) {
	locals, ms, ok := rosterSources(t.TempDir(), time.Now())
	if ok || locals != nil || ms != nil {
		t.Fatalf("no socket answered: the fallback signal is ok=false, got (%v, %v, %v)", locals, ms, ok)
	}
}
