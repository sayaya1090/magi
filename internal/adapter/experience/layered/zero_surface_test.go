package layered

import (
	"context"
	"testing"
)

// The wiki pass-throughs hold on a store with no tiers wired: touching is a no-op and the index
// is empty — not a panic, which is what a nil tier would otherwise be.
func TestWikiPassThroughsHoldWithNoTiers(t *testing.T) {
	s := &Store{}
	s.WikiTouch([]string{"Deploy Steps"})
	pages, err := s.WikiIndex(context.Background(), 5)
	if err != nil || len(pages) != 0 {
		t.Fatalf("no tiers, no pages, no error: (%v, %v)", pages, err)
	}
}
