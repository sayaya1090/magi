package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/port"
)

// ask_user walks the questions in order, labels each answer, and degrades
// gracefully when no interactive user exists or the input is malformed.
func TestAskUser(t *testing.T) {
	var asked []string
	var grounds [][]report.Filled
	var placed []string
	env := port.ToolEnv{AskUser: func(q port.Question) (string, error) {
		asked = append(asked, q.Text)
		grounds = append(grounds, q.Grounds)
		placed = append(placed, fmt.Sprintf("%d/%d", q.Index, q.Total))
		return q.Options[1], nil
	}}
	// Every question now carries the report the person decides on. The default contract applies
	// here: no LoadSkill is wired, so there is no decision-report skill to read.
	full := func(q, a, b string) string {
		return `{"question":"` + q + `","options":["` + a + `","` + b + `"],` +
			`"report":{"tried":"ran the tests","stakes":"one is hard to undo","lean":"` + a + `"}}`
	}
	res, _ := AskUser{}.Execute(context.Background(), json.RawMessage(
		`{"questions":[`+full("approach?", "A", "B")+`,`+full("scope?", "small", "big")+`]}`), env)
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if res.IsError || !strings.Contains(out, "Q: approach?\nA: B") || !strings.Contains(out, "Q: scope?\nA: big") {
		t.Fatalf("labeled answers missing: err=%v out=%q", res.IsError, out)
	}
	if len(asked) != 2 {
		t.Fatalf("questions should be asked in order, got %v", asked)
	}
	// Each one says where it stands in the run. A person answering the first of two is entitled to
	// know the second is coming, and only the tool knows how many it is about to ask.
	if strings.Join(placed, " ") != "1/2 2/2" {
		t.Errorf("questions did not say which of how many: %v", placed)
	}

	// Dismissed pick ("") is surfaced as an explicit no-pick, not an empty answer.
	env.AskUser = func(port.Question) (string, error) { return "", nil }
	res, _ = AskUser{}.Execute(context.Background(), json.RawMessage(
		`{"questions":[`+full("q", "A", "B")+`]}`), env)
	_ = json.Unmarshal(res.Content, &out)
	if !strings.Contains(out, "dismissed") {
		t.Fatalf("dismissed answer should be explicit, got %q", out)
	}

	// No interactive user → an instructive error, never a block.
	res, _ = AskUser{}.Execute(context.Background(), json.RawMessage(`{"questions":[{"question":"q","options":["A","B"]}]}`), port.ToolEnv{})
	if !res.IsError {
		t.Fatal("headless ask_user must error out instructively")
	}

	// Fewer than 2 options is not a real choice.
	res, _ = AskUser{}.Execute(context.Background(), json.RawMessage(`{"questions":[{"question":"q","options":["only"]}]}`), env)
	if !res.IsError {
		t.Fatal("a single option should be rejected")
	}

	// The grounds reach the person, in the contract's order rather than the map's.
	if len(grounds) != 2 || len(grounds[0]) != 3 || grounds[0][0].Key != "tried" || grounds[0][2].Key != "lean" {
		t.Fatalf("the report did not arrive in the contract's order: %+v", grounds)
	}
}

// A decision with no grounds is refused, and the refusal says what was wanted.
//
// Refused rather than filled in: a person is about to decide on what is written here, and grounds
// magi invented would be the worst thing to put in front of them. The message carries the whole
// contract so one rejection is enough to learn the shape.
func TestAskUserRefusesADecisionWithNoGrounds(t *testing.T) {
	env := port.ToolEnv{AskUser: func(port.Question) (string, error) {
		t.Fatal("the person was asked before the report was checked")
		return "", nil
	}}
	for _, c := range []struct{ what, args string }{
		{"no report at all", `{"questions":[{"question":"q","options":["A","B"]}]}`},
		{"a section missing", `{"questions":[{"question":"q","options":["A","B"],` +
			`"report":{"tried":"x","stakes":"y"}}]}`},
		{"a section blanked", `{"questions":[{"question":"q","options":["A","B"],` +
			`"report":{"tried":"x","stakes":"y","lean":"   "}}]}`},
	} {
		res, _ := AskUser{}.Execute(context.Background(), json.RawMessage(c.args), env)
		var out string
		_ = json.Unmarshal(res.Content, &out)
		if !res.IsError {
			t.Errorf("%s was accepted", c.what)
			continue
		}
		if !strings.Contains(out, "lean") || !strings.Contains(out, "which one you would pick") {
			t.Errorf("%s: the refusal does not say what is wanted: %q", c.what, out)
		}
	}
}

// The shape comes from the skill, so an operator who wants different sections gets them — and a
// companion with its own copy in its workspace gets its own. Nothing here knows about tiers; it
// reads whatever LoadSkill hands back, which is where the tiering already lives.
func TestTheReportsShapeComesFromTheSkill(t *testing.T) {
	env := port.ToolEnv{
		LoadSkill: func(name string) (string, bool) {
			if name != report.SkillName {
				return "", false
			}
			return "Some prose about how to write one.\n\n## sections\n" +
				"- blast: what breaks if this is wrong\n- rollback: how to undo it\n", true
		},
		AskUser: func(port.Question) (string, error) { return "A", nil },
	}
	res, _ := AskUser{}.Execute(context.Background(), json.RawMessage(
		`{"questions":[{"question":"q","options":["A","B"],"report":{"tried":"x","stakes":"y","lean":"z"}}]}`), env)
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if !res.IsError {
		t.Fatal("the default sections were accepted while the skill asked for others")
	}
	if !strings.Contains(out, "blast") || !strings.Contains(out, "rollback") {
		t.Errorf("the refusal did not name the skill's sections: %q", out)
	}
}
