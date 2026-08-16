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
// without coordination. Two concurrent edits both survive: the loser stays in the revision log,
// where a later editor — or the gardener pass — can still merge what it said, unlike a lost
// write nobody knows happened. (Surfacing "this page has a concurrent loser" on the winning page
// itself is the gardener's job, not the store's.)
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
	"unicode"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/port"
)

// wikiWrite lands one edit as a new revision and refreshes the page cache. Editor comes from the
// contribution's Source — the same provenance line memories carry.
func (s *Store) wikiWrite(ctx context.Context, e port.WikiEdit, editor string) error {
	// Frontmatter is line-oriented, so every value that lands in it is folded to ONE line here —
	// a summary of "fixed\nstale: true" must not retire the page, a "\nts: 9999…" must not forge
	// the tie-break, and a "\neditor: alice" must not spoof provenance. Folding beats escaping:
	// there is no reading of these fields where an embedded newline is meaning.
	title := oneLine(e.Page)
	if title == "" {
		return fmt.Errorf("wiki: a page needs a title")
	}
	body := strings.TrimSpace(e.Text)
	if body == "" {
		return fmt.Errorf("wiki: page %q: an empty body erases nothing and says nothing — to retire a page, write remember{page, stale:true} with WHY it stopped being true as the text", title)
	}
	slug := wikiSlug(title)
	revDir := filepath.Join(s.dir, "wiki", "revisions", slug)
	// No renaming of a pre-fold legacy chain: a rename raced the set-union sync (a peer still
	// holding the legacy files re-offered them, absorb recreated the dir, and the page came back
	// twice) and behaved differently on case-insensitive filesystems. Chains that fold to one
	// title are instead UNIFIED AT READ TIME (wikiBuckets groups revisions by their title's slug),
	// which no rename can race. New revisions land in the folded dir; the seq continues from the
	// SAME bucket the readers compute — not from a guessed pair of directories, which missed a
	// legacy chain whose dir spelling differed from sanitize(title) (a double-spaced or
	// case-variant original), landed the correction at seq 1, and let the chain it corrected keep
	// winning while the tool answered "updated".
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		return err
	}
	revs := wikiBuckets(filepath.Join(s.dir, "wiki", "revisions"))[slug]
	sortWikiRevisions(revs)
	seq := 1
	if len(revs) > 0 {
		seq = revs[0].seq + 1
	}
	summary := oneLine(e.Summary)
	if summary == "" {
		summary = firstLine(body) // tolerated, not refused: a refused write teaches a model to stop writing
	}
	// Nanosecond timestamps: two edits from two machines land within the same second routinely,
	// and a seconds-granular tie falls through to the filename — deterministic, but "the later
	// edit wins" should hold whenever the clocks can actually tell the edits apart.
	// [[wikilinks]] written in the body ARE links — that is how a wiki is written, and a model
	// that names a related page mid-sentence should not also have to repeat it in a field.
	// Merged with the explicit list, deduplicated, order preserved.
	var links []string
	seen := map[string]bool{}
	for _, l := range append(append([]string{}, e.Links...), wikiLinksIn(body)...) {
		// Commas fold to spaces along with newlines: the frontmatter renders links comma-joined,
		// so a comma inside one link would split it into two on the next parse.
		l = oneLine(strings.ReplaceAll(l, ",", " "))
		if k := strings.ToLower(l); l != "" && !seen[k] {
			seen[k] = true
			links = append(links, l)
		}
	}
	rev := wikiRevision{
		Title: title, Editor: oneLine(editor), TS: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: summary, Links: links, Body: body, Stale: e.Stale, seq: seq,
	}
	name := fmt.Sprintf("%04d-%s-%s.md", seq, sanitize(nonEmpty(editor, "unknown")), memoryID(body))
	if err := atomicfile.Write(filepath.Join(revDir, name), []byte(renderWikiRevision(rev)), 0o644); err != nil {
		return err
	}
	s.WikiRefreshPages()
	return nil
}

// wikiLinksIn extracts [[bracketed]] page titles from a body, in order of appearance. A candidate
// still holding a bracket is malformed nesting ("[[a[[b]]", "[[a]b]]") and is dropped whole — a
// garbage link pollutes the graph forever, a dropped one costs a reader nothing.
func wikiLinksIn(body string) []string {
	var out []string
	for {
		i := strings.Index(body, "[[")
		if i < 0 {
			return out
		}
		body = body[i+2:]
		j := strings.Index(body, "]]")
		if j < 0 {
			return out
		}
		if t := strings.TrimSpace(body[:j]); t != "" && !strings.ContainsAny(t, "\n[]") {
			out = append(out, t)
		}
		body = body[j+2:]
	}
}

