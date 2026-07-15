package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func waitForArgsJSON(t *testing.T, cond string, timeout, interval int) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"condition": cond, "timeout": timeout, "interval": interval})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWaitForConditionMet: a condition that already succeeds returns promptly with
// an ok result naming the check count, without waiting an interval.
func TestWaitForConditionMet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX /bin/sh true")
	}
	start := time.Now()
	res, err := WaitFor{}.Execute(context.Background(), waitForArgsJSON(t, "true", 30, 5), port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("met condition should be ok, got error: %s", res.Content)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("a met condition returned after %s — should not sleep an interval", d)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "condition met") {
		t.Errorf("result %q missing 'condition met'", s)
	}
}

// TestWaitForTimeout: a condition that never succeeds returns an error after the
// timeout, and emits at least one live progress note along the way.
func TestWaitForTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX /bin/sh false")
	}
	var mu sync.Mutex
	var beats []string
	env := port.ToolEnv{EmitProgress: func(s string) { mu.Lock(); beats = append(beats, s); mu.Unlock() }}

	start := time.Now()
	// timeout 2s, interval 1s → ~2 checks, ≥1 progress beat before the deadline.
	res, err := WaitFor{}.Execute(context.Background(), waitForArgsJSON(t, "false", 2, 1), env)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("unmet condition should be an error, got ok: %s", res.Content)
	}
	if d := time.Since(start); d < 2*time.Second {
		t.Fatalf("returned after %s, before the 2s timeout", d)
	}
	mu.Lock()
	n := len(beats)
	mu.Unlock()
	if n == 0 {
		t.Errorf("expected at least one live progress beat before timeout, got none")
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "not met") {
		t.Errorf("timeout result %q missing 'not met'", s)
	}
}

// TestWaitForCancel: cancelling the context ends the wait promptly (it does not run
// to the full timeout) and reports a cancellation rather than a clean success.
func TestWaitForCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX /bin/sh false")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()

	start := time.Now()
	res, err := WaitFor{}.Execute(ctx, waitForArgsJSON(t, "false", 30, 2), port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("cancel did not cut the wait short: returned after %s", d)
	}
	if !res.IsError {
		t.Fatalf("a cancelled wait should be an error, got ok: %s", res.Content)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "cancelled") {
		t.Errorf("cancel result %q missing 'cancelled'", s)
	}
}

// TestWaitForValidation: an empty condition is rejected without running anything.
func TestWaitForValidation(t *testing.T) {
	res, err := WaitFor{}.Execute(context.Background(), waitForArgsJSON(t, "   ", 0, 0), port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("empty condition should error")
	}
}

// TestWaitForNilProgress: a nil EmitProgress (bare ToolEnv, no observer) must not
// panic — the tool nil-checks before emitting.
func TestWaitForNilProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX /bin/sh false")
	}
	res, err := WaitFor{}.Execute(context.Background(), waitForArgsJSON(t, "false", 2, 1), port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("unmet condition should error")
	}
}
