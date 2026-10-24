package app

import (
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func TestOverflowRequestTokensReadsEachBackendsWording(t *testing.T) {
	cases := map[string]int{
		// The exact refusal the claudecode shim relayed (2026-10-24).
		"provider stream: Prompt is too long · the request is ~1035211 tokens (limit 1000000) but this conversation is only ~487532 tokens — the rest is system prompt": 1035211,
		"prompt is too long: 1,035,211 tokens > 1000000 maximum":                                    1035211,
		"This model's maximum context length is 131072 tokens, however you requested 140000 tokens": 140000,
		"context_length_exceeded": 0,
		"connection reset":        0,
	}
	for msg, want := range cases {
		if got := overflowRequestTokens(errors.New(msg)); got != want {
			t.Errorf("%q: got %d, want %d", msg, got, want)
		}
	}
	if overflowRequestTokens(nil) != 0 {
		t.Error("nil error must state no size")
	}
}

func TestCalibratedScaleIsBoundedAndOnlyCorrectsUnderestimates(t *testing.T) {
	if got := calibratedScale(1035211, 487532); got < 2.12 || got > 2.13 {
		t.Errorf("measured ratio: got %v, want ~2.123", got)
	}
	if got := calibratedScale(400, 500); got != 0 {
		t.Errorf("an overestimate must not shrink the keep: got %v", got)
	}
	if got := calibratedScale(1_000_000, 10); got != maxEstimateScale {
		t.Errorf("cap: got %v, want %v", got, maxEstimateScale)
	}
	if calibratedScale(0, 10) != 0 || calibratedScale(10, 0) != 0 {
		t.Error("missing numbers must learn nothing")
	}
}

func TestScaleKeepConvertsRealBudgetIntoEstimateUnits(t *testing.T) {
	if got := scaleKeep(100_000, 0); got != 100_000 {
		t.Errorf("unlearned: got %d", got)
	}
	if got := scaleKeep(100_000, 2); got != 50_000 {
		t.Errorf("scale 2: got %d, want 50000", got)
	}
}

func TestLearnEstimateScaleOnlyRises(t *testing.T) {
	a := newTestApp(t)
	sid := session.SessionID("s_scale")
	a.learnEstimateScale(sid, 300, 100)
	a.learnEstimateScale(sid, 150, 100)
	if got := a.estimateScaleOf(sid); got != 3 {
		t.Errorf("got %v, want 3 (a smaller later ratio must not lower it)", got)
	}
}

func TestObserveEstimateScaleFollowsTheLatestAnsweredRequest(t *testing.T) {
	a := newTestApp(t)
	sid := session.SessionID("s_observe")
	a.observeEstimateScale(sid, 210, 100)
	if got := a.estimateScaleOf(sid); got != 2.1 {
		t.Fatalf("got %v, want 2.1", got)
	}
	// The conversation changed shape (now mostly ASCII): the newest measurement wins, downward too.
	a.observeEstimateScale(sid, 120, 100)
	if got := a.estimateScaleOf(sid); got != 1.2 {
		t.Fatalf("got %v, want 1.2", got)
	}
	// An estimate that ran high clears the correction rather than shrinking the keep below truth.
	a.observeEstimateScale(sid, 90, 100)
	if got := a.estimateScaleOf(sid); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

func TestScaleEstimateAndScaleKeepAreInverse(t *testing.T) {
	if scaleEstimate(1000, 0) != 1000 || scaleEstimate(1000, 1) != 1000 {
		t.Error("an unlearned scale must leave the estimate alone")
	}
	// The measured case: ~487k estimated is ~1,035k on the wire.
	if got := scaleEstimate(487532, calibratedScale(1035211, 487532)); got < 1035000 || got > 1035300 {
		t.Errorf("got %d, want ~1035211", got)
	}
	if got := scaleKeep(scaleEstimate(50000, 2), 2); got != 50000 {
		t.Errorf("round trip: got %d", got)
	}
}
