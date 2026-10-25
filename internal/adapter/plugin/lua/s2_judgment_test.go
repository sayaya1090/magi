package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
)

// A comparison judgment is the last step of an experiment that has already paid for two
// executions. Measured on a real run (aitoon, report f25dc94e…): both arms finished, then the
// whole report ended `incomplete` with "invalid comparison judgment" and the reply itself was
// not kept, so nobody could say afterwards what the model had answered.
func s2JudgmentRun(t *testing.T, reply string) (string, bool) {
	t.Helper()
	_, compare, _ := harnessLab(t, reply)
	n := 0
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		n++
		return port.SpawnResult{Trial: true, Snapshot: "same-tree", Model: "executor", Provider: "local",
			SessionID: fmt.Sprint(n), Steps: 2, Text: "reported", Workspace: "/private/trial"}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Output: "exit 0\nPASS", OutputBytes: 11}}, nil
	}}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"fix parser","guidance":"run the parser check first"}`), env)
	if err != nil {
		t.Fatal(err)
	}
	return string(res.Content), res.IsError
}

func TestS2JudgmentInAFenceOrWithPreambleIsStillAJudgment(t *testing.T) {
	for name, reply := range map[string]string{
		"fenced":   "```json\n{\"preferred\":\"inconclusive\",\"observed_effect\":\"confounded\",\"reason\":\"one pair\"}\n```",
		"preamble": "Here is the judgment:\n{\"preferred\":\"inconclusive\",\"observed_effect\":\"confounded\",\"reason\":\"one pair\"}",
	} {
		t.Run(name, func(t *testing.T) {
			body, isErr := s2JudgmentRun(t, reply)
			if isErr || !strings.Contains(body, `"status":"compared"`) {
				t.Fatalf("a well-formed judgment in its wrapping was thrown away: %s", body)
			}
		})
	}
}

// A long reason is a long reason, not a broken judgment: it is cut on a character boundary.
func TestS2JudgmentWithALongReasonIsKeptShortened(t *testing.T) {
	reason := strings.Repeat("비교 근거가 깁니다. ", 600) // ~16KB of Korean, past the 6000-byte limit
	b, _ := json.Marshal(map[string]string{"preferred": "baseline", "observed_effect": "no observed effect", "reason": reason})
	body, isErr := s2JudgmentRun(t, string(b))
	if isErr || !strings.Contains(body, `"status":"compared"`) {
		t.Fatalf("a long reason failed the whole experiment: %.300s", body)
	}
	var r struct{ Judgment struct{ Reason string } }
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if n := len(r.Judgment.Reason); n == 0 || n > 6000 || !utf8.ValidString(r.Judgment.Reason) {
		t.Fatalf("reason kept at %d bytes", n)
	}
}

// When the reply really is unusable, the report says what it was.
func TestS2InvalidJudgmentKeepsTheReplyItRejected(t *testing.T) {
	body, isErr := s2JudgmentRun(t, `{"preferred":"maybe","observed_effect":"helped","reason":"SENTINEL-reply"}`)
	if !isErr || !strings.Contains(body, "invalid comparison judgment") {
		t.Fatalf("an unusable judgment was accepted: %s", body)
	}
	if !strings.Contains(body, "SENTINEL-reply") {
		t.Fatalf("the rejected reply was not kept for diagnosis: %s", body)
	}
}
