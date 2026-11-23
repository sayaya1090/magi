package lua

import (
	"testing"
	"time"
)

// A sidecar analysis must outlast the long answers it is asked for. Engram reflections on the magi
// workspace (2026-11-23 daemon log) wrote 10-15k output tokens at about 120 tokens/s and took
// 76-110 s when they finished; under a 2-minute bound every longer one was cut at exactly 2m0s
// and the reflection inbox and dream queue never drained. The bound has to leave room for about
// twice the longest finished answer.
func TestTheAnalyzeBoundOutlastsALongReflection(t *testing.T) {
	const longestSeen = 15214 // output tokens of the longest reflection that finished
	const tokensPerSecond = 120
	need := time.Duration(2*longestSeen/tokensPerSecond) * time.Second
	if analyzeTimeout < need {
		t.Fatalf("analyzeTimeout %v cannot hold twice the longest observed reflection (%v)", analyzeTimeout, need)
	}
}
