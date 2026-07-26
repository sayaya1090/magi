package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
)

// diffSteps reports which entries are new in `after` (added, in after-order) and which are gone
// from `before` (removed, in before-order). Membership is set-based, so a step present in both is
// neither added nor removed regardless of position.
func TestDiffSteps(t *testing.T) {
	added, removed := diffSteps(
		[]string{"a", "b", "c"},
		[]string{"b", "c", "d", "e"},
	)
	if !reflect.DeepEqual(added, []string{"d", "e"}) {
		t.Errorf("added = %v, want [d e]", added)
	}
	if !reflect.DeepEqual(removed, []string{"a"}) {
		t.Errorf("removed = %v, want [a]", removed)
	}
	// Identical plans → no diff (both named returns stay nil).
	if a, r := diffSteps([]string{"x"}, []string{"x"}); a != nil || r != nil {
		t.Errorf("identical: added=%v removed=%v, want nil/nil", a, r)
	}
	// Reorder without content change → still no diff (set membership, not position).
	if a, r := diffSteps([]string{"a", "b"}, []string{"b", "a"}); a != nil || r != nil {
		t.Errorf("reorder: added=%v removed=%v, want nil/nil", a, r)
	}
	// From-empty is all added; to-empty is all removed, each in source order.
	if a, _ := diffSteps(nil, []string{"p", "q"}); !reflect.DeepEqual(a, []string{"p", "q"}) {
		t.Errorf("from empty: added=%v, want [p q]", a)
	}
	if _, r := diffSteps([]string{"p", "q"}, nil); !reflect.DeepEqual(r, []string{"p", "q"}) {
		t.Errorf("to empty: removed=%v, want [p q]", r)
	}
}

// planDepthFromEnv reads MAGI_MAX_PLAN_DEPTH: a valid non-negative int is used; empty (incl. unset),
// non-numeric, or negative all fall back to 0 (the "unset" sentinel). The value is trimmed.
func TestPlanDepthFromEnv(t *testing.T) {
	cases := []struct {
		val  string
		want int
	}{
		{"", 0}, // empty behaves like unset
		{"3", 3},
		{"  5 ", 5}, // trimmed
		{"0", 0},
		{"-2", 0},  // negative → 0
		{"abc", 0}, // non-numeric → 0
	}
	for _, c := range cases {
		t.Setenv("MAGI_MAX_PLAN_DEPTH", c.val)
		if got := planDepthFromEnv(); got != c.want {
			t.Errorf("MAGI_MAX_PLAN_DEPTH=%q: got %d, want %d", c.val, got, c.want)
		}
	}
}

// profileDefs maps config profiles to app.ProfileDef verbatim (name-keyed, every field carried), and
// returns nil for an empty input so the caller can treat "no profiles" as absent.
func TestProfileDefs(t *testing.T) {
	if got := profileDefs(nil); got != nil {
		t.Errorf("nil input: want nil, got %v", got)
	}
	if got := profileDefs(map[string]config.LLMProfile{}); got != nil {
		t.Errorf("empty input: want nil, got %v", got)
	}
	in := map[string]config.LLMProfile{
		"fast": {BaseURL: "https://fast/v1", APIKey: "k", Model: "m", Headers: map[string]string{"X": "1"}},
	}
	want := app.ProfileDef{Name: "fast", BaseURL: "https://fast/v1", APIKey: "k", Model: "m", Headers: map[string]string{"X": "1"}}
	if got := profileDefs(in); !reflect.DeepEqual(got["fast"], want) {
		t.Errorf("profileDefs[fast] = %+v, want %+v", got["fast"], want)
	}
}

// Guard against benchmark overfitting: the top-level agent system prompt is the one prompt every
// run sees, so an eval-set identifier here would steer every task. Task-agnostic examples only.
func TestSystemPromptCarriesNoEvalSetSpecifics(t *testing.T) {
	banned := []string{
		"grpcio", "kv-store", "kv_store", "pmars", "flashpaper", "rave.red", "extract-elf", "extract.js",
		"ocaml", "cobol", "compcert", "corewars", "caffe", "cifar", "qemu", "fasttext", "sparql",
		"grpc", "_pb2", ".proto", "gcov", "opam", "valgrind", "sqlite",
		"208", "377", "Cleaned up",
	}
	for _, b := range banned {
		if strings.Contains(systemPrompt, b) {
			t.Errorf("systemPrompt leaks eval-set-specific token %q — use a task-agnostic example", b)
		}
	}
}
