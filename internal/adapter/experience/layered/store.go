// Package layered composes git-backed experience stores into a single
// ExperienceStore with a project tier, a team tier and a global tier. The project tier lives
// inside the workspace (e.g. <workspace>/.magi/experience, git-trackable with the
// repo so a team shares it) and holds context-specific learnings; the global tier
// (e.g. <config>/experience) holds cross-project knowledge. Retrieval merges both
// under one fixed budget so adding a tier never widens the injected context;
// contributions route by Scope, defaulting to the project tier.
package layered

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/core/embed"
	"github.com/sayaya1090/magi/internal/port"
)

// Embedder is the semantic half of retrieval: what turns a document into a vector.
//
// An interface, not *embed.Client, so this package can be tested without a model and so the one
// thing this store needs of embedding is visible in one line. Available() is asked before any
// request, because a machine with no embed model configured is the DEFAULT, not an error.
type Embedder interface {
	Available() bool
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Store is a three-tier ExperienceStore. Any tier may be nil.
type Store struct {
	// emb is optional. Without it retrieval is lexical, which is what magi did for its whole life
	// and is still correct — just deaf to the question asked in words the answer does not use.
	emb     Embedder
	project *expgit.Store
	// team is what companions doing related work share. A workspace is one companion's, and the
	// global tier is every companion on the machine — neither can hold "the frontend team decided
	// X", which is the thing a person actually wants to write down once and have three companions
	// follow. It sits between them because it is more specific than the machine and less specific
	// than one directory.
	team   *expgit.Store
	global *expgit.Store
	// minSim is the cosine a document's best chunk must reach to enter the semantic ranking. 0 keeps
	// the old rule (any positive cosine), which is what a store built without WithMinSimilarity gets.
	minSim float64
}

// Managed projections currently stay in the explicit project store. No fallback
// to a wider scope: local undo must not acquire authority over a team copy.
func (s *Store) ApplyExperience(ctx context.Context, c port.ExperienceChange) (port.ExperienceReceipt, error) {
	if c.Scope != "" && c.Scope != "project" {
		return port.ExperienceReceipt{}, fmt.Errorf("managed experience currently supports project scope only")
	}
	if s.project == nil {
		return port.ExperienceReceipt{}, fmt.Errorf("project experience store is not configured")
	}
	return s.project.ApplyExperience(ctx, c)
}

// New returns a store with the three tiers rooted at the given directories. An empty dir disables
// that tier — a companion that declares no team has no team tier, which is not an error.
func New(projectDir, teamDir, globalDir string) *Store {
	s := &Store{}
	if projectDir != "" {
		s.project = expgit.New(projectDir)
	}
	if teamDir != "" {
		s.team = expgit.New(teamDir)
	}
	if globalDir != "" {
		s.global = expgit.New(globalDir)
	}
	return s
}

// WithEmbedder returns the store with a semantic ranker attached. Nil, or a client with nothing
// configured, leaves retrieval lexical.
//
// The tiers get it too, for the other half of the same question. Reading asks "which of these says
// what I am asking about"; writing asks "does one of these already say what I am about to write".
// One judge answers both, or the store would find a memory by meaning and then file a second copy
// of it by string.
func (s *Store) WithEmbedder(e Embedder) *Store {
	s.emb = e
	for _, t := range []*expgit.Store{s.project, s.team, s.global} {
		if t != nil {
			t.WithSame(e)
		}
	}
	return s
}

// WithMinSimilarity sets the cosine below which a document is left out of the semantic ranking.
//
// Without it every document with a positive cosine is ranked, and an embedding model scores almost
// any two texts above zero, so an unrelated memory still takes a place in the fusion and can fill a
// slot the lexical half left open. The value is model-specific: a cosine from one embedding model
// says nothing about another's scale. Out-of-range values are ignored.
func (s *Store) WithMinSimilarity(v float64) *Store {
	if v >= 0 && v < 1 {
		s.minSim = v
	}
	return s
}

const (
	memCap   = 5
	skillCap = 3
)

// Retrieve merges the tiers under one combined budget (project results first, since they are the
// most context-specific), tagging each entry with its tier so a reader can tell workspace-local
// from global knowledge.
//
// The budget is the point of doing this here rather than per tier: adding a tier must not widen
// the injected context, and one ranking across the union is also the only place a team memory can
// legitimately outrank a project one on merit.
func (s *Store) Retrieve(ctx context.Context, query string, agentGroups []string) ([]port.Memory, []port.Skill, error) {
	// Every candidate every tier holds, lexical score attached. Tier order is preserved and is the
	// tie-break: what this workspace learned beats what the team decided, which beats what the
	// machine knows.
	var mems []expgit.Scored[port.Memory]
	var skills []expgit.Scored[port.Skill]
	add := func(st *expgit.Store, tier string) {
		if st == nil {
			return
		}
		m, sk, err := st.Pool(ctx, query, agentGroups)
		if err != nil {
			return // best-effort: a broken tier must not sink the other
		}
		for _, x := range m {
			x.V.ID = tier + " " + x.V.ID
			x.V.Text = tier + " " + x.V.Text
			mems = append(mems, x)
		}
		for _, x := range sk {
			x.V.Name = tier + " " + x.V.Name
			skills = append(skills, x)
		}
	}
	add(s.project, "[project]")
	add(s.team, "[team]")
	add(s.global, "[global]")
	expgit.RankCJK(query, mems, func(m port.Memory) string { return m.Text })
	expgit.RankCJK(query, skills, func(k port.Skill) string { return k.Name + "\n" + k.Description + "\n" + k.Body })

	outMem := rankWithin(ctx, s.emb, s.minSim, query, mems, memCap, func(m port.Memory) string { return m.Text },
		func(m port.Memory, e string) port.Memory { m.Excerpt = e; return m })
	// The body is embedded too: it is chunked, so a long skill no longer costs one blurred vector,
	// and the step that answers a question is usually in the body, not the one-line description.
	outSkill := rankWithin(ctx, s.emb, s.minSim, query, skills, skillCap, func(k port.Skill) string {
		return k.Name + "\n" + k.Description + "\n" + k.Body
	}, nil)
	return outMem, outSkill, nil
}

// rank picks the best n candidates: lexically when there is no embedder, and by rank fusion when
// there is.
//
// Fused on ORDER, never on the two scores — an IDF-ish overlap count and a cosine are not
// comparable numbers, and any weighted sum of them needs a normalisation invented on whatever
// corpus was at hand (embed.Fuse carries the argument in full). The lexical list stays in the
// fusion rather than being replaced: an embedding is a similarity judgement made by a model, and
// it is confidently wrong often enough that a rare exact token — a file name, an error code, an
// identifier — must not lose to something that merely feels related.
//
// A document with NO lexical hit is in the semantic list but not the lexical one, which is the
// whole point: that is the note about invoices, when the question said billing.
func rank[T any](ctx context.Context, emb Embedder, query string, xs []expgit.Scored[T], n int, textOf func(T) string) []T {
	return rankWithin(ctx, emb, 0, query, xs, n, textOf, nil)
}

// rankWithin is rank with a similarity floor and chunked documents.
//
// Each document is split into passages (embed.Chunks) and scored by its best passage. A document at
// or under one chunk embeds exactly as before, so its cached vector is still a hit; a long one is
// no longer one averaged vector that its matching paragraph cannot lift. minSim drops documents
// whose best passage is below it; 0 keeps any positive cosine.
//
// withExcerpt, when set, receives each semantically ranked document's best passage clipped to
// excerptRunes, so the caller can show the part that matched instead of the whole document.
func rankWithin[T any](ctx context.Context, emb Embedder, minSim float64, query string, xs []expgit.Scored[T], n int, textOf func(T) string, withExcerpt func(T, string) T) []T {
	if len(xs) == 0 || n <= 0 {
		return nil
	}
	// Lexical order: score desc, stable, zeros dropped — the ranking this store has always used.
	lex := make([]int, 0, len(xs))
	for i := range xs {
		if xs[i].Score > 0 {
			lex = append(lex, i)
		}
	}
	sort.SliceStable(lex, func(a, b int) bool { return xs[lex[a]].Score > xs[lex[b]].Score })

	order := lex
	excerpts := make([]string, len(xs))
	if emb != nil && emb.Available() {
		var chunks []string
		var owner []int
		for i := range xs {
			for _, c := range embed.Chunks(textOf(xs[i].V)) {
				chunks = append(chunks, c)
				owner = append(owner, i)
			}
		}
		// Chunks and query in one request; chunks are cached by the client, so the steady-state
		// cost is one embedding for the query.
		if vecs, err := emb.Embed(ctx, append(append([]string{}, chunks...), query)); err == nil && len(vecs) == len(chunks)+1 {
			q := vecs[len(vecs)-1]
			best := make([]float64, len(xs))
			seen := make([]bool, len(xs))
			bestAt := make([]int, len(xs))
			for j, i := range owner {
				if c := embed.Cosine(vecs[j], q); !seen[i] || c > best[i] {
					best[i], seen[i], bestAt[i] = c, true, j
				}
			}
			sem := make([]int, 0, len(xs))
			for i := range xs {
				if seen[i] && best[i] > 0 && best[i] >= minSim {
					sem = append(sem, i)
				}
			}
			sort.SliceStable(sem, func(a, b int) bool { return best[sem[a]] > best[sem[b]] })
			for _, i := range sem {
				excerpts[i] = clipRunes(chunks[bestAt[i]], excerptRunes)
			}
			order = embed.Fuse(lex, sem)
		}
		// On error: the lexical order stands. A search that quietly became worse is one nobody can
		// interpret, but a search that fails outright over a missing sidecar is worse still — the
		// turn needs its memories either way.
	}
	// Keep the fused ranking within each age class, then use cold only to
	// fill slots not satisfied by active candidates across ALL tiers.
	sort.SliceStable(order, func(a, b int) bool { return !xs[order[a]].Cold && xs[order[b]].Cold })
	out := make([]T, 0, n)
	for _, i := range order {
		v := xs[i].V
		if withExcerpt != nil && excerpts[i] != "" {
			v = withExcerpt(v, excerpts[i])
		}
		out = append(out, v)
		if len(out) >= n {
			break
		}
	}
	return out
}

// Propose routes a contribution to the tier named by c.Scope: "global", "team", or anything else
// (including "" and "project") for the project tier.
//
// A scope with no tier behind it falls back rather than failing — a companion that declares no team
// and is asked to remember something for the team should not lose it. It lands in the next tier
// down, which is the project, because writing to the machine's tier on a companion's behalf would
// put a private decision in front of everybody.
func (s *Store) Propose(ctx context.Context, c port.Contribution) error {
	order := []*expgit.Store{s.project, s.team, s.global}
	switch c.Scope {
	case "global":
		order = []*expgit.Store{s.global, s.team, s.project}
	case "team":
		order = []*expgit.Store{s.team, s.project, s.global}
	case "":
		// A wiki page's DEFAULT tier is TEAM, not the project: a page is written to be read by
		// the OTHER companions — that is its whole reason to exist — and a workspace-local
		// default would file it where they never look. Only the unstated case: an explicit
		// "project" scope routes a page like anything else. A MIXED unscoped contribution
		// (wiki edits beside memories or skills) keeps the project default for the whole batch
		// — no producer mixes them today (remember sends a page alone), and one contribution
		// landing in two tiers would be worse than either default.
		if len(c.Wiki) > 0 && len(c.Memories) == 0 && len(c.Skills) == 0 {
			order = []*expgit.Store{s.team, s.project, s.global}
		}
	}
	for _, t := range order {
		if t != nil {
			return t.Propose(ctx, c)
		}
	}
	// Nowhere to put it is not "saved". Answered as a failure so the caller — the tool that told
	// the model its note was written down — says what actually happened.
	return fmt.Errorf("nothing was recorded: this workspace has no experience store configured")
}

// WikiSearch implements port.WikiStore across the tiers, each page tagged with its tier — under
// one shared budget, like Retrieve. Every tier is ASKED before anything is cut: consuming the
// budget tier-by-tier let a weak project-tier body match shadow an exact team-tier title match,
// which broke the one promise the search makes ("a title query behaves as a page fetch") and made
// the near-duplicate advisory nag updates whose page it simply never saw. Exact title matches
// lead; within each half the tier order (most specific first) still decides.
func (s *Store) WikiSearch(ctx context.Context, query string, n int) ([]port.WikiPage, error) {
	q := strings.TrimSpace(query)
	var exact, rest []port.WikiPage
	for _, t := range []struct {
		st   *expgit.Store
		tier string
	}{{s.project, "project"}, {s.team, "team"}, {s.global, "global"}} {
		if t.st == nil {
			continue
		}
		pages, err := t.st.WikiSearch(ctx, query, n)
		if err != nil {
			continue // best-effort: a broken tier must not sink the others
		}
		for _, p := range pages {
			p.Tier = t.tier
			if strings.EqualFold(strings.TrimSpace(p.Title), q) {
				exact = append(exact, p)
			} else {
				rest = append(rest, p)
			}
		}
	}
	out := append(exact, rest...)
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// WikiTouch records a recall on whichever tier holds each page — the usage half of the
// forgetting horizon (see the git store). Best-effort fan-out; a tier that holds none of the
// titles writes nothing.
func (s *Store) WikiTouch(titles []string) {
	for _, t := range []*expgit.Store{s.project, s.team, s.global} {
		if t != nil {
			t.WikiTouch(titles)
		}
	}
}

// WikiIndex implements port.WikiStore across the tiers, tier-tagged, team first — the index
// exists to advertise what the OTHER companions wrote, which is the team tier's whole content.
func (s *Store) WikiIndex(ctx context.Context, n int) ([]port.WikiPage, error) {
	var out []port.WikiPage
	for _, t := range []struct {
		st   *expgit.Store
		tier string
	}{{s.team, "team"}, {s.project, "project"}, {s.global, "global"}} {
		if t.st == nil || len(out) >= n {
			continue
		}
		pages, err := t.st.WikiIndex(ctx, n-len(out))
		if err != nil {
			continue
		}
		for _, p := range pages {
			p.Tier = t.tier
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Store) ExperienceReferences(ctx context.Context, name string) ([]port.ExperienceReference, error) {
	if s.project == nil {
		return nil, nil
	}
	return s.project.ExperienceReferences(ctx, name)
}
func (s *Store) ProtectExperience(ctx context.Context, r port.ExperienceReference, id string, until time.Time) error {
	if s.project == nil {
		return fmt.Errorf("project experience unavailable")
	}
	return s.project.ProtectExperience(ctx, r, id, until)
}
func (s *Store) ReleaseExperience(ctx context.Context, r port.ExperienceReference, id string) error {
	if s.project == nil {
		return fmt.Errorf("project experience unavailable")
	}
	return s.project.ReleaseExperience(ctx, r, id)
}
func (s *Store) ConfirmExperienceApplication(ctx context.Context, r port.ExperienceReference, id, evidence string) error {
	if s.project == nil {
		return fmt.Errorf("project experience unavailable")
	}
	return s.project.ConfirmExperienceApplication(ctx, r, id, evidence)
}

func (s *Store) ManagedMemoryReferences(ctx context.Context, ids []string) ([]port.ExperienceReference, error) {
	if s.project == nil {
		return nil, nil
	}
	var project []string
	for _, id := range ids {
		if strings.HasPrefix(id, "[project] ") {
			project = append(project, strings.TrimPrefix(id, "[project] "))
		}
	}
	return s.project.ManagedMemoryReferences(ctx, project)
}

func (s *Store) ManageKnowledge(ctx context.Context, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	if s.project == nil {
		return nil, fmt.Errorf("project experience unavailable")
	}
	return s.project.ManageKnowledge(ctx, action)
}

func (s *Store) ImportLegacySkill(ctx context.Context, skill port.Skill) error {
	if s.project == nil {
		return fmt.Errorf("project experience unavailable")
	}
	return s.project.ImportLegacySkill(ctx, skill)
}

// excerptRunes bounds a recall excerpt. Five memories at this size are about 3,000 characters, the
// per-turn recall budget of the design (docs/ENGRAM_MEMORY.md §5, §11.3).
const excerptRunes = 600

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
