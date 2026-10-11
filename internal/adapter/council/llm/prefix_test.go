package llm

import (
	"context"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

type preparingProvider struct {
	panelProvider
	prepared []port.ChatRequest
}

func (p *preparingProvider) PrefillPrefix(_ context.Context, r port.ChatRequest) error {
	p.prepared = append(p.prepared, r)
	return nil
}
func TestPreparationAndPanelShareExactEvidenceHead(t *testing.T) {
	p := &preparingProvider{}
	p.reply = func(n int) string {
		if n == 0 {
			return replyWith([3]string{"Melchior", "correctness", "done"}, [3]string{"Balthasar", "verification", "done"}, [3]string{"Casper", "completeness", "done"})
		}
		return `{"decision":"done","rationale":"holds"}`
	}
	var route string
	c := New(func(name string) port.LLMProvider { route = name; return p }, "m")
	req := port.DeliberationRequest{EvidencePrefix: "# History\nA\nB", Task: "verify", Declared: true, Members: council.DefaultMembers()}
	for i := range req.Members {
		req.Members[i].Provider = "judges"
		req.Members[i].Model = "judge-model"
	}
	if err := c.Prepare(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(p.prepared) != 1 {
		t.Fatalf("same backend/model prepared %d times", len(p.prepared))
	}
	if _, err := c.Deliberate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	warm := p.prepared[0]
	judge := p.reqs[0]
	if route != "judges" || warm.Model != "judge-model" || judge.Model != warm.Model {
		t.Fatal("preparation and judgment used different backend/model")
	}
	if warm.System != judge.System || !strings.HasPrefix(judge.Messages[0].Parts[0].Text, warm.Messages[0].Parts[0].Text) {
		t.Fatal("warm prefix differs from actual judgment")
	}
	if len(p.reqs) < 2 || p.reqs[1].Messages[0].Parts[0].Text != judge.Messages[0].Parts[0].Text {
		t.Fatal("closing call rewrote first judgment request")
	}
}
