package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// thinker answers with reasoning, and optionally with an actual reply after it.
type thinker struct {
	thought string
	say     string
}

func (p thinker) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: p.thought}
	if p.say != "" {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: p.say}
	}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A member that answers with NOTHING BUT reasoning must still leave an account of itself.
//
// This is the whole case. Such a member is recorded silent — "a verdict nobody gave" — and before
// the thought travelled, every surface drew that as a considered shrug: three seats, one of them
// saying "no answer came back", over thousands of characters the model had actually produced. The
// operator's next move differs completely between "the member declined" and "the member wrote at
// length and none of it was an answer", and nothing on any screen could tell them apart.
func TestAMemberThatOnlyThoughtStillSaysWhatItThought(t *testing.T) {
	const musing = "The report claims the tests pass. I cannot find the run. Without it I have no way to"
	c := New(only(thinker{thought: musing}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range d.Verdicts {
		if !v.Silent {
			t.Errorf("%s: 답이 없었는데 답이 있다고 한다", v.Member)
		}
		if v.Member == d.Verdicts[0].Member && !strings.Contains(v.Thought, musing) {
			t.Errorf("%s: 생각이 안 실렸다 — %q", v.Member, v.Thought)
		}
	}

	// And it is never mistaken for the verdict itself: the reasoning here is a well-formed reply,
	// so a leak into the parser would show up as a real vote rather than an abstain.
	c = New(only(thinker{thought: `{"decision":"done","confidence":1,"rationale":"all good"}`}), "m")
	d, _ = c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x"})
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain || !v.Silent {
			t.Fatalf("%s: 생각이 표가 됐다 — %s (silent=%v)", v.Member, v.Decision, v.Silent)
		}
	}
	if d.Decision == council.Done {
		t.Fatal("아무도 답을 안 했는데 카운슬이 done 을 냈다 — 생각이 집계에 샜다")
	}
}

// A member that DID answer carries its thinking beside the answer, not instead of it.
func TestAnAnsweringMemberKeepsBothItsAnswerAndItsThinking(t *testing.T) {
	// ⚠ One member, deliberately. Above two the council batches every seat into ONE panel call
	// (`samePanelBackend`), and there the reasoning belongs to the panel rather than to a member —
	// it is attached only where nothing readable came back. This is the per-member path, where
	// the thinking is that member's own.
	c := New(only(thinker{
		thought: "weighing the two readings",
		say:     `{"decision":"done","confidence":0.9,"rationale":"looks complete"}`,
	}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x",
		Members: []council.Member{{Name: "Melchior", Lens: "correctness"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Done || v.Silent {
			t.Fatalf("%s: 답이 왔는데 %s(silent=%v)", v.Member, v.Decision, v.Silent)
		}
		if v.Rationale != "looks complete" {
			t.Errorf("%s: 답이 생각에 덮였다 — %q", v.Member, v.Rationale)
		}
		if v.Thought != "weighing the two readings" {
			t.Errorf("%s: 생각을 버렸다 — %q", v.Member, v.Thought)
		}
	}
}

func TestLongThoughtRemainsComplete(t *testing.T) {
	huge := strings.Repeat("검증 기록을 확인하고 판단한다. ", 4000)
	c := New(only(thinker{thought: huge}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "do x",
		Members: []council.Member{{Name: "Melchior", Lens: "correctness"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Verdicts[0].Thought; got != strings.TrimSpace(huge) {
		t.Fatalf("thought truncated: got %d bytes, want %d", len(got), len(strings.TrimSpace(huge)))
	}
}

// scripted answers with reasoning AND a reply, varying both by whether this is the rebuttal round.
type scripted struct {
	reply func(name string, rebuttal bool) (thought, say string)
}

func (p scripted) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	rebuttal := false
	for _, m := range r.Messages {
		for _, part := range m.Parts {
			if strings.Contains(part.Text, "Council disagreement") {
				rebuttal = true
			}
		}
	}
	name := "Melchior"
	for _, n := range []string{"Balthasar", "Casper"} {
		if strings.Contains(r.System, "You are "+n) {
			name = n
		}
	}
	thought, say := p.reply(name, rebuttal)
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: thought}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: say}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A debate round rebuilds the verdict from scratch, and a field assigned only in the FIRST place a
// verdict is built vanishes for every member the moment a round happens.
//
// The tree has paid for this exact shape before: `cite` was assigned in `poll` and forgotten in
// `pollRebut`, so every member's grounds disappeared after a debate — and "no grounds given" and
// "grounds discarded in transit" look identical on screen.
func TestADebateRoundDoesNotThrowAwayWhatTheMemberThought(t *testing.T) {
	c := New(only(scripted{reply: func(name string, rebuttal bool) (string, string) {
		if rebuttal {
			return "their reading of the spec is better than mine",
				`{"decision":"done","rationale":"peers are right"}`
		}
		if name == "Melchior" { // the dissent that makes it a split, so a debate round runs
			return "the report does not mention the migration",
				`{"decision":"continue","rationale":"incomplete"}`
		}
		return "the suite covers it", `{"decision":"done","rationale":"tests pass"}`
	}}), "m")
	d, _ := c.Deliberate(context.Background(), port.DeliberationRequest{
		Round: 1, Task: "do x", Rule: council.RuleMajority, Debate: true,
		Members: []council.Member{
			{Name: "Melchior", Lens: "correctness"},
			{Name: "Balthasar", Lens: "verification", Model: "other"},
			{Name: "Casper", Lens: "completeness", Model: "third"},
		},
	})
	for _, v := range d.Verdicts {
		if v.Thought == "" {
			t.Errorf("%s: 반박 라운드가 생각을 버렸다", v.Member)
		}
		if v.Thought != "their reading of the spec is better than mine" {
			t.Errorf("%s: 반박 뒤인데 첫 라운드의 생각이 서 있다 — %q", v.Member, v.Thought)
		}
	}
}

func TestAnsweringPanelKeepsSharedThinkingOnce(t *testing.T) {
	c := New(only(thinker{thought: "shared panel thinking", say: replyWith(
		[3]string{"Melchior", "correctness", "done"},
		[3]string{"Balthasar", "verification", "continue"},
		[3]string{"Casper", "completeness", "done"},
	)}), "m")
	d, err := c.Deliberate(context.Background(), port.DeliberationRequest{Round: 1, Task: "review"})
	if err != nil {
		t.Fatal(err)
	}
	shown := 0
	for _, v := range d.Verdicts {
		if v.Thought != "" {
			shown++
			if !strings.Contains(v.Thought, "shared reasoning") || !strings.Contains(v.Thought, "shared panel thinking") {
				t.Fatalf("panel thought lost or attributed to a member: %q", v.Thought)
			}
		}
	}
	if shown != 1 {
		t.Fatalf("shared thought displayed %d times, want once", shown)
	}
	if d.Decision != council.Done {
		t.Fatalf("reasoning changed votes: %s", d.Decision)
	}
}
