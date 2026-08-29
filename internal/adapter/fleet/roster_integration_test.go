package fleet

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// stillReader is a Reader with nothing to say — the listing under test is about discovery, not
// about what a log holds.
type stillReader struct{}

func (stillReader) UnfinishedTurnOf(context.Context, session.SessionID) (app.UnfinishedTurn, bool) {
	return app.UnfinishedTurn{}, false
}
func (stillReader) PlanOf(context.Context, session.SessionID) ([]session.Todo, error) {
	return nil, nil
}
func (stillReader) SessionState(context.Context, session.SessionID) ([]session.Message, int64, error) {
	return nil, 0, nil
}
func (stillReader) ListSessions(context.Context, string) ([]session.SessionMeta, error) {
	return nil, nil
}
func (stillReader) CouncilMarks(context.Context, session.SessionID) ([]app.CouncilMark, error) {
	return nil, nil
}
func (stillReader) RankSessions(context.Context, string, string) ([]app.SessionHit, error) {
	return nil, nil
}
func (stillReader) NewSince(context.Context, session.SessionID, int64) (int64, bool, error) {
	return 0, false, nil
}

// stillEngine answers the daemon's Engine with nothing in flight.
type stillEngine struct{}

func (stillEngine) Submit(context.Context, command.SubmitPrompt) error { return nil }
func (stillEngine) Steer(context.Context, command.SubmitPrompt) error  { return nil }
func (stillEngine) Interrupt(context.Context, command.Interrupt) error { return nil }
func (stillEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}
func (stillEngine) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }
func (stillEngine) Waiting(session.SessionID) (app.Ask, bool)                      { return app.Ask{}, false }
func (stillEngine) Doing(session.SessionID) (string, bool)                         { return "", false }

// shortHome is a config directory short enough to hold a unix socket path on darwin.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	return home
}

// With one companion alive, the listing's records come off its roster door — and the rows a
// reader sees are the same fleet they always were: the live self, and the sighting as an
// elsewhere row.
func TestListCachedTakesTheRosterPath(t *testing.T) {
	home := shortHome(t)
	sock := filepath.Join(home, "daemon-self.sock")
	d, err := daemon.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), stillEngine{}) }()
	if _, err := daemon.Publish(sock, "/w/self", "s_self", daemon.Identity{Name: "self"}); err != nil {
		t.Fatal(err)
	}
	// A sighting, planted the way gossip leaves it: the cluster file is a JSON array of members,
	// and reading it back does not re-verify — verification happened when it was heard.
	sighting, err := json.Marshal([]cluster.Member{{
		Host: "otherbox", Socket: "/tmp/daemon-far.sock", Name: "far",
		State: "working", Seen: time.Now().Add(-20 * time.Second),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "cluster.json"), sighting, 0o644); err != nil {
		t.Fatal(err)
	}

	// The wiring under test, first: the door answers, and the halves split.
	locals, ms, ok := rosterSources(home, time.Now())
	if !ok {
		t.Fatal("a live companion is listening and the roster path was not taken")
	}
	if len(locals) != 1 || locals[0].Session != "s_self" {
		t.Fatalf("the door should hand back this machine's record, got %+v", locals)
	}
	if len(ms) != 1 || ms[0].Host != "otherbox" {
		t.Fatalf("the sighting should come back as a member, got %+v", ms)
	}

	// Then the whole listing through it.
	rows, err := ListCached(context.Background(), stillReader{}, home, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var self, far *Agent
	for i := range rows {
		switch rows[i].Name {
		case "self":
			self = &rows[i]
		case "far":
			far = &rows[i]
		}
	}
	if self == nil || far == nil {
		t.Fatalf("both rows must draw, got %+v", rows)
	}
	if self.State != Idle {
		t.Fatalf("the live self is idle — Probe's own dial decides, not the snapshot: %+v", *self)
	}
	if !far.Elsewhere || far.State != Working {
		t.Fatalf("the sighting draws as elsewhere, saying what it was last seen doing: %+v", *far)
	}
}

// With nobody alive there is no door to ask, and the fleet still draws — records and logs
// outlive the processes, which is the fallback's whole point.
func TestListCachedFallsBackWhenNobodyListens(t *testing.T) {
	home := shortHome(t)
	sock := filepath.Join(home, "daemon-gone.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Publish(sock, "/w/gone", "s_gone", daemon.Identity{Name: "gone"}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListCached(context.Background(), stillReader{}, home, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "gone" || rows[0].State != Stopped {
		t.Fatalf("a machine of corpses still deserves its fleet, got %+v", rows)
	}
}
