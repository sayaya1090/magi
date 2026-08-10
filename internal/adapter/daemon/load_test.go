package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a companion was asked to take on survives the companion.
//
// The published record is removed when a daemon stops, and that is right: it says where to reach
// something that is no longer there. This file is the opposite kind of fact — the week after a
// companion was killed is exactly when somebody asks whether it was overloaded.
func TestWhatACompanionWasAskedToTakeOnOutlivesIt(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-a.sock")
	unpub, err := Publish(sock, "/w/a", "s_a", Identity{Name: "design"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		full  bool
		ahead int
	}{{false, 0}, {false, 1}, {false, 3}, {true, 4}, {true, 4}} {
		if err := NoteLoad(sock, "design", m.full, m.ahead); err != nil {
			t.Fatal(err)
		}
	}
	unpub() // the daemon stops

	got := LoadSince(cfg, time.Now().Add(-time.Hour))
	if len(got) != 1 {
		t.Fatalf("what it was asked to take on went with it: %+v", got)
	}
	p := got[0]
	if p.Name != "design" {
		t.Errorf("the companion it is about is %q", p.Name)
	}
	if p.Taken != 3 || p.Refused != 2 {
		t.Errorf("%d taken and %d turned away", p.Taken, p.Refused)
	}
	// The deepest queue is the difference between a companion that was briefly busy and one that
	// runs at its limit, and no count of arrivals says which.
	if p.Deepest != 4 {
		t.Errorf("the deepest queue reads as %d", p.Deepest)
	}
	if !p.Busy() {
		t.Error("a companion that turned work away reads as keeping up")
	}
}

// A companion that never had anything waiting says nothing.
//
// It is not an empty row: printing one for every companion doing its job is how the ones that are
// not get buried.
func TestACompanionThatKeptUpIsNotReportedAsUnderPressure(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-b.sock")
	for i := 0; i < 5; i++ {
		if err := NoteLoad(sock, "calm", false, 0); err != nil {
			t.Fatal(err)
		}
	}
	got := LoadSince(cfg, time.Now().Add(-time.Hour))
	if len(got) != 1 || got[0].Taken != 5 {
		t.Fatalf("the work it did was lost: %+v", got)
	}
	if got[0].Busy() {
		t.Error("work that started immediately every time reads as pressure")
	}
}

// The window is a window: older moments are read past, not summed in.
func TestOnlyMomentsInsideTheWindowAreCounted(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-c.sock")
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	if err := os.WriteFile(LoadFile(sock), []byte(
		`{"at":"`+old.Format(time.RFC3339Nano)+`","name":"c","full":true,"ahead":4}`+"\n"+
			`{"at":"`+time.Now().UTC().Format(time.RFC3339Nano)+`","name":"c","ahead":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadSince(cfg, time.Now().Add(-7*24*time.Hour))
	if len(got) != 1 {
		t.Fatalf("nothing came back: %+v", got)
	}
	if got[0].Refused != 0 {
		t.Errorf("a refusal from last month counts against this week")
	}
	if got[0].Taken != 1 || got[0].Deepest != 1 {
		t.Errorf("this week reads as %+v", got[0])
	}
}

// Pruning drops what is past the window, and does not touch a file that has nothing past it.
//
// The second half is the one worth asserting: a rewrite per daemon start, of a file nothing has
// aged out of, is a write for nothing — and it is observable, because a rewrite also silently
// drops the half-written last line that an append-only file whose writer was killed always has.
func TestPruningDropsTheOldAndLeavesTheRestAlone(t *testing.T) {
	cfg := shortDir(t)
	sock := filepath.Join(cfg, "daemon-d.sock")
	fresh := `{"at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","name":"d","ahead":2}`
	old := `{"at":"` + time.Now().UTC().Add(-40*24*time.Hour).Format(time.RFC3339Nano) + `","name":"d"}`
	torn := `{"at":"2026-08-0` // what a kill mid-write leaves behind

	if err := os.WriteFile(LoadFile(sock), []byte(fresh+"\n"+torn), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PruneLoad(sock, time.Now().Add(-LoadKept)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(LoadFile(sock))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), torn) {
		t.Error("a file with nothing to drop was rewritten anyway")
	}

	if err := os.WriteFile(LoadFile(sock), []byte(old+"\n"+fresh+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PruneLoad(sock, time.Now().Add(-LoadKept)); err != nil {
		t.Fatal(err)
	}
	got := LoadSince(cfg, time.Time{})
	if len(got) != 1 || got[0].Taken != 1 {
		t.Fatalf("pruning did not leave exactly this week's moment: %+v", got)
	}
	if got[0].Deepest != 2 {
		t.Errorf("the surviving moment lost what it said: %+v", got[0])
	}
}
