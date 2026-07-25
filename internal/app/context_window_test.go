package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/model"
)

// TestContextWindowLazyProbe: a seeded model is answered from the registry with
// no probe; an unseeded model returns 0 (unlimited) immediately, kicks off a
// single background probe, and reports the probed window once it lands.
func TestContextWindowLazyProbe(t *testing.T) {
	var probes atomic.Int32
	a := &App{
		probingWindows: map[string]struct{}{},
		cfg: Config{
			Models: model.NewRegistry(),
			ContextWindowProber: func(_ context.Context, _ string) (int, bool) {
				probes.Add(1)
				return 200000, true
			},
		},
	}

	// Seeded: authoritative, no probe.
	if w := a.contextWindow("qwen3-coder:30b"); w != 262144 {
		t.Fatalf("seeded window = %d, want 262144", w)
	}

	// Unseeded: first read is 0 (unlimited) while the probe runs in the background.
	if w := a.contextWindow("mystery:latest"); w != 0 {
		t.Fatalf("unseeded first read = %d, want 0 (unlimited until probed)", w)
	}
	// Poll for the async probe to register the real window.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.contextWindow("mystery:latest") != 200000 {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.contextWindow("mystery:latest"); w != 200000 {
		t.Fatalf("probed window not registered, got %d", w)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("prober should run exactly once, ran %d", got)
	}
}

// TestContextWindowFamilyFallbackWhileProbing: a model with no EXACT seed but a
// known family (e.g. "qwen3-coder:480b-cloud" → "qwen3-coder") must report the
// family window immediately — NOT 0/unlimited — while the exact-window probe runs,
// so ratio compaction isn't disabled at session start for a window we already know.
func TestContextWindowFamilyFallbackWhileProbing(t *testing.T) {
	var probes atomic.Int32
	probed := make(chan struct{})
	a := &App{
		probingWindows: map[string]struct{}{},
		cfg: Config{
			Models: model.NewRegistry(),
			ContextWindowProber: func(_ context.Context, _ string) (int, bool) {
				probes.Add(1)
				<-probed // hold the probe open so the first read is the pre-probe fallback
				return 999999, true
			},
		},
	}
	// qwen3-coder:30b is seeded (262144); the :480b-cloud variant is not, but shares
	// the family, so the first read is the family window, not 0.
	if w := a.contextWindow("qwen3-coder:480b-cloud"); w != 262144 {
		t.Fatalf("family-variant first read = %d, want the family window 262144 (not 0)", w)
	}
	close(probed) // let the probe land
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.contextWindow("qwen3-coder:480b-cloud") != 999999 {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.contextWindow("qwen3-coder:480b-cloud"); w != 999999 {
		t.Fatalf("probed exact window should override the family fallback, got %d", w)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("prober should still run exactly once for the variant, ran %d", got)
	}
}

// TestContextWindowProbeFailNoRetry: a failed probe leaves the window at 0
// (unlimited) and is not retried on subsequent reads.
func TestContextWindowProbeFailNoRetry(t *testing.T) {
	var probes atomic.Int32
	a := &App{
		probingWindows: map[string]struct{}{},
		cfg: Config{
			Models: model.NewRegistry(),
			ContextWindowProber: func(_ context.Context, _ string) (int, bool) {
				probes.Add(1)
				return 0, false
			},
		},
	}
	for i := 0; i < 5; i++ {
		if w := a.contextWindow("nope:1"); w != 0 {
			t.Fatalf("failed-probe window = %d, want 0", w)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("failed probe must not be retried, ran %d times", got)
	}
}
