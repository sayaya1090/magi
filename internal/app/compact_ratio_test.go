package app

import "testing"

// The ratio reached App only as a hardcoded 0.8: the field existed on app.Config, nothing outside
// this package ever set it, and [limits] had no key for it. Now that it is settable, the defaulting
// has to answer for values a config file can actually contain — a share of the window is only
// meaningful in (0,1], and a number outside that would silently compact on every step (<=0 makes
// the budget 0) or never (>1 puts the trigger past the window).
func TestTheCompactRatioFallsBackOnAValueThatCannotBeAShare(t *testing.T) {
	for _, c := range []struct {
		what string
		in   float64
		want float64
	}{
		{"unset", 0, 0.8},
		{"negative", -0.5, 0.8},
		{"past the whole window", 1.5, 0.8},
		{"the whole window exactly", 1.0, 1.0},
		{"earlier than the default", 0.6, 0.6},
		{"later than the default", 0.92, 0.92},
	} {
		cfg := Config{CompactRatio: c.in}.withDefaults()
		if cfg.CompactRatio != c.want {
			t.Errorf("%s: %v → %v, want %v", c.what, c.in, cfg.CompactRatio, c.want)
		}
	}
}

// The budget the trigger actually compares against, so a changed ratio is visibly a changed
// threshold and not just a stored number.
func TestTheRatioMovesTheCompactionThreshold(t *testing.T) {
	const window = 65536
	for _, c := range []struct {
		ratio  float64
		budget int
	}{
		{0.8, 52428},  // the default
		{0.9, 58982},  // later: more context kept, more to prefill each step
		{0.65, 42598}, // earlier: cheaper steps, less detail in front of the model
	} {
		cfg := Config{CompactRatio: c.ratio}.withDefaults()
		if got := int(float64(window) * cfg.CompactRatio); got != c.budget {
			t.Errorf("ratio %v: budget %d, want %d", c.ratio, got, c.budget)
		}
	}
}
