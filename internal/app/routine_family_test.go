package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRoutineFamilyRunsOnceAndReplaysPolicies(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, "scope/proof.go", []byte("first\nsecond\n"))
	if _, err := r.host.Load(context.Background(), filepath.Join("..", "..", "plugins", "explore")); err != nil {
		t.Fatal(err)
	}
	model := &scriptLLM{steps: [][]port.ProviderEvent{
		asideText("proof.go:1\nproof.go:2"),
		asideText("proof.go:1"),
		asideText("proof.go:2"),
	}}
	r.a.llm = model
	args := map[string]any{
		"question": "investigate this internal request", "directory": "scope", "client": "web", "action": "recovery",
		"request_limit": 2, "timeout": 15,
		"handoffs": []map[string]string{{"label": "first private input", "evidence": "proof.go:1"}, {"label": "second", "evidence": "proof.go:2"}},
	}
	run := func(id string, wantPolicy string, calls int) string {
		t.Helper()
		out, asks, failed := r.call(t, id, "routine_trace_path", args, "allow")
		if failed || len(asks) != 1 || !strings.Contains(out, wantPolicy) || model.call != calls {
			t.Fatalf("%s: %q, asks=%v, calls=%d", id, out, asks, model.call)
		}
		match := regexp.MustCompile(`call-path run ([a-f0-9]+)`).FindStringSubmatch(out)
		if match == nil {
			t.Fatalf("missing execution ID: %q", out)
		}
		return match[1]
	}
	first := run("first", "whole-v1", 1)
	if err := r.command(t, "routine-trace-policy", "replay", first); err != nil {
		t.Fatal(err)
	}
	if model.call != 1 || !strings.Contains(r.logs.text(), "handoff-v1: handoff, handoff") {
		t.Fatalf("replay should compare choices without model work: %s", r.logs.text())
	}
	if err := r.command(t, "routine-trace-policy", "use", "handoff-v1"); err != nil {
		t.Fatal(err)
	}
	args["client"], args["action"] = "vscode", "cancellation"
	second := run("second", "handoff-v1", 3)
	raw, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "routines", "client-daemon-call-trace", "runs", second+".json"))
	if err != nil || strings.Contains(string(raw), "private input") || strings.Contains(string(raw), "investigate this internal") {
		t.Fatalf("run persisted request text or missing: %q %v", raw, err)
	}
	var record struct {
		Client, Action, Phase string
		Handoffs              []string
		Metrics               struct {
			Calls int `json:"model_calls"`
		}
	}
	if json.Unmarshal(raw, &record) != nil || record.Client != "vscode" || record.Action != "cancellation" ||
		record.Phase != "completed" || record.Metrics.Calls != 2 ||
		strings.Join(record.Handoffs, ",") != "verified_reference,verified_reference" {
		t.Fatalf("execution record: %s", raw)
	}

	if err := r.command(t, "routine-lesson", "promote", first, first); err == nil {
		t.Fatal("the same run counted as independent evidence")
	}
	if err := r.command(t, "routine-lesson", "promote", first, second); err != nil {
		t.Fatal(err)
	}
	discovery, _, failed := r.call(t, "families", "routine_task_families", map[string]any{}, "allow")
	if failed || !strings.Contains(discovery, `"id":"implementation-evidence","lesson":"cite-executable-handoff"`) || model.call != 3 {
		t.Fatalf("discovery should expose the promoted lesson without more model work: %s", discovery)
	}

	args["request_limit"] = 1
	model.steps = append(model.steps, asideText("proof.go:1"))
	out, _, failed := r.call(t, "budget", "routine_trace_path", args, "allow")
	if !failed || model.call != 4 || !strings.Contains(out, "budget exhausted") {
		t.Fatalf("candidate exceeded the shared budget or masked its failure: %s (calls %d)", out, model.call)
	}
	if err := r.command(t, "routine-trace-policy", "rollback"); err != nil {
		t.Fatal(err)
	}
	if err := r.command(t, "routine-trace-policy"); err != nil || !strings.HasSuffix(strings.TrimSpace(r.logs.text()), "whole-v1 (available: whole-v1, handoff-v1)") {
		t.Fatalf("rollback: %v %s", err, r.logs.text())
	}
	args["family"], args["action"] = "implementation-evidence", "claim"
	args["request_limit"] = 2
	model.steps = append(model.steps, asideText("proof.go:1\nproof.go:2"))
	third := run("transfer", "whole-v1", 5)
	raw, err = os.ReadFile(filepath.Join(r.s.Workdir, ".magi", "routines", "client-daemon-call-trace", "runs", third+".json"))
	if err != nil || !strings.Contains(string(raw), `"family":"implementation-evidence"`) ||
		!strings.Contains(string(raw), `"lesson":"cite-executable-handoff"`) {
		t.Fatalf("transfer record: %s (%v)", raw, err)
	}

}

func TestRoutineFamilyPairedPolicyComparisonAndAdoption(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, "scope/proof.go", []byte("first\nsecond\n"))
	dir := ".magi/routines/client-daemon-call-trace/"
	r.put(t, dir+"policy-tasks.json", []byte(`{"tasks":[{"id":"stop","split":"holdout","protected":true,"input":{
		"question":"trace Stop","directory":"scope","client":"vscode","action":"cancellation","request_limit":2,"timeout":10,
		"handoffs":[{"label":"first","evidence":"proof.go:1"},{"label":"second","evidence":"proof.go:2"}]
	}}]}`))
	if _, err := gitRun(context.Background(), r.s.Workdir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := commitAll(context.Background(), r.s.Workdir, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.host.Load(context.Background(), filepath.Join("..", "..", "plugins", "explore")); err != nil {
		t.Fatal(err)
	}
	model := &scriptLLM{steps: [][]port.ProviderEvent{
		asideText("proof.go:1"), asideText("proof.go:1"), asideText("proof.go:2"),
	}}
	r.a.llm = model
	input := map[string]any{"split": "holdout"}
	if out, _, failed := r.call(t, "frozen", "routine_compare_trace_policy", input, "allow"); !failed || !strings.Contains(out, "freeze current") {
		t.Fatalf("unfrozen comparison: %s", out)
	}
	if err := r.command(t, "routine-trace-policy", "freeze"); err != nil {
		t.Fatal(err)
	}
	out, asks, failed := r.call(t, "compare", "routine_compare_trace_policy", input, "allow")
	var report struct {
		ID, Tree, Phase string
		Solved          int
		Regressed       int
		Rows            []struct {
			Active, Candidate string
			CandidateRun      string `json:"candidate_run"`
		}
	}
	if failed || len(asks) != 1 || model.call != 3 || json.Unmarshal([]byte(out), &report) != nil ||
		report.Tree == "" || report.Phase != "completed" || report.Solved != 1 || report.Regressed != 0 ||
		len(report.Rows) != 1 || report.Rows[0].Active != "partial" || report.Rows[0].Candidate != "completed" {
		t.Fatalf("paired holdout: %s, calls=%d, approvals=%d", out, model.call, len(asks))
	}
	if err := r.command(t, "routine-trace-policy", "replay", report.Rows[0].CandidateRun); err == nil {
		t.Fatal("holdout run must stay out of replay")
	}
	if err := r.command(t, "routine-trace-policy", "adopt", report.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.command(t, "routine-trace-policy"); err != nil || !strings.HasSuffix(strings.TrimSpace(r.logs.text()), "handoff-v1 (available: whole-v1, handoff-v1)") {
		t.Fatalf("adoption: %v %s", err, r.logs.text())
	}
}