// wikiSlug names a page's directory, from the title's FOLDED form — lowercased, whitespace
// normalized. Folding first is what keeps replicas structurally identical: a slug that preserved
// case put "Auth Flow" and "auth flow" in two directories on Linux and ONE on case-insensitive
// APFS, so a sync left the two filesystems disagreeing about the tree itself. Case and spacing
// variants of a title are one topic and now one chain; that merge is normalization, not loss.
//
// What IS loss: sanitize maps every rune outside [a-z0-9_-] to '-', which for a non-ASCII title
// collapses everything — two Korean titles land in one chain and the winner hides a whole topic.
// A lossy title therefore carries a short hash of its folded text, a pure function of the title,
// so distinct titles get distinct chains on every replica without coordination.
func wikiSlug(title string) string {
	folded := strings.ToLower(strings.Join(strings.Fields(title), " "))
	lossy := false
	for _, r := range folded {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ' ') {
			lossy = true
			break
		}
	}
	base := sanitize(folded)
	if !lossy {
		return base
	}
	id := memoryID(folded)
	if len(id) > 6 {
		id = id[:6]
	}
	return base + "-" + id
}

// wikiTokenize splits on runs of Unicode letters and digits, length ≥2, lowercased. The store's
// own tokenize keeps only ASCII runs — built for English memories, it scored every Korean title
// and body at ZERO, so the same change that stopped Korean titles colliding left Korean content
// unsearchable and the near-duplicate advisory blind to it. Pages are written in whatever
// language the team thinks in; the index has to read it.
func wikiTokenize(s string) map[string]bool {
	set := map[string]bool{}
	var run []rune
	flush := func() {
		if len(run) >= 2 {
			set[string(run)] = true
		}
		run = run[:0]
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			run = append(run, r)
			continue
		}
		flush()
	}
	flush()
	return set
}

func wikiOverlap(terms map[string]bool, text string) int {
	n := 0
	for w := range wikiTokenize(text) {
		if terms[w] {
			n++
		}
	}
	return n
}

// ContentID exposes the store's content-hash naming to the door sync, which verifies a received
// file's name against its body — the check that turns "content-addressed" from an honest-peer
// assumption into a property.
func ContentID(text string) string { return memoryID(text) }

// RevisionParts splits a wiki revision file the way THIS store's parser does, for the door sync's
// verification. One extractor, exported, because two implementations of "where does the body
// start" is exactly how the round-2 hash check was bypassed: the sync cut at the first "\n---\n"
// ANYWHERE while the parser demands the file open with "---\n", so a prefix pasted above the
// frontmatter passed the hash and became the parsed body. ok is false when the shape is not a
// revision at all.
func RevisionParts(content string) (title, body string, ok bool) {
	r := parseWikiRevision(content)
	if !strings.HasPrefix(content, "---\n") || r.Title == "" {
		return "", "", false
	}
	return r.Title, r.Body, true
}

// SlugOf exposes the page-directory naming (and its legacy pre-fold form) so the sync can check
// that a received revision actually belongs to the chain directory it arrived addressed to.
func SlugOf(title string) (current, legacy string) { return wikiSlug(title), sanitize(title) }

// oneLine folds a frontmatter value to a single trimmed line — see wikiWrite for why folding, not
// escaping.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r'
	}), " "))
}

