package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// countingExperience counts Retrieve calls and can be switched to fail.
type countingExperience struct {
	calls int
	fail  bool
}

func (c *countingExperience) Retrieve(ctx context.Context, q string) ([]port.Memory, []port.Skill, error) {
	c.calls++
	if c.fail {
		return nil, nil, fmt.Errorf("boom")
	}
	return []port.Memory{{ID: "m", Text: "t"}}, nil, nil
}
func (c *countingExperience) Propose(ctx context.Context, con port.Contribution) error { return nil }

// The experience pointer is scanned once per (session, query): repeat steps hit the
// cache, a new query misses, and a Retrieve error is NOT cached so the next step retries.
func TestExperiencePointerCached(t *testing.T) {
	exp := &countingExperience{}
	a := &App{cfg: Config{Experience: exp}}
	sid := session.SessionID("s1")

	p1 := a.experiencePointerCached(context.Background(), sid, "query one")
	p2 := a.experiencePointerCached(context.Background(), sid, "query one")
	if exp.calls != 1 {
		t.Fatalf("same query must scan once, got %d calls", exp.calls)
	}
	if p1 == "" || p1 != p2 {
		t.Errorf("cache must return the identical pointer, got %q / %q", p1, p2)
	}
	a.experiencePointerCached(context.Background(), sid, "query two")
	if exp.calls != 2 {
		t.Errorf("a new query must re-scan, got %d calls", exp.calls)
	}

	// Errors are not cached: the per-step retry of the uncached code is preserved.
	exp.fail = true
	if got := a.experiencePointerCached(context.Background(), sid, "query three"); got != "" {
		t.Errorf("error must yield empty pointer, got %q", got)
	}
	a.experiencePointerCached(context.Background(), sid, "query three")
	if exp.calls != 4 {
		t.Errorf("a failed retrieve must be retried (not cached), got %d calls", exp.calls)
	}
}

// countingProvider counts Provide calls; fail makes it error (folded to "" upstream).
type countingProvider struct {
	calls int
	fail  bool
}

func (c *countingProvider) Provide(ctx context.Context, q port.ContextQuery) ([]port.ContextChunk, error) {
	c.calls++
	if c.fail {
		return nil, fmt.Errorf("provider down")
	}
	return []port.ContextChunk{{Source: "src", Text: "chunk"}}, nil
}

// The plugin-RAG block is gathered once per (session, query) — and unlike the experience
// pointer, an empty (all-errored) result IS cached, so a dead provider's 5s timeout can't
// re-block every remaining step of the turn.
func TestGatherContextCached(t *testing.T) {
	p := &countingProvider{}
	a := &App{}
	a.RegisterContextProvider(p)
	s := session.Session{ID: "s1", Workdir: t.TempDir()}

	c1 := a.gatherContextCached(context.Background(), s, "query one")
	c2 := a.gatherContextCached(context.Background(), s, "query one")
	if p.calls != 1 {
		t.Fatalf("same query must gather once, got %d calls", p.calls)
	}
	if c1 == "" || c1 != c2 {
		t.Errorf("cache must return the identical block, got %q / %q", c1, c2)
	}

	// An errored (empty) gather is cached too — that's the whole point.
	p.fail = true
	a.gatherContextCached(context.Background(), s, "query two")
	a.gatherContextCached(context.Background(), s, "query two")
	if p.calls != 2 {
		t.Errorf("an empty result must be cached (no per-step re-block), got %d calls", p.calls)
	}
}

// Registering a provider invalidates the RAG cache so the newcomer is consulted.
func TestRegisterContextProviderInvalidatesRagCache(t *testing.T) {
	p1 := &countingProvider{}
	a := &App{}
	a.RegisterContextProvider(p1)
	s := session.Session{ID: "s1", Workdir: t.TempDir()}

	a.gatherContextCached(context.Background(), s, "q")
	p2 := &countingProvider{}
	a.RegisterContextProvider(p2)
	a.gatherContextCached(context.Background(), s, "q")
	if p2.calls != 1 {
		t.Errorf("newly registered provider must be consulted after invalidation, got %d calls", p2.calls)
	}
}

// A new top-level turn clears both retrieval caches even when the prompt text repeats.
func TestResetForNewTopLevelClearsRetrievalCaches(t *testing.T) {
	exp := &countingExperience{}
	a := &App{cfg: Config{Experience: exp}}
	sid := session.SessionID("s1")

	a.experiencePointerCached(context.Background(), sid, "same prompt")
	a.resetForNewTopLevel(sid)
	a.experiencePointerCached(context.Background(), sid, "same prompt")
	if exp.calls != 2 {
		t.Errorf("reset must clear the cache (fresh scan on the identical prompt), got %d calls", exp.calls)
	}
}
