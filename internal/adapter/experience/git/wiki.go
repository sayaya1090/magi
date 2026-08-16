// The wiki half of the store: canonical pages, updated in place, replicated by set-union.
//
// The experience store's memories are append-only lessons; a page here is the CURRENT truth about
// one topic. The unit of storage — and of replication — is an immutable REVISION file:
//
//	<dir>/wiki/revisions/<slug>/<seq>-<editor>-<hash>.md   (frontmatter + full body snapshot)
//	<dir>/wiki/pages/<slug>.md                              (the winning revision, for humans/git)
//
// A revision is content-addressed and never rewritten, so syncing two replicas is a set union
// with no conflicts at the transport layer; "current" is a deterministic function of the set
// (highest seq, then newest ts, then filename), so every replica converges to the same page
// without coordination. Two concurrent edits both survive — the loser stays in the revision log
// and the winner's page carries a note, which is a merge a later editor can actually perform,
// unlike a lost write nobody knows happened.
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/port"
)

// wikiWrite lands one edit as a new revision and refreshes the page cache. Editor comes from the
// contribution's Source — the same provenance line memories carry.
func (s *Store) wikiWrite(ctx context.Context, e port.WikiEdit, editor string) error {
	title := strings.TrimSpace(e.Page)
	if title == "" {
		return fmt.Errorf("wiki: a page needs a title")
	}
	body := strings.TrimSpace(e.Text)
	if body == "" {
		return fmt.Errorf("wiki: page %q: an empty body erases nothing and says nothing — to retire a page, write WHY it is stale instead", title)
	}
	slug := sanitize(title)
	revDir := filepath.Join(s.dir, "wiki", "revisions", slug)
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		return err
	}
	revs := readWikiRevisions(revDir)
	seq := 1
	if len(revs) > 0 {
		seq = revs[0].seq + 1
	}
	summary := strings.TrimSpace(e.Summary)
	if summary == "" {
		summary = firstLine(body) // tolerated, not refused: a refused write teaches a model to stop writing
	}
	// Nanosecond timestamps: two edits from two machines land within the same second routinely,
	// and a seconds-granular tie falls through to the filename — deterministic, but "the later
	// edit wins" should hold whenever the clocks can actually tell the edits apart.
	rev := wikiRevision{
		Title: title, Editor: editor, TS: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: summary, Links: e.Links, Body: body, seq: seq,
	}
	name := fmt.Sprintf("%04d-%s-%s.md", seq, sanitize(nonEmpty(editor, "unknown")), memoryID(body))
	if err := atomicfile.Write(filepath.Join(revDir, name), []byte(renderWikiRevision(rev)), 0o644); err != nil {
		return err
	}
	s.refreshWikiPage(slug)
	return nil
}

// refreshWikiPage rewrites the human-facing page cache from the revision set. Best-effort: the
// revisions are the source of truth and every reader derives from them, so a failed cache write
// loses nothing but git-diff convenience.
func (s *Store) refreshWikiPage(slug string) {
	revs := readWikiRevisions(filepath.Join(s.dir, "wiki", "revisions", slug))
	if len(revs) == 0 {
		return
	}
	cur := revs[0]
	pageDir := filepath.Join(s.dir, "wiki", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		return
	}
	_ = atomicfile.Write(filepath.Join(pageDir, slug+".md"), []byte(renderWikiRevision(cur)), 0o644)
}

