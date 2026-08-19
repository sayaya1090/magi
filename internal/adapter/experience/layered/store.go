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
	"sort"
	"strings"

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
func (s *Store) WithEmbedder(e Embedder) *Store { s.emb = e; return s }

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

	outMem := rank(ctx, s.emb, query, mems, memCap, func(m port.Memory) string { return m.Text })
	outSkill := rank(ctx, s.emb, query, skills, skillCap, func(k port.Skill) string {
		return k.Name + "\n" + k.Description
	})
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
	if emb != nil && emb.Available() {
		docs := make([]string, len(xs))
		for i := range xs {
			docs[i] = textOf(xs[i].V)
		}
		// Documents and query in one request; documents are cached by the client, so the
		// steady-state cost is one embedding for the query.
		if vecs, err := emb.Embed(ctx, append(append([]string{}, docs...), query)); err == nil && len(vecs) == len(docs)+1 {
			q := vecs[len(vecs)-1]
			sem := make([]int, 0, len(xs))
			for i := range xs {
				if embed.Cosine(vecs[i], q) > 0 {
					sem = append(sem, i)
				}
			}
			sort.SliceStable(sem, func(a, b int) bool {
				return embed.Cosine(vecs[sem[a]], q) > embed.Cosine(vecs[sem[b]], q)
			})
			order = embed.Fuse(lex, sem)
		}
		// On error: the lexical order stands. A search that quietly became worse is one nobody can
		// interpret, but a search that fails outright over a missing sidecar is worse still — the
		// turn needs its memories either way.
	}
	out := make([]T, 0, n)
	for _, i := range order {
		out = append(out, xs[i].V)
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
	return nil // no tier configured: silently drop rather than error
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
