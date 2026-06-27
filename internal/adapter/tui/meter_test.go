package tui

import (
	"strings"
	"testing"
	"time"
)

func TestFmtDur(t *testing.T) {
	cases := map[time.Duration]string{
		47 * time.Second:                 "47s",
		0:                                "0s",
		(3*time.Minute + 49*time.Second): "3m49s",
		(2 * time.Minute):                "2m00s",
		(1*time.Hour + 2*time.Minute):    "1h02m",
		(1*time.Hour + 2*time.Minute + 30*time.Second): "1h02m",
	}
	for d, want := range cases {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestHumanTokens(t *testing.T) {
	cases := map[int]string{0: "0", 847: "847", 1000: "1.0k", 10400: "10.4k", 1234567: "1.2M"}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTurnMeter(t *testing.T) {
	// Both tokens present.
	got := turnMeter(3*time.Minute+49*time.Second, 28100, 10400)
	for _, w := range []string{"3m49s", "↑28.1k", "↓10.4k"} {
		if !strings.Contains(got, w) {
			t.Errorf("turnMeter missing %q: %q", w, got)
		}
	}
	// No usage reported → time only, no arrows.
	if got := turnMeter(5*time.Second, 0, 0); got != "5s" {
		t.Errorf("turnMeter with no tokens = %q, want %q", got, "5s")
	}
	// Output only.
	if got := turnMeter(time.Second, 0, 200); !strings.Contains(got, "↓200") || strings.Contains(got, "↑") {
		t.Errorf("turnMeter output-only = %q", got)
	}
}
