package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

func TestCouncilRequestsQualitativeGroundsAndIgnoresLegacyConfidence(t *testing.T) {
	if strings.Contains(panelSchema, `"confidence":`) {
		t.Fatal("panel still requests an invented numerical rating")
	}
	c := New(only(thinker{say: `{"decision":"continue","confidence":0.99,"rationale":"test was not run","cite":"NO-EVIDENCE","feedback":"run the parser test"}`}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "test the parser", Members: []council.Member{{Name: "M", Lens: "verification"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Verdicts) != 1 || d.Verdicts[0].Confidence != 0 || d.Verdicts[0].Rationale != "test was not run" || d.Verdicts[0].Feedback != "run the parser test" {
		t.Fatalf("qualitative grounds lost or confidence trusted: %+v", d.Verdicts)
	}
}
