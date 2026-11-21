package port

import "context"

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
