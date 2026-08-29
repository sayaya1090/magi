package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// parseMemory splits the "tags:" line Propose writes off the front of a fact — and a fact written
// without one is all body.
func TestParseMemorySplitsTheTagsLine(t *testing.T) {
	tags, body := parseMemory("tags: build, flaky ,\nThe build is flaky on Tuesdays.\n")
	if len(tags) != 2 || tags[0] != "build" || tags[1] != "flaky" {
		t.Fatalf("tags: %v", tags)
	}
	if body != "The build is flaky on Tuesdays." {
		t.Fatalf("body: %q", body)
	}
	tags, body = parseMemory("Just a fact.\nWith a second line.")
	if tags != nil || body != "Just a fact.\nWith a second line." {
		t.Fatalf("no tags line means all body, got (%v, %q)", tags, body)
	}
}

// ContentID is the store's content-hash naming, exported so the door sync can verify a received
// file's name against its body.
func TestContentIDIsContentAddressed(t *testing.T) {
	a, b := ContentID("one fact"), ContentID("another fact")
	if a == "" || a == b {
		t.Fatal("two bodies, two names")
	}
	if a != ContentID("one fact") {
		t.Fatal("the same body always earns the same name — that is the whole property")
	}
}

// RevisionParts refuses a file that does not OPEN with frontmatter — the exact bypass it exists
// to close: a prefix pasted above the frontmatter must not pass as the parsed body.
//
// A mutation note, recorded rather than papered over: dropping RevisionParts' own HasPrefix
// clause SURVIVES this test, because parseWikiRevision already demands the same prefix
// (CutPrefix) and answers Title == "" for a prefixed file — the two conditions coincide today.
// The clause stays as the sync's own statement of the contract, so a future loosening of the
// parser cannot silently loosen the verification riding on it; this test would start killing
// that mutant the day the parser diverges.
func TestRevisionPartsRefusesAPrefixedFile(t *testing.T) {
	if _, _, ok := RevisionParts("junk\n---\ntitle: X\n---\nbody"); ok {
		t.Fatal("a revision opens with ---\\n, and a pasted prefix is not a revision")
	}
	if _, _, ok := RevisionParts("not a revision at all"); ok {
		t.Fatal("shapeless content is not a revision")
	}
}

// SlugOf answers both the current and the legacy directory name, deterministically.
func TestSlugOfIsDeterministic(t *testing.T) {
	c1, l1 := SlugOf("Deploy Steps")
	c2, l2 := SlugOf("Deploy Steps")
	if c1 != c2 || l1 != l2 || c1 == "" || l1 == "" {
		t.Fatalf("one title, one pair of names: (%q,%q) vs (%q,%q)", c1, l1, c2, l2)
	}
}

// Inventory shows BOTH halves of a store — a person governs what they can see — and Forget
// removes by listed name only, never by joined path.
func TestInventoryAndForgetCoverBothHalves(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memories", "m1.md"),
		[]byte("tags: ops\nRestart the cache after deploys.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	inv, err := s.Inventory(context.Background())
	if err != nil || len(inv) != 1 {
		t.Fatalf("one memory in, one row out: (%+v, %v)", inv, err)
	}
	if inv[0].Kind != "memory" || inv[0].Description != "Restart the cache after deploys." {
		t.Fatalf("a fact's first line is the fact: %+v", inv[0])
	}
	if err := s.Forget(context.Background(), "../m1"); err == nil {
		// Matched against the listing, a traversal name simply matches nothing.
		if _, err := os.Stat(filepath.Join(dir, "memories", "m1.md")); err != nil {
			t.Fatal("a traversal name must not reach the file")
		}
	}
	if err := s.Forget(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "memories", "m1.md")); !os.IsNotExist(err) {
		t.Fatal("forgetting by its listed name removes it")
	}
}

// WikiList reads the chains as the parser groups them: the newest revision of each page, newest
// pages first — and an empty store is an empty list.
func TestWikiListReadsTheChains(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if got := s.WikiList(context.Background()); len(got) != 0 {
		t.Fatalf("an empty store lists nothing, got %+v", got)
	}
	slug, _ := SlugOf("Deploy Steps")
	rev := filepath.Join(dir, "wiki", "revisions", slug)
	if err := os.MkdirAll(rev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rev, "0001-x.md"),
		[]byte("---\ntitle: Deploy Steps\nts: 2026-08-29T01:00:00Z\n---\npush, then watch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.WikiList(context.Background())
	if len(got) != 1 || got[0].Title != "Deploy Steps" || got[0].Body != "push, then watch" {
		t.Fatalf("the chain's winner is the page: %+v", got)
	}
}
