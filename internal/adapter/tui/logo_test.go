package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/version"
)

// The splash renders the wordmark and version, and biases it below the vertical
// center so it sits just above the input prompt (blank rows below < blank rows
// above). Guards against reverting to a dead-center splash that floats far from
// the prompt.
func TestSplashViewBottomBiased(t *testing.T) {
	applyTheme(true)
	const w, h = 60, 30
	rows := strings.Split(splashView(w, h), "\n")
	if len(rows) != h {
		t.Fatalf("splash height = %d, want %d", len(rows), h)
	}
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	above := 0
	for _, r := range rows {
		if !blank(r) {
			break
		}
		above++
	}
	below := 0
	for i := len(rows) - 1; i >= 0; i-- {
		if !blank(rows[i]) {
			break
		}
		below++
	}
	if above == 0 || below == 0 {
		t.Fatalf("expected blank padding on both sides, above=%d below=%d", above, below)
	}
	if below >= above {
		t.Errorf("splash should be bottom-biased (below=%d < above=%d)", below, above)
	}
	if !strings.Contains(splashView(w, h), version.String()) {
		t.Errorf("splash should include the build version %q", version.String())
	}
}