// WikiSearch implements port.WikiStore: up to n current pages matching the query, best first. An
// exact (case-insensitive) title match outranks everything — that is what makes a title query
// behave as a page fetch — and title-word hits weigh triple a body hit.
func (s *Store) WikiSearch(ctx context.Context, query string, n int) ([]port.WikiPage, error) {
	terms := wikiTokenize(query)
	q := strings.ToLower(strings.TrimSpace(query))
	type scoredPage struct {
		score int
		p     port.WikiPage
	}
	var out []scoredPage
	for _, p := range s.wikiPages() {
		score := 3*wikiOverlap(terms, p.Title) + wikiOverlap(terms, p.Body)
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

// wikiRetireDays is the forgetting horizon: a page neither edited nor recalled for this long
// drops out of the INDEX — quiet retirement. It makes no claim about truth (that is the stale
// mark's job); it says the advertisement earned nothing lately. Search and the governance screen
// still hold the page, and one recall or edit re-advertises it.
const wikiRetireDays = 21 * 24 * time.Hour

// WikiTouch records that these pages were actually handed to a model — the usage half of
// forgetting. Local observation, deliberately NOT replicated: the ledger is a dot-file, and the
// sync's path filter refuses dot-names, so each machine forgets on its own experience.
func (s *Store) WikiTouch(titles []string) {
	if len(titles) == 0 {
		return
	}
	u := s.readWikiUsage()
	today := time.Now().UTC().Format("2006-01-02")
	changed := false
	for _, t := range titles {
		slug := wikiSlug(t)
		if _, err := os.Stat(filepath.Join(s.dir, "wiki", "revisions", slug)); err != nil {
			// A chain from before the folded-slug scheme sits under the raw sanitize; it migrates
			// on its next WRITE, but a page that is only ever READ would otherwise never receive
			// a touch and retire while in active use.
			if legacy := sanitize(t); legacy != slug {
				if _, lerr := os.Stat(filepath.Join(s.dir, "wiki", "revisions", legacy)); lerr == nil {
					slug = legacy
				} else {
					continue
				}
			} else {
				continue // not this tier's page; the tier that holds it records the touch
			}
		}
		if u[slug] != today {
			u[slug] = today
			changed = true
		}
	}
	if !changed {
		return
	}
	// Sorted, because this file is inside a directory the store git-commits wholesale: random map
	// order rewrote every line on every touch and turned the team history into noise. The door
	// sync never carries it (dot-name), but git does — so a .gitignore rides beside it.
	slugs := make([]string, 0, len(u))
	for slug := range u {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	var b strings.Builder
	for _, slug := range slugs {
		b.WriteString(slug + "\t" + u[slug] + "\n")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "wiki", ".gitignore")); os.IsNotExist(err) {
		if werr := atomicfile.Write(filepath.Join(s.dir, "wiki", ".gitignore"), []byte(".usage\n"), 0o644); werr != nil { //nolint:staticcheck // best-effort
			// The cost of a missing .gitignore is a noisy git history, not lost data.
		}
	}
	if err := atomicfile.Write(filepath.Join(s.dir, "wiki", ".usage"), []byte(b.String()), 0o644); err != nil {
		return // usage is advisory; losing a touch costs at worst an early retirement
	}
}

func (s *Store) readWikiUsage() map[string]string {
	u := map[string]string{}
	data, err := os.ReadFile(filepath.Join(s.dir, "wiki", ".usage"))
	if err != nil {
		return u
	}
	for _, line := range strings.Split(string(data), "\n") {
		if slug, day, ok := strings.Cut(line, "\t"); ok {
			u[slug] = day
		}
	}
	return u
}

// WikiIndex implements port.WikiStore: titles and hooks, most recently edited first. Stale pages
// are left out — the index is an advertisement, and a retired page has nothing to advertise
// (search still surfaces it, demoted and marked, for anyone who asks about its topic). So are
// FORGOTTEN pages: old, and recalled by nobody within the retirement horizon — the pile of
// never-used advertisements is exactly how an index stops being read.
func (s *Store) WikiIndex(ctx context.Context, n int) ([]port.WikiPage, error) {
	all := s.wikiPages()
	usage := s.readWikiUsage()
	now := time.Now().UTC()
	fresh := func(p port.WikiPage) bool {
		// One parse: RFC3339 accepts fractional seconds, so it covers the Nano-formatted stamps
		// this store writes and the plain ones a foreign editor might.
		if t, err := time.Parse(time.RFC3339, p.Updated); err == nil && now.Sub(t) < wikiRetireDays {
			return true
		}
		day, ok := usage[wikiSlug(p.Title)]
		if !ok {
			day, ok = usage[sanitize(p.Title)] // a pre-folding chain records touches under its old slug
		}
		if ok {
			if t, err := time.Parse("2006-01-02", day); err == nil && now.Sub(t) < wikiRetireDays {
				return true
			}
		}
		return false
	}
	pages := all[:0]
	for _, p := range all {
		if !p.Stale && fresh(p) {
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

// WikiRefreshPages rewrites the whole pages/ cache from the bucketed revision sets — after a
// local write and after the door sync absorbs foreign revisions, so the human/git view never
// keeps showing a pre-merge winner. It also removes cache files whose bucket no longer exists
// under that name (a legacy chain unified into its folded successor), so the humans do not see a
// page twice that the readers correctly see once. Best-effort throughout: readers derive from
// revisions; only the human-facing cache is at stake.
func (s *Store) WikiRefreshPages() {
	root := filepath.Join(s.dir, "wiki", "revisions")
	if _, err := os.Stat(root); err != nil {
		return // no revisions ever landed: nothing to render, and no pages/ dir to invent
	}
	buckets := wikiBuckets(root)
	pageDir := filepath.Join(s.dir, "wiki", "pages")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		return
	}
	for key, revs := range buckets {
		sortWikiRevisions(revs)
		if err := atomicfile.Write(filepath.Join(pageDir, key+".md"), []byte(renderWikiRevision(revs[0])), 0o644); err != nil { //nolint:staticcheck
			// Cache only; the revisions remain the truth.
		}
	}
	if cached, err := os.ReadDir(pageDir); err == nil {
		for _, f := range cached {
			name := strings.TrimSuffix(f.Name(), ".md")
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") || len(buckets[name]) > 0 {
				continue
			}
			// On a case-insensitive filesystem a legacy-cased entry ("Auth-Flow.md") IS the file
			// the folded write above just landed — the rename kept the old directory-entry
			// spelling — so removing it would empty the cache this refresh wrote and hand git a
			// spurious deletion. Same inode, same file: keep it. A genuinely distinct
			// legacy-cased file (case-sensitive filesystem) is a duplicate in the human view and
			// still goes.
			if low := strings.ToLower(name); low != name && len(buckets[low]) > 0 {
				a, aerr := os.Stat(filepath.Join(pageDir, f.Name()))
				b, berr := os.Stat(filepath.Join(pageDir, low+".md"))
				if aerr == nil && berr == nil && os.SameFile(a, b) {
					continue
				}
			}
			if rerr := os.Remove(filepath.Join(pageDir, f.Name())); rerr != nil { //nolint:staticcheck
				// An orphan cache file that will not leave costs a duplicate in the human view only.
			}
		}
	}
}

// WikiList returns every current page INCLUDING the stale ones, newest edit first — the
// governance view's read, where a tombstone is exactly the thing a person wants to see.
func (s *Store) WikiList(ctx context.Context) []port.WikiPage {
	pages := s.wikiPages()
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Updated > pages[j].Updated })
	return pages
}

// wikiBuckets reads every revision under root and groups it by the slug of its OWN title. This is
// THE grouping — the one fact every reader and writer must agree on: chains that fold to one
// title — a pre-fold legacy dir beside its folded successor, a case-variant dir a sync delivered
// from a differently-cased filesystem — are one page, with one winner computed over the union.
// Read-time unification is what a rename-based migration could not be: nothing races it, nothing
// resurrects, and every replica computes it identically from whatever set of directories it
// happens to hold. wikiWrite draws its next seq from the same buckets, so an edit outranks the
// WHOLE chain it corrects — round 4 found the write-time union guessing two directories and
// losing to a third the readers could see.
func wikiBuckets(root string) map[string][]wikiRevision {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	buckets := map[string][]wikiRevision{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		for _, r := range readWikiRevisions(filepath.Join(root, e.Name())) {
			key := wikiSlug(r.Title)
			if strings.TrimSpace(r.Title) == "" {
				key = e.Name() // a titleless revision can only belong to the dir it sits in
			}
			buckets[key] = append(buckets[key], r)
		}
	}
	return buckets
}

// wikiPages derives every current page from the bucketed revision sets (see wikiBuckets).
func (s *Store) wikiPages() []port.WikiPage {
	buckets := wikiBuckets(filepath.Join(s.dir, "wiki", "revisions"))
	var out []port.WikiPage
	for _, revs := range buckets {
		sortWikiRevisions(revs)
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
	sortWikiRevisions(out)
	return out
}

// sortWikiRevisions orders a revision set winner-first — shared by the per-directory read and the
// cross-directory bucket merge, because the winner must be the same function of the set wherever
// the set was assembled.
func sortWikiRevisions(out []wikiRevision) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		// Chronological, not lexicographic: RFC3339Nano drops trailing zeros, so "…:00Z"
		// string-compares above "…:00.5Z" and an EARLIER edit won the tie. Parsed times get the
		// order right; anything unparsable falls back to the string, which every replica computes
		// identically either way — convergence never depended on which order is "right".
		ti, ei := time.Parse(time.RFC3339, out[i].TS)
		tj, ej := time.Parse(time.RFC3339, out[j].TS)
		if ei == nil && ej == nil && !ti.Equal(tj) {
			return ti.After(tj)
		}
		if out[i].TS != out[j].TS {
			return out[i].TS > out[j].TS
		}
		return out[i].file > out[j].file
	})
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
