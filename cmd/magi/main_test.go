package main

import (
	"testing"
	"time"
)

// mergeStrMap overlays `over` onto `base` (over wins), used to merge a project
// config / hooks over the global one.
func TestMergeStrMap(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	got := mergeStrMap(base, map[string]string{"b": "X", "c": "3"})
	if got["a"] != "1" || got["b"] != "X" || got["c"] != "3" {
		t.Errorf("merge = %v (over should win, base kept)", got)
	}
	// Empty override returns the base untouched.
	if got := mergeStrMap(map[string]string{"k": "v"}, nil); got["k"] != "v" || len(got) != 1 {
		t.Errorf("empty over should keep base: %v", got)
	}
	// nil base + override allocates a new map.
	if got := mergeStrMap(nil, map[string]string{"k": "v"}); got["k"] != "v" {
		t.Errorf("nil base + over = %v", got)
	}
}

func TestEnvDur(t *testing.T) {
	t.Setenv("MAGI_TEST_DUR", "2s")
	if d := envDur("MAGI_TEST_DUR", time.Second); d != 2*time.Second {
		t.Errorf("envDur parsed = %v, want 2s", d)
	}
	if d := envDur("MAGI_TEST_UNSET_DUR", 5*time.Second); d != 5*time.Second {
		t.Errorf("unset → default, got %v", d)
	}
	t.Setenv("MAGI_TEST_BAD_DUR", "notaduration")
	if d := envDur("MAGI_TEST_BAD_DUR", 7*time.Second); d != 7*time.Second {
		t.Errorf("invalid → default, got %v", d)
	}
}
