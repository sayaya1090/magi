package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A check whose source names an existing file by the WRONG PATH looks exactly like a record that
// was never produced: it fails, it keeps failing however good the work is, and the failure drives
// re-planning of a step that has nothing wrong with it. Observed live — `caml/major_gc.c` when the
// file is `ocaml/runtime/major_gc.c`, failing across three worker attempts and 45 minutes.
//
// magi has the filesystem, so a path that does not resolve whose basename resolves at exactly one
// place is a misspelling of that place. Two places is a guess; none is the legitimate case.
func TestCheckSourceRepair(t *testing.T) {
	wd := t.TempDir()
	for _, p := range []string{
		"ocaml/runtime/major_gc.c",
		"src/util.go", "test/util.go", // the ambiguous pair
		".git/objects/major_gc.c", // must never be chosen
	} {
		if err := os.MkdirAll(filepath.Join(wd, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wd, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{states: map[session.SessionID]*sessionState{}}
	s := session.Session{ID: "s1", Workdir: wd}

	in := []council.DeliverableCheck{
		{Step: "1", Deliverable: "fix applied", Source: "caml/major_gc.c", Assert: "matches /caml_fl_sweep/"},
		{Step: "2", Deliverable: "build log", Source: "build.log", Assert: "nonempty"},
		{Step: "3", Deliverable: "helper changed", Source: "lib/util.go", Assert: "matches /func/"},
		{Step: "4", Deliverable: "server up", Source: "", Assert: "port_open 8080"},
		{Step: "5", Deliverable: "already right", Source: "ocaml/runtime/major_gc.c", Assert: "nonempty"},
	}
	out := a.repairCheckSources(context.Background(), s, in)

	if out[0].Source != "ocaml/runtime/major_gc.c" {
		t.Errorf("a unique basename match must be adopted, got %q", out[0].Source)
	}
	// A record the step still has to write: nothing by that name anywhere, so the gate's
	// absent-source failure is the right answer and must not be taken away.
	if out[1].Source != "build.log" {
		t.Errorf("a to-be-produced record must be left alone, got %q", out[1].Source)
	}
	// Two candidates is a guess, and a wrong path silently swapped for another wrong one is worse
	// than the failure it replaces.
	if out[2].Source != "lib/util.go" {
		t.Errorf("an ambiguous basename must be left as authored, got %q", out[2].Source)
	}
	if out[3].Source != "" || out[4].Source != "ocaml/runtime/major_gc.c" {
		t.Errorf("a probe and an already-correct path must be untouched: %q %q", out[3].Source, out[4].Source)
	}
	// The input is never mutated: the caller keeps what the council authored.
	if in[0].Source != "caml/major_gc.c" {
		t.Errorf("the authored checks must not be mutated in place, got %q", in[0].Source)
	}

	// Hidden and dependency trees are not candidates — a check asserting on .git would be nonsense.
	hits := findByBasename(wd, []string{"major_gc.c"})
	for _, h := range hits["major_gc.c"] {
		if strings.HasPrefix(h, ".git/") {
			t.Errorf("the walk must skip version-control internals, got %q", h)
		}
	}
	// The flag restores the pre-repair behavior exactly.
	t.Setenv("MAGI_CHECK_SOURCE_REPAIR", "0")
	if off := a.repairCheckSources(context.Background(), s, in); off[0].Source != "caml/major_gc.c" {
		t.Errorf("MAGI_CHECK_SOURCE_REPAIR=0 must leave every source as authored, got %q", off[0].Source)
	}
}
