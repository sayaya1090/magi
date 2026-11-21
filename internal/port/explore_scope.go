package port

import (
	"context"
	"sync"
)

type exploreReadScopeKey struct{}

// WithExploreReadScope limits a nested explore call to a workspace-relative directory.
func WithExploreReadScope(ctx context.Context, directory string) context.Context {
	return context.WithValue(ctx, exploreReadScopeKey{}, directory)
}

// ExploreReadScope returns the directory supplied by a scoped script host.
func ExploreReadScope(ctx context.Context) string {
	scope, _ := ctx.Value(exploreReadScopeKey{}).(string)
	return scope
}

type exploreMetricsKey struct{}

// WithExploreMetrics attaches a receiver for the model work of a scoped explore.
func WithExploreMetrics(ctx context.Context, sink func(RunMetrics, string, string)) context.Context {
	return context.WithValue(ctx, exploreMetricsKey{}, sink)
}

func ReportExploreMetrics(ctx context.Context, metrics RunMetrics, model, profile string) {
	if sink, ok := ctx.Value(exploreMetricsKey{}).(func(RunMetrics, string, string)); ok {
		sink(metrics, model, profile)
	}
}

// ExploreComparison identifies the pinned source available during a comparison callback.
type ExploreComparison struct {
	Root, Revision, Tree string
}

type exploreBudgetKey struct{}

// ExploreBudget is shared by all backend requests made by a tracing routine.
type ExploreBudget struct {
	mu                       sync.Mutex
	requests, tokens         int
	usedRequests, usedTokens int
}

func WithExploreBudget(ctx context.Context, requests, tokens int) context.Context {
	return context.WithValue(ctx, exploreBudgetKey{}, &ExploreBudget{requests: requests, tokens: tokens})
}

func ExploreBudgetFrom(ctx context.Context) *ExploreBudget {
	b, _ := ctx.Value(exploreBudgetKey{}).(*ExploreBudget)
	return b
}

func (b *ExploreBudget) Admit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.usedRequests >= b.requests || (b.tokens > 0 && b.usedTokens >= b.tokens) {
		return false
	}
	b.usedRequests++
	return true
}

func (b *ExploreBudget) RecordTokens(tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usedTokens += tokens
}
