package layered

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func seedMem(t *testing.T, dir, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memories", file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Retrieve merges both tiers under one budget and tags each entry with its tier.
func TestRetrieveMergesAndTags(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	seedMem(t, projDir, "p.md", "deploy uses the staging cluster first")
	seedMem(t, globDir, "g.md", "deploy scripts always run gofmt")

	s := New(projDir, "", globDir)
	mems, _, err := s.Retrieve(context.Background(), "how does deploy work?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("want 2 merged memories, got %d: %+v", len(mems), mems)
	}
	// Project entry comes first (most context-specific) and both carry a tier tag.
	if !strings.HasPrefix(mems[0].Text, "[project]") {
		t.Errorf("first entry should be the project tier, got %q", mems[0].Text)
	}
	var sawGlobal bool
	for _, m := range mems {
		if strings.HasPrefix(m.Text, "[global]") {
			sawGlobal = true
		}
	}
	if !sawGlobal {
		t.Errorf("global tier missing from merge: %+v", mems)
	}
}

// Retrieve caps the merged result so adding a tier never widens injected context.
func TestRetrieveCombinedCap(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	for i := 0; i < 6; i++ {
		seedMem(t, projDir, string(rune('a'+i))+".md", "cache invalidation strategy note")
		seedMem(t, globDir, string(rune('a'+i))+".md", "cache invalidation strategy note")
	}
	s := New(projDir, "", globDir)
	mems, _, err := s.Retrieve(context.Background(), "cache invalidation strategy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) > 5 {
		t.Fatalf("combined cap should hold merged memories at 5, got %d", len(mems))
	}
}

// Propose routes by scope, defaulting to the project tier.
func TestProposeScopeRouting(t *testing.T) {
	projDir, globDir := t.TempDir(), t.TempDir()
	s := New(projDir, "", globDir)
	ctx := context.Background()

	if err := s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "project-scoped default"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Propose(ctx, port.Contribution{Scope: "global", Memories: []port.Memory{{Text: "global-scoped"}}}); err != nil {
		t.Fatal(err)
	}
	if n := countMem(projDir); n != 1 {
		t.Errorf("default scope should write to project tier, project has %d files", n)
	}
	if n := countMem(globDir); n != 1 {
		t.Errorf("global scope should write to global tier, global has %d files", n)
	}
}

func countMem(dir string) int {
	entries, _ := os.ReadDir(filepath.Join(dir, "memories"))
	return len(entries)
}

// The team tier is what a workspace and a machine cannot hold between them.
//
// A project tier is one companion's directory and a global tier is every companion on the machine.
// Neither can carry "the frontend team decided X" — the thing somebody wants to write once and have
// three companions follow — so it sits between them, more specific than the machine and less than
// one directory.
func TestTheTeamTierIsItsOwnPlace(t *testing.T) {
	dirs := [3]string{t.TempDir(), t.TempDir(), t.TempDir()}
	s := New(dirs[0], dirs[1], dirs[2])
	ctx := context.Background()

	for i, scope := range []string{"project", "team", "global"} {
		if err := s.Propose(ctx, port.Contribution{
			Memories: []port.Memory{{Text: "a fact for " + scope + " about widgets"}},
			Scope:    scope,
		}); err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		if n := len(readDirNames(t, dirs[i])); n == 0 {
			t.Errorf("%s went somewhere other than its own tier", scope)
		}
	}

	// And all three come back, tagged, so a reader can tell whose knowledge it is.
	mems, _, err := s.Retrieve(ctx, "widgets", nil)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, m := range mems {
		seen = append(seen, m.Text)
	}
	joined := strings.Join(seen, " | ")
	for _, tier := range []string{"[project]", "[team]", "[global]"} {
		if !strings.Contains(joined, tier) {
			t.Errorf("%s is missing from a retrieval that should merge all three: %s", tier, joined)
		}
	}
}

// A companion with no team must not lose what it was asked to remember for one.
//
// It falls to the project rather than to the machine: writing a team decision into the global tier
// would put it in front of every unrelated companion on the box.
func TestRememberingForATeamThatDoesNotExistFallsToTheProject(t *testing.T) {
	proj, glob := t.TempDir(), t.TempDir()
	s := New(proj, "", glob)
	if err := s.Propose(context.Background(), port.Contribution{
		Memories: []port.Memory{{Text: "something for a team this companion is not on"}},
		Scope:    "team",
	}); err != nil {
		t.Fatal(err)
	}
	if len(readDirNames(t, proj)) == 0 {
		t.Error("it was dropped instead of landing in the project tier")
	}
	if len(readDirNames(t, glob)) != 0 {
		t.Error("it landed in the global tier, in front of every companion on the machine")
	}
}

func readDirNames(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && filepath.Ext(p) == ".md" {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// fakeEmbedder answers with hand-built vectors, so the fusion can be tested without a model: the
// point under test is what the store does with a similarity judgement, not the judgement itself.
type fakeEmbedder struct {
	vec  map[string][]float32
	fail error
	saw  int // how many requests were made — the cache claim is about this
}

func (f *fakeEmbedder) Available() bool { return true }
func (f *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.saw++
	if f.fail != nil {
		return nil, f.fail
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		for k, v := range f.vec {
			if strings.Contains(t, k) {
				out[i] = v
			}
		}
		if out[i] == nil {
			out[i] = []float32{0, 0, 1} // orthogonal to everything the test cares about
		}
	}
	return out, nil
}

// A memory that shares NO WORD with the query still reaches the turn, when an embedder says it is
// about the same thing.
//
// This is the case lexical retrieval cannot express, and it is not a corner: every question is
// asked in the asker's vocabulary, not in the words the answer happened to use. Ask about billing
// and the note is filed under invoices, and for magi's whole life before this the answer was that
// there is nothing written down.
func TestAMemoryIsFoundByMeaningWhenNoWordMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, "memories", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("invoice.md", "the invoice job runs nightly and retries twice")
	write("unrelated.md", "the mascot is a penguin")

	s := New(dir, "", "")
	// Lexical only: "billing" is in neither file, so nothing comes back. Stated as the baseline the
	// next assertion is measured against, not as an aside.
	if mems, _, err := s.Retrieve(context.Background(), "billing", nil); err != nil || len(mems) != 0 {
		t.Fatalf("lexically, billing should match nothing here; got %v (%v)", mems, err)
	}

	near := []float32{1, 0, 0}
	s = s.WithEmbedder(&fakeEmbedder{vec: map[string][]float32{
		"invoice": near,
		"billing": near, // the query and the note, judged to be about one thing
	}})
	mems, _, err := s.Retrieve(context.Background(), "billing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) == 0 {
		t.Fatal("the invoice note is still unreachable — a semantic rank that cannot promote a " +
			"zero-scoring document has not changed anything")
	}
	if !strings.Contains(mems[0].Text, "invoice") {
		t.Errorf("the top memory is %q; the note about invoices should lead", mems[0].Text)
	}
}

// An exact token still wins. An embedding is a similarity judgement made by a model and it is
// confidently wrong often enough that a rare identifier — a file name, an error code — must not
// lose to something that merely feels related. The lexical list stays in the fusion for this.
func TestAnExactTokenIsNotLostToSomethingThatFeelsRelated(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "memories"), 0o755)
	os.WriteFile(filepath.Join(dir, "memories", "exact.md"),
		[]byte("heap.go leaks when the pool is drained twice"), 0o644)
	os.WriteFile(filepath.Join(dir, "memories", "vibes.md"),
		[]byte("memory management is subtle and rewards care"), 0o644)

	// The embedder prefers the vague one; the exact token must still surface.
	s := New(dir, "", "").WithEmbedder(&fakeEmbedder{vec: map[string][]float32{
		"heap.go":           {0, 1, 0},
		"memory management": {1, 0, 0},
		"heap.go leaks":     {0, 1, 0},
	}})
	mems, _, err := s.Retrieve(context.Background(), "heap.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if strings.Contains(m.Text, "heap.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("the memory naming heap.go did not survive the fusion: %v", mems)
	}
}

// A broken embedder leaves the lexical answer standing. The turn needs its memories either way,
// and a retrieval that fails outright over a missing sidecar is worse than one that is merely
// deaf to vocabulary.
func TestABrokenEmbedderLeavesTheLexicalAnswer(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "memories"), 0o755)
	os.WriteFile(filepath.Join(dir, "memories", "a.md"), []byte("the invoice job runs nightly"), 0o644)

	f := &fakeEmbedder{fail: errors.New("embedding endpoint refused")}
	s := New(dir, "", "").WithEmbedder(f)
	mems, _, err := s.Retrieve(context.Background(), "invoice", nil)
	if err != nil {
		t.Fatalf("a failed embedding must not fail retrieval: %v", err)
	}
	if len(mems) != 1 || !strings.Contains(mems[0].Text, "invoice") {
		t.Errorf("the lexical answer did not stand: %v", mems)
	}
	if f.saw == 0 {
		t.Error("the embedder was never asked, so this proves nothing about the failure path")
	}
}
