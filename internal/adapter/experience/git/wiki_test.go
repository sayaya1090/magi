package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func wikiWriteT(t *testing.T, s *Store, page, text, summary, editor string) {
	t.Helper()
	if err := s.Propose(context.Background(), port.Contribution{
		Wiki:   []port.WikiEdit{{Page: page, Text: text, Summary: summary}},
		Source: editor,
	}); err != nil {
		t.Fatal(err)
	}
}

// A page is the CURRENT truth about its topic: writing it again replaces what a reader gets, and
// the older claim survives only as a revision — corrected, not contradicted beside itself.
func TestAPageUpdatesInPlace(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	wikiWriteT(t, s, "auth flow", "refresh is done by the gateway", "first mapping", "melchior")
	wikiWriteT(t, s, "auth flow", "refresh is done by the SIDECAR, not the gateway", "corrected owner", "casper")

	pages, err := s.WikiSearch(ctx, "auth flow", 3)
	if err != nil || len(pages) == 0 {
		t.Fatalf("search: %v %d", err, len(pages))
	}
	p := pages[0]
	if !strings.Contains(p.Body, "SIDECAR") || strings.Contains(p.Body, "refresh is done by the gateway") {
		t.Errorf("the current page must be the correction alone:\n%s", p.Body)
	}
	if p.Editor != "casper" || p.Summary != "corrected owner" {
		t.Errorf("the page must carry its last editor and summary, got %q %q", p.Editor, p.Summary)
	}
	// Both claims remain in the revision log — history is the record, the page is the truth.
	revDir := filepath.Join(s.dir, "wiki", "revisions", sanitize("auth flow"))
	if revs := readWikiRevisions(revDir); len(revs) != 2 {
		t.Errorf("want 2 revisions kept, got %d", len(revs))
	}
}

// An exact title query behaves as a page fetch: it outranks a page that merely mentions the words.
func TestATitleQueryFetchesThePage(t *testing.T) {
	s := New(t.TempDir())
	wikiWriteT(t, s, "deploy pipeline", "three stages, fan-in at the end", "", "m")
	wikiWriteT(t, s, "service map", "the deploy pipeline feeds the service map hourly with deploy pipeline events", "", "m")

	pages, err := s.WikiSearch(context.Background(), "deploy pipeline", 2)
	if err != nil || len(pages) == 0 {
		t.Fatal(err)
	}
	if pages[0].Title != "deploy pipeline" {
		t.Errorf("exact title must win, got %q first", pages[0].Title)
	}
}

// Replication is a set union: hand replica B's revision files to replica A and both compute the
// same current page, whatever order the files arrived in. This is the property the door sync
// stands on, so it is proven at the store, not assumed at the transport.
func TestReplicasConvergeByFileUnion(t *testing.T) {
	a, b := New(t.TempDir()), New(t.TempDir())
	wikiWriteT(t, a, "ports", "8080 is the api", "", "melchior")
	wikiWriteT(t, b, "ports", "8080 is the api; 9090 is metrics", "added metrics", "casper")

	// Union BOTH ways — the sync is a bidirectional exchange, and convergence is a property of
	// equal sets, not of one side's donation. Copy order does not matter: the names are
	// content-addressed and the winner is a pure function of the set.
	slug := sanitize("ports")
	dirA := filepath.Join(a.dir, "wiki", "revisions", slug)
	dirB := filepath.Join(b.dir, "wiki", "revisions", slug)
	for _, pair := range [][2]string{{dirB, dirA}, {dirA, dirB}} {
		for _, f := range readDir(pair[0]) {
			data, _ := os.ReadFile(f)
			if err := os.WriteFile(filepath.Join(pair[1], filepath.Base(f)), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	pa, _ := a.WikiSearch(context.Background(), "ports", 1)
	pb, _ := b.WikiSearch(context.Background(), "ports", 1)
	if len(pa) == 0 || len(pb) == 0 {
		t.Fatal("both replicas must answer")
	}
	// Same revision set → same winner on both sides (seq tie broken by ts, then filename).
	if pa[0].Body != pb[0].Body || pa[0].Editor != pb[0].Editor {
		t.Errorf("replicas diverged:\nA: %q by %s\nB: %q by %s", pa[0].Body, pa[0].Editor, pb[0].Body, pb[0].Editor)
	}
}

// Stale is a tombstone, not a deletion: the index stops advertising the page, search demotes it
// but still answers a direct ask — and the body stays, because "no longer so, and here is why"
// is knowledge too.
func TestStaleIsDemotedNotErased(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	wikiWriteT(t, s, "legacy queue", "jobs ride rabbitmq", "", "m")
	// Retire it the way an agent does: a stale revision with the reason as the body.
	slug := sanitize("legacy queue")
	revDir := filepath.Join(s.dir, "wiki", "revisions", slug)
	revs := readWikiRevisions(revDir)
	stale := wikiRevision{Title: "legacy queue", Editor: "gardener", TS: "2099-01-01T00:00:00Z",
		Summary: "replaced by kafka", Stale: true, Body: "no longer true: jobs moved to kafka (see [[event bus]])", seq: revs[0].seq + 1}
	if err := os.WriteFile(filepath.Join(revDir, "0002-gardener-x.md"), []byte(renderWikiRevision(stale)), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, _ := s.WikiIndex(ctx, 10)
	for _, p := range idx {
		if p.Title == "legacy queue" {
			t.Error("a stale page must not be advertised in the index")
		}
	}
	hits, _ := s.WikiSearch(ctx, "legacy queue", 2)
	if len(hits) == 0 || !hits[0].Stale || !strings.Contains(hits[0].Body, "kafka") {
		t.Errorf("a direct ask must still answer with the stale page and its reason: %+v", hits)
	}
}

// The index is an advertisement: titles and one-line hooks, never bodies.
func TestTheIndexCarriesHooksNotBodies(t *testing.T) {
	s := New(t.TempDir())
	wikiWriteT(t, s, "service map", "gateway fronts everything\nand twenty more lines of detail", "", "m")
	idx, err := s.WikiIndex(context.Background(), 5)
	if err != nil || len(idx) != 1 {
		t.Fatalf("%v %d", err, len(idx))
	}
	if strings.Contains(idx[0].Body, "twenty more") {
		t.Errorf("the index must clip to the hook line, got %q", idx[0].Body)
	}
}

// An empty body is refused with the alternative named: retiring a page is a stale revision that
// says WHY, never a blank page.
func TestAnEmptyBodyIsRefused(t *testing.T) {
	s := New(t.TempDir())
	err := s.Propose(context.Background(), port.Contribution{
		Wiki: []port.WikiEdit{{Page: "ports", Text: "   "}}, Source: "m"})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("an empty body must be refused, pointing at the stale path: %v", err)
	}
}
