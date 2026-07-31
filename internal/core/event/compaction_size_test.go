package event

import (
	"strings"
	"testing"
)

// Reduction clamps a negative saving to zero, which is right for a number called "freed" and wrong
// for the sentence built out of it. A compaction whose summary came out LARGER than what it
// replaced rendered as "(−0, −0%)" on both surfaces — which reads as "nothing was freed" when the
// truth is that the context grew.
//
// It is reachable through the manual /compact: a user who folds a short conversation can get a
// model-written brief longer than the exchange it replaces.
func TestASummaryLargerThanWhatItReplacedSaysSo(t *testing.T) {
	d := CompactionData{TokensBefore: 2000, TokensAfter: 3500}
	got := d.SizeNote()
	if strings.Contains(got, "−0") || strings.Contains(got, "-0") {
		t.Errorf("a context that GREW is reported as nothing freed: %q", got)
	}
	if !strings.Contains(got, "1500") {
		t.Errorf("the note does not say how much it grew: %q", got)
	}
	if !strings.Contains(got, "LARGER") {
		t.Errorf("the direction is not stated: %q", got)
	}
	// The freed number itself still clamps — a caller asking "how much was freed" gets zero, which
	// is true. Only the sentence changed.
	if freed, _ := d.Reduction(); freed != 0 {
		t.Errorf("Reduction() = %d freed, want the clamp to stay at 0", freed)
	}
}

// The ordinary case is untouched, including the rounding.
func TestAnOrdinaryCompactionReadsAsBefore(t *testing.T) {
	for _, c := range []struct {
		before, after int
		want          string
	}{
		{100000, 20000, "−80000, −80%"},
		{5000, 5000, "−0, −0%"},   // no change is not growth
		{1000, 0, "−1000, −100%"}, // everything folded away
		{0, 0, "−0, −0%"},         // nothing measured either side
	} {
		d := CompactionData{TokensBefore: c.before, TokensAfter: c.after}
		if got := d.SizeNote(); got != c.want {
			t.Errorf("%d→%d: SizeNote() = %q, want %q", c.before, c.after, got, c.want)
		}
	}
}

// An unmeasured before with a measured after is growth, not a quiet zero: it is the one case where
// "0 → 500" and "nothing was freed" would both be printed, and only one of them is true.
func TestAnUnmeasuredBeforeIsNotAQuietZero(t *testing.T) {
	got := CompactionData{TokensBefore: 0, TokensAfter: 500}.SizeNote()
	if !strings.Contains(got, "500") || !strings.Contains(got, "LARGER") {
		t.Errorf("0→500 reads as %q", got)
	}
}