// WikiSearch implements port.WikiStore: up to n current pages matching the query, best first. An
// exact (case-insensitive) title match outranks everything — that is what makes a title query
// behave as a page fetch — and title-word hits weigh triple a body hit.
func (s *Store) WikiSearch(ctx context.Context, query string, n int) ([]port.WikiPage, error) {
	terms := tokenize(query)
	q := strings.ToLower(strings.TrimSpace(query))
	type scoredPage struct {
		score int
		p     port.WikiPage
	}
	var out []scoredPage
	for _, p := range s.wikiPages() {
		score := 3*overlap(terms, p.Title) + overlap(terms, p.Body)
		if strings.EqualFold(strings.TrimSpace(p.Title), q) {
			score += 1000
		}
		if score == 0 {
			continue
		}
		// A stale page is DEMOTED, never hidden: asked about its topic, "this stopped being true,
		// and here is why" is the answer — silently dropping it would re-teach the stale fact from
		// wherever the asker heard it first.
		if p.Stale {
			score /= 4
		}
		out = append(out, scoredPage{score, p})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	var pages []port.WikiPage
	for _, sp := range out {
		if len(pages) >= n {
			break
		}
		pages = append(pages, sp.p)
	}
	return pages, nil
}

// WikiIndex implements port.WikiStore: titles and hooks, most recently edited first. Stale pages
// are left out — the index is an advertisement, and a retired page has nothing to advertise
// (search still surfaces it, demoted and marked, for anyone who asks about its topic).
func (s *Store) WikiIndex(ctx context.Context, n int) ([]port.WikiPage, error) {
	all := s.wikiPages()
	pages := all[:0]
	for _, p := range all {
		if !p.Stale {
			pages = append(pages, p)
		}
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Updated > pages[j].Updated })
	if len(pages) > n {
		pages = pages[:n]
	}
	// The index is the advertisement, not the content: keep the hook to one line.
	for i := range pages {
		pages[i].Body = firstLine(pages[i].Body)
	}
	return pages, nil
}

// WikiList returns every current page INCLUDING the stale ones, newest edit first — the
// governance view's read, where a tombstone is exactly the thing a person wants to see.
func (s *Store) WikiList(ctx context.Context) []port.WikiPage {
	pages := s.wikiPages()
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Updated > pages[j].Updated })
	return pages
}

// wikiPages derives every current page from its revision set.
func (s *Store) wikiPages() []port.WikiPage {
	root := filepath.Join(s.dir, "wiki", "revisions")
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []port.WikiPage
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		revs := readWikiRevisions(filepath.Join(root, e.Name()))
		if len(revs) == 0 {
			continue
		}
		cur := revs[0]
		out = append(out, port.WikiPage{
			Title: cur.Title, Body: cur.Body, Links: cur.Links, Stale: cur.Stale,
			Updated: cur.TS, Editor: cur.Editor, Summary: cur.Summary,
		})
	}
	return out
}

// ---- revision files ----

type wikiRevision struct {
	Title   string
	Editor  string
	TS      string
	Summary string
	Links   []string
	Stale   bool
	Body    string
	seq     int
	file    string
}

func renderWikiRevision(r wikiRevision) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + r.Title + "\n")
	b.WriteString("editor: " + r.Editor + "\n")
	b.WriteString("ts: " + r.TS + "\n")
	b.WriteString("summary: " + r.Summary + "\n")
	if len(r.Links) > 0 {
		b.WriteString("links: [" + strings.Join(r.Links, ", ") + "]\n")
	}
	if r.Stale {
		b.WriteString("stale: true\n")
	}
	b.WriteString("---\n")
	b.WriteString(r.Body)
	b.WriteString("\n")
	return b.String()
}

// readWikiRevisions returns a directory's revisions, WINNER FIRST: highest seq, then newest ts,
// then filename — a total order every replica computes identically, whatever order sync delivered
// the files in.
func readWikiRevisions(dir string) []wikiRevision {
	var out []wikiRevision
	for _, f := range readDir(dir) {
		text := readFile(f)
		if text == "" {
			continue
		}
		r := parseWikiRevision(text)
		base := filepath.Base(f)
		if i := strings.IndexByte(base, '-'); i > 0 {
			if n, err := strconv.Atoi(base[:i]); err == nil {
				r.seq = n
			}
		}
		r.file = base
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		if out[i].TS != out[j].TS {
			return out[i].TS > out[j].TS
		}
		return out[i].file > out[j].file
	})
	return out
}

func parseWikiRevision(text string) wikiRevision {
	var r wikiRevision
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if head, tail, ok := strings.Cut(rest, "\n---\n"); ok {
			body = tail
			for _, line := range strings.Split(head, "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "title":
					r.Title = v
				case "editor":
					r.Editor = v
				case "ts":
					r.TS = v
				case "summary":
					r.Summary = v
				case "links":
					v = strings.Trim(v, "[]")
					for _, l := range strings.Split(v, ",") {
						if l = strings.TrimSpace(l); l != "" {
							r.Links = append(r.Links, l)
						}
					}
				case "stale":
					r.Stale = v == "true"
				}
			}
		}
	}
	r.Body = strings.TrimSpace(body)
	return r
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
