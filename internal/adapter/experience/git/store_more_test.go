package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestTokenize(t *testing.T) {
	got := tokenize("Use TABS, not 2 spaces! go_lang")
	// lowercased, split on non-alnum, words <3 chars dropped (so "go", "2", "not"? "not" is 3 → kept).
	for _, w := range []string{"use", "tabs", "spaces", "not"} {
		if !got[w] {
			t.Errorf("expected token %q in %v", w, got)
		}
	}
	for _, w := range []string{"go", "2"} { // too short
		if got[w] {
			t.Errorf("short token %q should be dropped", w)
		}
	}
	// "go_lang" splits on '_' into "go"(dropped) + "lang"(kept).
	if !got["lang"] || got["go_lang"] {
		t.Errorf("underscore should split: %v", got)
	}
}

func TestOverlap(t *testing.T) {
	terms := tokenize("tabs indentation")
	if n := overlap(terms, "Always use TABS for INDENTATION here"); n != 2 {
		t.Errorf("overlap = %d, want 2", n)
	}
	if n := overlap(terms, "nothing relevant"); n != 0 {
		t.Errorf("no-match overlap = %d, want 0", n)
	}
	if n := overlap(map[string]bool{}, "anything"); n != 0 {
		t.Errorf("empty terms should score 0, got %d", n)
	}
	// Substring match (not word-boundary): "port" is in "important".
	if n := overlap(tokenize("port"), "this is important"); n != 1 {
		t.Errorf("substring overlap = %d, want 1", n)
	}
}

// topMemories ranks by score desc, drops zero-score entries, caps at n, and is
// stable for ties (input order preserved).
func TestTopMemoriesRanking(t *testing.T) {
	in := []Scored[port.Memory]{
		{Score: 0, V: port.Memory{ID: "zero"}},
		{Score: 2, V: port.Memory{ID: "a"}},
		{Score: 5, V: port.Memory{ID: "b"}},
		{Score: 2, V: port.Memory{ID: "c"}}, // tie with a → a before c (stable)
	}
	got := topMemories(in, 2)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("ranking/cap wrong: %+v", got)
	}
	// All-zero input → no results (nothing relevant).
	if z := topMemories([]Scored[port.Memory]{{Score: 0, V: port.Memory{ID: "x"}}}, 5); len(z) != 0 {
		t.Errorf("zero-score entries must be dropped, got %+v", z)
	}
}

func TestSplitFirstLine(t *testing.T) {
	desc, body := splitFirstLine("a short skill\nline two\nline three")
	if desc != "a short skill" || body != "line two\nline three" {
		t.Errorf("split = %q / %q", desc, body)
	}
	d2, b2 := splitFirstLine("only one line")
	if d2 != "only one line" || b2 != "" {
		t.Errorf("single line split = %q / %q", d2, b2)
	}
}

// sanitize must strip path separators and dots so a skill name can't escape the
// pending directory (path-traversal safety) — only [A-Za-z0-9-_] survive.
func TestSanitizePathSafety(t *testing.T) {
	got := sanitize("../../etc/pass wd.sh")
	if strings.ContainsAny(got, "/.") || strings.Contains(got, " ") {
		t.Errorf("sanitize left unsafe chars: %q", got)
	}
	if sanitize("ok-name_1") != "ok-name_1" {
		t.Errorf("safe chars should be preserved, got %q", sanitize("ok-name_1"))
	}
}

// Retrieve must also surface skills, splitting the first line into the description.
func TestRetrieveSkills(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "skills"), 0o755)
	os.WriteFile(filepath.Join(dir, "skills", "deploy.md"),
		[]byte("how to deploy the service\nstep 1\nstep 2"), 0o644)

	_, skills, err := New(dir).Retrieve(context.Background(), "how do I deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "deploy" || skills[0].Description != "how to deploy the service" || !strings.Contains(skills[0].Body, "step 1") {
		t.Errorf("skill parsed wrong: %+v", skills[0])
	}
}

// Propose writes each memory (with tags + source) into memories/ and each skill
// (under a sanitized, non-escaping filename) into skills/ — the directories Retrieve
// reads, so the entries are immediately recallable.
func TestProposeMemoriesAndSkills(t *testing.T) {
	dir := t.TempDir()
	err := New(dir).Propose(context.Background(), port.Contribution{
		Memories: []port.Memory{
			{Text: "first memory", Tags: []string{"a", "b"}},
			{Text: "second memory"},
		},
		Skills: []port.Skill{{Name: "../evil name", Description: "d", Body: "b"}},
		Source: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	mems, _ := os.ReadDir(filepath.Join(dir, "memories"))
	if len(mems) != 2 {
		t.Fatalf("want 2 memory files, got %d", len(mems))
	}
	skills, _ := os.ReadDir(filepath.Join(dir, "skills"))
	if len(skills) != 1 {
		t.Fatalf("want 1 skill file, got %d", len(skills))
	}

	var sawTagged bool
	for _, e := range mems {
		name := e.Name()
		b, _ := os.ReadFile(filepath.Join(dir, "memories", name))
		if strings.Contains(string(b), "tags: a, b") {
			sawTagged = true
		}
		if !strings.Contains(string(b), "(source: agent)") {
			t.Errorf("memory %q missing source attribution", name)
		}
	}
	if !sawTagged {
		t.Error("expected a tagged memory")
	}

	// The unsafe skill name must be fully sanitized — no separators/dots that could
	// escape skills/.
	sname := skills[0].Name()
	stem := strings.TrimSuffix(strings.TrimPrefix(sname, "skill-"), ".md")
	if strings.ContainsAny(stem, "/.") {
		t.Errorf("skill filename not sanitized (escapable): %q", sname)
	}
	if sname != "skill-"+sanitize("../evil name")+".md" {
		t.Errorf("skill filename = %q, want sanitized form", sname)
	}
}

// sameJudge answers by a hand-built vector per marker word, so dedup can be tested without a model.
type sameJudge struct {
	vec map[string][]float32
}

func (j *sameJudge) Available() bool { return true }
func (j *sameJudge) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{0, 0, 1}
		for k, v := range j.vec {
			if strings.Contains(t, k) {
				out[i] = v
			}
		}
	}
	return out, nil
}

