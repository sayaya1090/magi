package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type exploreComparisonKey struct{}

type pinnedExploreComparison struct {
	source          port.ExploreComparison
	once            sync.Once
	model, profile  string
	provider        port.LLMProvider
	overrideModel   string
	overrideProfile string
}

func exploreComparisonFrom(ctx context.Context) *pinnedExploreComparison {
	value, _ := ctx.Value(exploreComparisonKey{}).(*pinnedExploreComparison)
	return value
}

func (p *pinnedExploreComparison) route(a *App, parent session.ModelRef, spec port.SpawnSpec) (string, string) {
	p.once.Do(func() {
		p.model, p.profile = spec.Model, spec.Provider
		if p.model == "" {
			p.model = parent.Model
		}
		if p.overrideModel != "" {
			p.model = p.overrideModel
		}
		if p.overrideProfile != "" {
			p.profile = p.overrideProfile
		}
		p.provider = a.providerFor(AgentSpec{Provider: p.profile})
	})
	return p.model, p.profile
}

func (a *App) exploreComparison(ctx context.Context, workdir string, run func(context.Context, port.ExploreComparison)) error {
	var source trialSource
	defer source.close()
	root, tree, _, err := source.get(ctx, a, workdir)
	if err != nil {
		return fmt.Errorf("pin explore source: %w", err)
	}
	revision, err := gitRun(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve explore revision: %w", err)
	}
	model, profile := a.subagentOverride("explore")
	pinned := &pinnedExploreComparison{
		source:        port.ExploreComparison{Root: root, Revision: strings.TrimSpace(revision), Tree: tree},
		overrideModel: model, overrideProfile: profile,
	}
	run(context.WithValue(ctx, exploreComparisonKey{}, pinned), pinned.source)
	return nil
}
