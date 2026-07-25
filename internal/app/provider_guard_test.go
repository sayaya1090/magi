package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestDegenerateRepeat(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want bool // whether a repetition loop is detected
	}{
		{"sentence repeated", strings.Repeat("The server is now running on port 5328. ", 6), true},
		{"short phrase repeated", strings.Repeat("the ", 60), true},
		{"single char run", strings.Repeat("a", 200), true},
		{"normal prose", "The quick brown fox jumps over the lazy dog. " +
			"A completely ordinary paragraph of varied text that does not loop at all, continuing with more words.", false},
		{"realistic planner JSON (must not false-fire)", `{"steps":[` +
			`{"n":1,"strategy":"solo","title":"Read and analyze the OCaml runtime shared_heap.c"},` +
			`{"n":2,"strategy":"solo","title":"Locate the POOL_BLOCK_FREE_HP macro and its callers"},` +
			`{"n":3,"strategy":"solo","title":"Implement the run-length compression fix in the free-list walk"},` +
			`{"n":4,"strategy":"solo","title":"Build the runtime and run the GC stress test to verify"}]}`, false},
		{"blank lines only (not content)", strings.Repeat("\n", 200), false},
		{"too short to judge", "the the the", false},
		{"few reps below threshold", strings.Repeat("hello world ", 2), false}, // 24 bytes < repMinBlock
	}
	for _, c := range cases {
		got := degenerateRepeat([]byte(c.tail)) > 0
		if got != c.want {
			t.Errorf("%s: degenerateRepeat=%v, want %v", c.name, got, c.want)
		}
	}
}

// A non-repeating tail must stay cheap: each candidate period mismatches at the first comparison.
func TestDegenerateRepeatNoFalsePositiveOnVariedText(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		// The line number makes the content non-periodic (no repeating block), unlike a fixed cycle.
		fmt.Fprintf(&b, "line %d: %s done\n", i, strings.Repeat("x", i%13))
	}
	if p := degenerateRepeat([]byte(b.String())); p > 0 {
		t.Errorf("varied text falsely flagged as repetition (period %d)", p)
	}
}

// GuardProvider is idempotent (double-wrap returns the same guarded provider, never a nested one) and
// nil-safe, so applying it at every provider-creation site is cheap and cannot double-count the guard.
func TestGuardProviderIdempotentAndNilSafe(t *testing.T) {
	if GuardProvider(nil) != nil {
		t.Error("GuardProvider(nil) must stay nil")
	}
	g1 := GuardProvider(noopProvider{})
	if _, ok := g1.(guardedProvider); !ok {
		t.Fatalf("GuardProvider must return a guardedProvider, got %T", g1)
	}
	if g2 := GuardProvider(g1); g2 != g1 {
		t.Error("wrapping an already-guarded provider must return it unchanged, not double-wrap")
	}
}

// noopProvider is a minimal port.LLMProvider for the wrapping tests; its StreamChat is never invoked.
type noopProvider struct{}

func (noopProvider) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	return nil, nil
}
