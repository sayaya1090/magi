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

// Two Korean titles must not share a chain: sanitize collapses every non-ASCII rune to '-', so
// without the hash suffix "인증 흐름" and "배포 절차" were ONE page and the winner hid a topic.
func TestLossyTitlesGetDistinctChains(t *testing.T) {
	s := New(t.TempDir())
	wikiWriteT(t, s, "인증 흐름", "사이드카가 갱신한다", "", "m")
	wikiWriteT(t, s, "배포 절차", "3단계 파이프라인", "", "m")
	ctx := context.Background()
	a, _ := s.WikiSearch(ctx, "인증 흐름", 1)
	b, _ := s.WikiSearch(ctx, "배포 절차", 1)
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("both pages must exist: %d %d", len(a), len(b))
	}
	if a[0].Title == b[0].Title || a[0].Body == b[0].Body {
		t.Fatalf("two lossy titles merged into one chain: %+v / %+v", a[0], b[0])
	}
	if wikiSlug("인증 흐름") == wikiSlug("배포 절차") {
		t.Fatal("slugs collide")
	}
}

// Forgetting: a page old and recalled by nobody drops out of the INDEX — quiet retirement, no
// truth claim. A touch (a real recall) re-advertises it; search never stops finding it.
func TestAnOldUntouchedPageRetiresFromTheIndex(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	// An old page, crafted with an old timestamp the way the stale test does.
	slug := wikiSlug("legacy map")
	revDir := filepath.Join(s.dir, "wiki", "revisions", slug)
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := wikiRevision{Title: "legacy map", Editor: "m", TS: "2026-01-01T00:00:00Z",
		Summary: "s", Body: "the old routing table", seq: 1}
	if err := os.WriteFile(filepath.Join(revDir, "0001-m-x.md"), []byte(renderWikiRevision(old)), 0o644); err != nil {
		t.Fatal(err)
	}
	wikiWriteT(t, s, "fresh page", "written today", "", "m")

	idx, _ := s.WikiIndex(ctx, 10)
	for _, p := range idx {
		if p.Title == "legacy map" {
			t.Error("an old, never-recalled page must retire from the index")
		}
	}
	if hits, _ := s.WikiSearch(ctx, "legacy map", 1); len(hits) == 0 {
		t.Error("retirement is index-only — search must still find the page")
	}
	// One real recall re-advertises it.
	s.WikiTouch([]string{"legacy map"})
	idx, _ = s.WikiIndex(ctx, 10)
	found := false
	for _, p := range idx {
		if p.Title == "legacy map" {
			found = true
		}
	}
	if !found {
		t.Error("a touched page must be advertised again")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "wiki", ".usage")); err != nil {
		t.Error("the touch must persist to wiki/.usage")
	}
}

// A [[wikilink]] written in the body IS a link: extracted, merged with the explicit list, and
// deduplicated — a model that names a related page mid-sentence should not have to repeat it in
// a field for the graph to see it.
func TestBodyWikilinksBecomeLinks(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Propose(context.Background(), port.Contribution{Source: "m",
		Wiki: []port.WikiEdit{{Page: "auth flow", Links: []string{"service map"},
			Text: "the sidecar refreshes tokens; see [[deploy pipeline]] and [[Service Map]] for the rest"}}}); err != nil {
		t.Fatal(err)
	}
	pages, _ := s.WikiSearch(context.Background(), "auth flow", 1)
	if len(pages) == 0 {
		t.Fatal("page missing")
	}
	got := strings.Join(pages[0].Links, "|")
	if !strings.Contains(got, "service map") || !strings.Contains(got, "deploy pipeline") {
		t.Fatalf("links = %v", pages[0].Links)
	}
	if strings.Count(strings.ToLower(got), "service map") != 1 {
		t.Fatalf("case-folded duplicate not merged: %v", pages[0].Links)
	}
}

// The stale tombstone is WRITABLE through the ordinary edit: remember{page, stale:true} lands a
// stale revision whose body is the reason — the retirement path the refusal message advertises
// must actually exist (it did not, and the whole feature was dead code behind a missing field).
func TestStaleIsWritableThroughAnEdit(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	wikiWriteT(t, s, "legacy queue", "jobs ride rabbitmq", "", "m")
	if err := s.Propose(ctx, port.Contribution{Source: "casper", Wiki: []port.WikiEdit{{
		Page: "legacy queue", Text: "no longer true: jobs moved to [[event bus]]", Stale: true,
		Summary: "replaced by kafka"}}}); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.WikiSearch(ctx, "legacy queue", 1)
	if len(hits) == 0 || !hits[0].Stale || !strings.Contains(hits[0].Body, "event bus") {
		t.Fatalf("the stale edit must become the current page, marked: %+v", hits)
	}
	idx, _ := s.WikiIndex(ctx, 10)
	for _, p := range idx {
		if p.Title == "legacy queue" {
			t.Error("a freshly-retired page must leave the index")
		}
	}
}

// Frontmatter values are folded to one line: a newline smuggled through the summary (or title, or
// editor) must not forge a later ts, spoof an editor, or retire the page.
func TestFrontmatterNewlinesAreFolded(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	if err := s.Propose(ctx, port.Contribution{Source: "mallory\neditor: alice", Wiki: []port.WikiEdit{{
		Page: "po\nrts", Text: "the real body", Summary: "fixed\nstale: true\nts: 9999-01-01T00:00:00Z"}}}); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.WikiSearch(ctx, "po rts", 1)
	if len(hits) == 0 {
		t.Fatal("page missing")
	}
	p := hits[0]
	if p.Stale {
		t.Error("a smuggled stale: line retired the page")
	}
	if strings.Contains(p.Editor, "alice") && !strings.Contains(p.Editor, "mallory") {
		t.Errorf("editor spoofed: %q", p.Editor)
	}
	if strings.HasPrefix(p.Updated, "9999") {
		t.Errorf("ts forged: %q", p.Updated)
	}
}

// Case and spacing variants of a title are ONE topic and one chain — and, critically, one
// directory name on every filesystem, or a case-insensitive APFS replica and a Linux replica
// disagree about the tree itself after a sync.
func TestTitleCaseVariantsShareOneChain(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	wikiWriteT(t, s, "Auth Flow", "first", "", "m")
	wikiWriteT(t, s, "auth flow", "second — the correction", "", "c")
	if wikiSlug("Auth Flow") != wikiSlug("auth flow") {
		t.Fatal("case variants produced two slugs")
	}
	hits, _ := s.WikiSearch(ctx, "auth flow", 2)
	if len(hits) != 1 || !strings.Contains(hits[0].Body, "second") {
		t.Fatalf("variants must converge to one page with the later edit current: %+v", hits)
	}
}
