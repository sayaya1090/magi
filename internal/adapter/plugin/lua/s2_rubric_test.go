package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// recAnalyzer answers in order and keeps every request, so a test can see WHAT each judging
// stage was shown and in which order.
type recAnalyzer struct {
	mu      sync.Mutex
	replies []string
	systems []string
	texts   []string
}

func (a *recAnalyzer) Analyze(_ context.Context, system, text, _ string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.systems, a.texts = append(a.systems, system), append(a.texts, text)
	i := len(a.texts) - 1
	if i >= len(a.replies) {
		i = len(a.replies) - 1
	}
	return a.replies[i], nil
}

const rubricReply = `{"criteria":["says whether each object can sit on its own layer","keeps the existing ObjectState split"]}`

func rubricLab(t *testing.T, steps []port.ChildStep, replies ...string) (string, *recAnalyzer) {
	t.Helper()
	an := &recAnalyzer{replies: replies}
	r := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: r, DataDir: t.TempDir(),
		Runtime: RuntimeInfo{Workdir: t.TempDir()}, Analyzer: an, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	compare, _ := r.Get("s2_compare")
	n := 0
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		n++ // baseline runs first, so REPORT-1 is the baseline and REPORT-2 the candidate
		return port.SpawnResult{Trial: true, Snapshot: "same-tree", Model: "executor", Provider: "local",
			SessionID: fmt.Sprint(n), Steps: 1, Text: fmt.Sprintf("REPORT-%d", n), Workspace: "/private/trial"}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) { return steps, nil }}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"can each object get its own layer?","guidance":"read the design document before answering"}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("experiment failed: %.500s", res.Content)
	}
	return string(res.Content), an
}

var ranCheck = []port.ChildStep{{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Output: "exit 0\nPASS", OutputBytes: 11}}

// A question answered in words is still an answer to compare. Measured on aitoon: two of seven
// automatic runs ended "no recorded tool calls" before anything was compared, both on design
// questions. The run is now judged as text, and the report says it was.
func TestS2AToolFreeRunIsComparedAsText(t *testing.T) {
	body, _ := rubricLab(t, []port.ChildStep{}, rubricReply,
		`{"preferred":"candidate","observed_effect":"helped","reason":"covers both criteria"}`)
	if !strings.Contains(body, `"status":"compared"`) || !strings.Contains(body, `"text_only":true`) {
		t.Fatalf("a tool-free run was not compared as text: %.600s", body)
	}
}

// The yardstick is written from the request alone, before either answer is seen, and kept.
func TestS2CriteriaAreWrittenBeforeTheRunsAreSeen(t *testing.T) {
	body, an := rubricLab(t, ranCheck, rubricReply,
		`{"preferred":"candidate","observed_effect":"helped","reason":"covers both criteria"}`)
	if len(an.texts) < 3 {
		t.Fatalf("want a criteria call and two judgments, got %d analyzer calls", len(an.texts))
	}
	first := an.texts[0]
	if !strings.Contains(first, "can each object get its own layer?") || strings.Contains(first, "REPORT-") || strings.Contains(first, "go test") {
		t.Fatalf("the criteria call saw the executions (or not the request): %.400s", first)
	}
	if !strings.Contains(body, "keeps the existing ObjectState split") {
		t.Fatalf("the criteria were not kept in the report: %.600s", body)
	}
	if !strings.Contains(an.texts[1], "keeps the existing ObjectState split") {
		t.Fatalf("the judge was not given the criteria: %.400s", an.texts[1])
	}
	if !strings.Contains(body, `"preferred":"candidate"`) {
		t.Fatalf("two agreeing judgments did not stand: %.600s", body)
	}
}

// The judge sees the two executions in both orders. A verdict that follows the position rather
// than the evidence flips with it, and is then recorded as inconclusive.
func TestS2OrderSwappedJudgmentsMustAgree(t *testing.T) {
	body, an := rubricLab(t, ranCheck, rubricReply,
		`{"preferred":"candidate","observed_effect":"helped","reason":"FIRST-ORDER"}`,
		`{"preferred":"baseline","observed_effect":"hurt","reason":"SWAPPED-ORDER"}`)
	if len(an.texts) < 3 {
		t.Fatalf("want three analyzer calls, got %d", len(an.texts))
	}
	b1, c1 := strings.Index(an.texts[1], "REPORT-1"), strings.Index(an.texts[1], "REPORT-2")
	b2, c2 := strings.Index(an.texts[2], "REPORT-1"), strings.Index(an.texts[2], "REPORT-2")
	if b1 < 0 || c1 < 0 || b2 < 0 || c2 < 0 || (b1 < c1) == (b2 < c2) {
		t.Fatalf("the second judgment did not see the executions in the other order: %d/%d then %d/%d", b1, c1, b2, c2)
	}
	var r struct {
		Judgment  struct{ Preferred, Observed_effect string }
		Judgments struct{ First, Swapped struct{ Reason string } }
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.Judgment.Preferred != "inconclusive" {
		t.Fatalf("disagreeing judgments produced a verdict: %+v", r.Judgment)
	}
	if r.Judgments.First.Reason != "FIRST-ORDER" || r.Judgments.Swapped.Reason != "SWAPPED-ORDER" {
		t.Fatalf("both judgments were not kept: %+v", r.Judgments)
	}
}
