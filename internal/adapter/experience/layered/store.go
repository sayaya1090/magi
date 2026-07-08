// Package layered composes two git-backed experience stores into a single
// ExperienceStore with a project tier and a global tier. The project tier lives
// inside the workspace (e.g. <workspace>/.magi/experience, git-trackable with the
// repo so a team shares it) and holds context-specific learnings; the global tier
// (e.g. <config>/experience) holds cross-project knowledge. Retrieval merges both
// under one fixed budget so adding a tier never widens the injected context;
// contributions route by Scope, defaulting to the project tier.
package layered

import (
	"context"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/port"
)

// Store is a two-tier ExperienceStore. Either tier may be nil.
type Store struct {
	project *expgit.Store
	global  *expgit.Store
}

// New returns a store with a project tier rooted at projectDir and a global tier
// rooted at globalDir. An empty dir disables that tier.
func New(projectDir, globalDir string) *Store {
	s := &Store{}
	if projectDir != "" {
		s.project = expgit.New(projectDir)
	}
	if globalDir != "" {
		s.global = expgit.New(globalDir)
	}
	return s
}

// Retrieve merges results from both tiers under one combined budget (project
// results first, since they are the most context-specific), tagging each entry
// with its tier so a reader can tell workspace-local from global knowledge.
func (s *Store) Retrieve(ctx context.Context, query string) ([]port.Memory, []port.Skill, error) {
	const memCap, skillCap = 5, 3
	var mems []port.Memory
	var skills []port.Skill

	add := func(st *expgit.Store, tier string) {
		if st == nil {
			return
		}
		m, sk, err := st.Retrieve(ctx, query)
		if err != nil {
			return // best-effort: a broken tier must not sink the other
		}
		for _, x := range m {
			x.ID = tier + " " + x.ID
			x.Text = tier + " " + x.Text
			mems = append(mems, x)
		}
		for _, x := range sk {
			x.Name = tier + " " + x.Name
			skills = append(skills, x)
		}
	}
	add(s.project, "[project]")
	add(s.global, "[global]")

	if len(mems) > memCap {
		mems = mems[:memCap]
	}
	if len(skills) > skillCap {
		skills = skills[:skillCap]
	}
	return mems, skills, nil
}

// Propose routes a contribution to the tier named by c.Scope. "global" targets
// the global tier; anything else (including "" and "project") targets the project
// tier. If the requested tier is not configured, it falls back to the other.
func (s *Store) Propose(ctx context.Context, c port.Contribution) error {
	target, fallback := s.project, s.global
	if c.Scope == "global" {
		target, fallback = s.global, s.project
	}
	if target == nil {
		target = fallback
	}
	if target == nil {
		return nil // no tier configured: silently drop rather than error
	}
	return target.Propose(ctx, c)
}
