package app

import (
	"testing"
	"time"
)

// attemptCap: base when unobserved; K×avg inside the elastic band; clamped to
// [base/2, base×3] outside it; per-model samples win over the global fallback.
func TestAttemptCapElastic(t *testing.T) {
	base := 5 * time.Minute
	a := &App{cfg: Config{SubagentTimeout: base}}

	if got := a.attemptCap("m"); got != base {
		t.Fatalf("no samples: cap should be the base, got %v", got)
	}

	// Nominal speed: 30s avg → 6×30s = 3m, inside [2.5m, 15m] → used as-is.
	for i := 0; i < 4; i++ {
		a.llmLat.record("m", 30*time.Second)
	}
	if got := a.attemptCap("m"); got != 3*time.Minute {
		t.Fatalf("nominal: want 3m, got %v", got)
	}

	// Fast model: 5s avg → 30s, below base/2 → clamped up to 2.5m.
	fast := &App{cfg: Config{SubagentTimeout: base}}
	fast.llmLat.record("f", 5*time.Second)
	if got := fast.attemptCap("f"); got != base/2 {
		t.Fatalf("fast: want %v (base/2 floor), got %v", base/2, got)
	}

	// Slow model: 5m avg → 30m, above 3×base → clamped down to 15m.
	slow := &App{cfg: Config{SubagentTimeout: base}}
	slow.llmLat.record("s", 5*time.Minute)
	if got := slow.attemptCap("s"); got != 3*base {
		t.Fatalf("slow: want %v (3×base ceiling), got %v", 3*base, got)
	}

	// Unknown model falls back to the cross-model average (better prior than base
	// when the gateway's overall speed is known).
	if got := slow.attemptCap("never-seen"); got != 3*base {
		t.Fatalf("fallback: want the global-average cap %v, got %v", 3*base, got)
	}
}

// The latency ring keeps only the newest llmLatWindow samples, so a model that
// sped up (or slowed down) converges to its recent behavior.
func TestLLMLatencyRingWindow(t *testing.T) {
	var l llmLatencies
	for i := 0; i < llmLatWindow; i++ {
		l.record("m", time.Hour) // old, slow samples
	}
	for i := 0; i < llmLatWindow; i++ {
		l.record("m", time.Second) // recent, fast samples push the old ones out
	}
	if got := l.avg("m"); got != time.Second {
		t.Fatalf("ring should hold only the recent samples: avg %v, want 1s", got)
	}
}

// record rejects degenerate samples (empty model, non-positive duration) and
// tolerates concurrent record/avg (the loop and supervisor run in parallel).
func TestLLMLatencyGuardsAndConcurrency(t *testing.T) {
	var l llmLatencies
	l.record("", time.Second)
	l.record("m", 0)
	l.record("m", -time.Second)
	if got := l.avg("m"); got != 0 {
		t.Fatalf("degenerate samples must be dropped: avg %v, want 0", got)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			l.record("m", time.Second)
		}
	}()
	for i := 0; i < 500; i++ {
		l.avg("m")
	}
	<-done
	if got := l.avg("m"); got != time.Second {
		t.Fatalf("avg after concurrent writes: %v, want 1s", got)
	}
}

// SetSubagentTimeout mutates the base at runtime and rejects absurdly small
// values; the elastic band follows the new base.
func TestSetSubagentTimeout(t *testing.T) {
	a := &App{cfg: Config{SubagentTimeout: 5 * time.Minute}}
	if err := a.SetSubagentTimeout(time.Second); err == nil {
		t.Fatal("1s should be rejected (min 30s)")
	}
	if err := a.SetSubagentTimeout(10 * time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := a.attemptCap("m"); got != 10*time.Minute {
		t.Fatalf("cap should track the new base: got %v", got)
	}
}
