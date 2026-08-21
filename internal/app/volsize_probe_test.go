package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// TestProbeVolatileSectionSizes is a measurement, not an assertion: it replays a real trial's
// event log and reports how many bytes each section of the per-step block costs at the end of
// the run. Skipped unless MAGI_PROBE_EVENTS names a magi-events.jsonl.
//
// The block was assumed small for a year (8b5f8c47) and measured at 4,742-7,304 tokens per call
// only after a paid campaign. Which SECTION carries that is the next thing not to assume.
func TestProbeVolatileSectionSizes(t *testing.T) {
	path := os.Getenv("MAGI_PROBE_EVENTS")
	if path == "" {
		t.Skip("set MAGI_PROBE_EVENTS to a trial's magi-events.jsonl")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var evs []event.Event
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e event.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		evs = append(evs, e)
	}
	a := &App{}
	rs := a.runState(evs)
	t.Logf("events=%d  runState=%d bytes (~%d tokens)", len(evs), len(rs), len(rs)/4)
	for _, line := range strings.Split(rs, "\n") {
		if len(line) > 0 {
			head := line
			if len(head) > 60 {
				head = head[:60]
			}
			t.Logf("   %6d B  %s", len(line), head)
		}
	}
}