// The same fact in different words is not written twice.
//
// Identity was normalised-string equality — lowercase, collapse whitespace, compare — which every
// rephrasing walks straight past. A resident process meets the same fact for weeks and the store
// fills with eight tellings of it, which is not a growing brain: it is retrieval getting worse at
// its own expense, since those eight now compete for the five slots the budget allows.
func TestARephrasedMemoryIsNotWrittenTwice(t *testing.T) {
	dir := t.TempDir()
	s := New(dir).WithSame(&sameJudge{vec: map[string][]float32{
		"nightly": {1, 0, 0}, // both tellings carry this marker
	}})
	ctx := context.Background()
	if err := s.Propose(ctx, port.Contribution{
		Memories: []port.Memory{{Text: "the invoice job runs nightly and retries twice"}},
	}); err != nil {
		t.Fatal(err)
	}
	// Same fact, different words. Nothing a string comparison could catch.
	if err := s.Propose(ctx, port.Contribution{
		Memories: []port.Memory{{Text: "billing runs once a night, with a nightly retry"}},
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(readDir(filepath.Join(dir, "memories"))); n != 1 {
		t.Errorf("the store holds %d memories; the second telling of one fact should not have landed", n)
	}
}

// A DIFFERENT fact still lands. The threshold is high on purpose: a false merge loses a fact
// permanently and silently, and that is the error worth spending near-duplicates to avoid.
func TestADifferentFactStillLands(t *testing.T) {
	dir := t.TempDir()
	s := New(dir).WithSame(&sameJudge{vec: map[string][]float32{
		"invoice": {1, 0, 0},
		"mascot":  {0, 1, 0},
	}})
	ctx := context.Background()
	s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "the invoice job runs nightly"}}})
	s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "the mascot is a penguin"}}})
	if n := len(readDir(filepath.Join(dir, "memories"))); n != 2 {
		t.Errorf("the store holds %d memories; two unrelated facts must both survive", n)
	}
}

// A skill renamed is the same skill.
//
// A skill's identity is its filename, so "run-go-tests" and "go-test-workflow" were two files for
// one technique — and the writer picks that name freshly each time it learns. Recognised as the
// same, the second arrival becomes the second OBSERVATION of the first: one file, count 2.
func TestARenamedSkillMergesIntoTheOneItAlreadyIs(t *testing.T) {
	dir := t.TempDir()
	s := New(dir).WithSame(&sameJudge{vec: map[string][]float32{
		"go test": {1, 0, 0},
	}})
	ctx := context.Background()
	if err := s.Propose(ctx, port.Contribution{Skills: []port.Skill{{
		Name: "run-go-tests", Description: "how to run go test", Body: "use go test ./...",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Propose(ctx, port.Contribution{Skills: []port.Skill{{
		Name: "go-test-workflow", Description: "how to run go test", Body: "use go test ./... with -count=1",
	}}}); err != nil {
		t.Fatal(err)
	}
	files := readDir(filepath.Join(dir, "skills"))
	if len(files) != 1 {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		t.Fatalf("one technique produced %d skill files: %v", len(files), names)
	}
	h, _ := parseSkill(readFile(files[0]))
	if h.Observed != 2 {
		t.Errorf("the merged skill was observed %d times; the second arrival is evidence, not a new skill", h.Observed)
	}
}

// Without a judge, nothing changes: identity stays textual and the store behaves as it always has.
// The default machine has no embedding model, and that must remain a working configuration rather
// than a degraded one.
func TestWithoutAJudgeIdentityStaysTextual(t *testing.T) {
	dir := t.TempDir()
	s := New(dir) // no WithSame
	ctx := context.Background()
	s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "the invoice job runs nightly"}}})
	s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "the invoice   job runs NIGHTLY"}}}) // same after normalising
	s.Propose(ctx, port.Contribution{Memories: []port.Memory{{Text: "billing runs once a night"}}})      // a rephrasing: lands
	if n := len(readDir(filepath.Join(dir, "memories"))); n != 2 {
		t.Errorf("lexical identity produced %d memories; want 2 (the normalised duplicate dropped, the rephrasing kept)", n)
	}
}
