package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// killedLLM fails every request outright, so every spawn attempt errors and the spawn exhausts its
// restarts — the shape a lease-KILL or a repeated stall leaves behind.
type killedLLM struct{}

func (killedLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	return nil, errors.New("subagent lease expired (judge: KILL)")
}

// TestSpawnExhaustionKeepsTheLastSessionID: an exhausted spawn used to return a bare error, dropping
// the id of the session its last attempt ran in. That id is magi's own record of everything the
// child searched and read, and it is the only thing a salvaging caller can ask about — dropping it
// made the record unreachable at exactly the moment it was all that was left.
func TestSpawnExhaustionKeepsTheLastSessionID(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_JUDGE", "off")
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// deadLLM hangs, so each attempt dies on its per-attempt deadline — the retriable failure that
	// actually reaches the exhaustion return (a provider error is terminal and returns its own result).
	a := New(store, deadLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       10 * time.Second,
		SubagentTimeout:     time.Second,
		SubagentMaxRestarts: 1,
	})
	r := a.spawn(context.Background(), parentSession(t.TempDir()), 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if r.Err == "" {
		t.Fatal("precondition: every attempt must have failed")
	}
	if !strings.Contains(r.Err, "failed after 2 attempts") {
		t.Errorf("the error must still say how many attempts were spent: %q", r.Err)
	}
	if r.SessionID == "" {
		t.Fatal("an exhausted spawn must carry its LAST attempt's session id — it is the only handle on what the child did")
	}
	if _, err := a.store.Read(context.Background(), r.SessionID, 0); err != nil {
		t.Errorf("the carried id must name a readable session: %v", err)
	}
}

// TestErroredExplorationSalvagesItsSearches: a stopped exploration and an errored one are the same
// thing to the caller — no findings — and the searches both ran are salvageable for the same reason.
// They got opposite treatment only because a guard stop leaves Err empty while an expired lease sets
// it, so the salvage sat behind a door the error case never opened.
func TestErroredExplorationSalvagesItsSearches(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_JUDGE", "off")
	t.Setenv("MAGI_SPECMINE_CONFIRM", "0") // the confirming search is its own pass; not under test here
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, killedLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		SubagentMaxRestarts: 0,
	})
	wd := t.TempDir()
	// repoMap must report something, or the exploration is skipped as greenfield before it spawns.
	if err := os.WriteFile(filepath.Join(wd, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := session.Session{ID: "s_parent", Workdir: wd, Agent: "default", Model: session.ModelRef{Model: "m"}}
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()
	w := watchProgress(t, a, s.ID)

	a.exploreSpecMine(context.Background(), s, "compress the free space", []planStep{{Title: "sweep", Task: "rewrite the sweep"}}, 0)

	notes := w.notes("specmine")
	if !strings.Contains(notes, "the exploration never reported") {
		t.Fatalf("an errored exploration must reach the salvage, not return silently:\n%s", notes)
	}
	if !strings.Contains(notes, "searched-and-not-found") {
		t.Errorf("the salvage must say what it kept from magi's own record:\n%s", notes)
	}
}

// TestSalvageInjectsWhatTheChildEstablished: the salvage's product is the searched-and-not-found
// note, and it has to land in the mined contract every later reader shares — the planner, the
// check-author, the termination council. A salvage that only logs changes nothing.
func TestSalvageInjectsWhatTheChildEstablished(t *testing.T) {
	t.Setenv("MAGI_SPECMINE_CONFIRM", "0")
	a := newTestApp(t)
	ctx := context.Background()
	sid := startSession(t, a, t.TempDir())
	child := startSession(t, a, t.TempDir())
	for _, e := range greppedEvents("c1", "caml_fl_sweep", "", "", nil, false) {
		e.SessionID = child
		if _, err := a.store.Append(ctx, child, e); err != nil {
			t.Fatal(err)
		}
	}
	s := session.Session{ID: sid, Workdir: t.TempDir()}

	a.salvageSearches(ctx, s, 0, AgentSpec{Name: "specmine"},
		[]planStep{{Title: "sweep", Task: "call caml_fl_sweep"}}, child, "the exploration never reported (lease)")

	got := a.cachedSpecMine(sid)
	if !strings.Contains(got, "caml_fl_sweep") {
		t.Fatalf("the absence the child established must reach the mined contract:\n%s", got)
	}
	// A child that established nothing must inject nothing — salvage is not a place to invent one.
	empty := startSession(t, a, t.TempDir())
	before := a.cachedSpecMine(sid)
	a.salvageSearches(ctx, s, 0, AgentSpec{Name: "specmine"}, nil, empty, "nothing to keep")
	if a.cachedSpecMine(sid) != before {
		t.Error("a child with no recorded searches must add nothing to the contract")
	}
}
