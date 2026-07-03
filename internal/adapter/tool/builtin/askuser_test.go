package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// ask_user walks the questions in order, labels each answer, and degrades
// gracefully when no interactive user exists or the input is malformed.
func TestAskUser(t *testing.T) {
	var asked []string
	env := port.ToolEnv{AskUser: func(q string, opts []string) (string, error) {
		asked = append(asked, q)
		return opts[1], nil
	}}
	res, _ := AskUser{}.Execute(context.Background(), json.RawMessage(
		`{"questions":[{"question":"approach?","options":["A","B"]},{"question":"scope?","options":["small","big"]}]}`), env)
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if res.IsError || !strings.Contains(out, "Q: approach?\nA: B") || !strings.Contains(out, "Q: scope?\nA: big") {
		t.Fatalf("labeled answers missing: err=%v out=%q", res.IsError, out)
	}
	if len(asked) != 2 {
		t.Fatalf("questions should be asked in order, got %v", asked)
	}

	// Dismissed pick ("") is surfaced as an explicit no-pick, not an empty answer.
	env.AskUser = func(string, []string) (string, error) { return "", nil }
	res, _ = AskUser{}.Execute(context.Background(), json.RawMessage(
		`{"questions":[{"question":"q","options":["A","B"]}]}`), env)
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
}
