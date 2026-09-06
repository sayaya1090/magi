package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The request prefix has exactly one door per piece: stepSystemFor for the system prompt,
// sessionToolSpecs for the tool catalog (prompt_frozen.go). Go cannot hide the raw builders
// inside one package, so the property is held the way the arch ratchets hold theirs — a test
// that reads the source and names the offender. A new call site reaching for the raw builder
// compiles, and then fails here with the file and the reason.
func TestPrefixBuildersHaveOneDoor(t *testing.T) {
	allowed := map[string]map[string]bool{
		// raw builder -> files that may name it (its own definition, the door, and their tests)
		"a.buildStepSystem(": {"prompt_frozen.go": true, "loop.go": true},
		// context_state.go MEASURES the catalog for a reading (assembledParts) and sends nothing.
		// It must not take the door: the door freezes, and a reading can come before the tools a
		// session will carry have attached — a pane polling its meter would have pinned a
		// catalog without the document tools (2026-09-06).
		"a.toolSpecs(": {"prompt_frozen.go": true, "prompt.go": true, "context_state.go": true},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		for needle, files := range allowed {
			if !strings.Contains(string(src), needle) || files[name] {
				continue
			}
			t.Errorf("%s calls %s directly — the prefix is frozen behind one door "+
				"(stepSystemFor / sessionToolSpecs, prompt_frozen.go); a bypass re-bills the whole "+
				"conversation the moment its output drifts", name, strings.TrimSuffix(needle, "("))
		}
	}
	// loop.go may name buildStepSystem only inside stepSystemFor's freeze window — it does not,
	// but the definition lives near its old call site; pin the actual call count there instead.
	src, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "a.buildStepSystem("); n > 0 {
		t.Errorf("loop.go calls a.buildStepSystem directly %d time(s) — it must go through stepSystemFor", n)
	}
}

// And the freeze itself, behaviourally: same bytes within a turn however the inputs move, new
// bytes only after resetTurnPrompt; the catalog never moves within a session.
func TestStepSystemIsFrozenWithinATurn(t *testing.T) {
	work := t.TempDir()
	a := &App{states: map[session.SessionID]*sessionState{}}
	first := a.stepSystemFor("s1", AgentSpec{}, work, nil)

	// A skill landing mid-turn moves neither the skill head (frozen) nor the turn system.
	sk := filepath.Join(work, ".magi", "skills")
	if err := os.MkdirAll(sk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sk, "late.md"), []byte("arrives late\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again := a.stepSystemFor("s1", AgentSpec{}, work, nil); again != first {
		t.Fatalf("the turn's system prompt moved mid-turn:\n was %q\n now %q", first, again)
	}

	a.resetTurnPrompt("s1")
	next := a.stepSystemFor("s1", AgentSpec{}, work, nil)
	// The skill head is frozen for the SESSION, not the turn — so the new turn's head is
	// byte-identical too, and the late skill reaches the model as an announcement instead.
	if next != first {
		t.Fatalf("nothing but the skill changed, so the next turn's head should be byte-identical:\n was %q\n now %q", first, next)
	}
	if got := a.skillArrivals("s1", work); len(got) != 1 || !strings.Contains(got[0], "late") {
		t.Fatalf("the late skill should surface as an arrival, got %v", got)
	}
}

func TestToolCatalogIsFrozenForTheSession(t *testing.T) {
	reg := builtin.NewRegistry()
	reg.Register(metaTool{name: "zz-late-read"})
	a := &App{states: map[session.SessionID]*sessionState{}, tools: reg}
	first := a.sessionToolSpecs("s1", AgentSpec{})
	if len(first) == 0 {
		t.Fatal("the opening catalog should not be empty")
	}
	n := len(first)
	reg.Register(metaTool{name: "zz-later-write"}) // a plugin hot-reload mid-session
	if again := a.sessionToolSpecs("s1", AgentSpec{}); len(again) != n {
		t.Fatalf("the catalog moved mid-session — that is the prefix re-billed and a tool the "+
			"transcript never introduced: %d -> %d", n, len(again))
	}
	if fresh := a.sessionToolSpecs("s2", AgentSpec{}); len(fresh) != n+1 {
		t.Fatalf("the next session opens with the catalog as it now is: %d want %d", len(fresh), n+1)
	}
}
