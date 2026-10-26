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

func harnessLab(t *testing.T, replies ...string) (*Host, port.Tool, port.Tool) {
	t.Helper()
	r := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: r, DataDir: t.TempDir(),
		Runtime: RuntimeInfo{Workdir: t.TempDir()}, Analyzer: &fakeAnalyzer{replies: replies}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	compare, ok := r.Get("s2_compare")
	if !ok {
		t.Fatal("comparison tool not registered")
	}
	read, ok := r.Get("s2_report")
	if !ok {
		t.Fatal("report reader not registered")
	}
	if !port.ToolMetaOf(compare).DefaultOff {
		t.Fatal("experiment must require explicit activation")
	}
	return h, compare, read
}

func TestHarnessLabComparesAndPersistsWithoutSkillPromotion(t *testing.T) {
	_, compare, read := harnessLab(t, `{"preferred":"candidate","observed_effect":"helped","reason":"candidate ran the requested check"}`)
	var specs []port.SpawnSpec
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(ctx context.Context, s port.SpawnSpec) (port.SpawnResult, error) {
		specs = append(specs, s)
		return port.SpawnResult{Trial: true, Snapshot: "same-tree", Model: "executor", Provider: "local",
			SessionID: fmt.Sprint(len(specs)), Steps: 2, Text: "reported", Workspace: "/private/trial"}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Output: "exit 0\nPASS", OutputBytes: 11}}, nil
	}}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"fix parser","guidance":"run the parser check first"}`), env)
	if err != nil || res.IsError {
		t.Fatalf("comparison: %s %v", res.Content, err)
	}
	var report struct {
		ID, Status, Attribution string
		Verified                bool
		Judgment                struct{ Preferred string }
	}
	if err := json.Unmarshal(res.Content, &report); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || !specs[0].Trial || !specs[1].Trial || specs[0].Prompt != specs[1].Prompt || specs[0].System == specs[1].System {
		t.Fatalf("not a matched instruction experiment: %+v", specs)
	}
	if report.Status != "compared" || report.Attribution != "confounded" || report.Verified || report.Judgment.Preferred != "candidate" {
		t.Fatalf("model preference promoted into proof: %+v", report)
	}
	args, _ := json.Marshal(map[string]string{"id": report.ID})
	stored, err := read.Execute(context.Background(), args, env)
	if err != nil || stored.IsError || string(stored.Content) != string(res.Content) {
		t.Fatalf("saved report differs: %s %v", stored.Content, err)
	}
	// The plugin store is shared, but reports are scoped to their workspace.
	env.Workdir = t.TempDir()
	other, _ := read.Execute(context.Background(), args, env)
	if !other.IsError {
		t.Fatal("another workspace could read this experiment")
	}
}

func TestHarnessLabRefusesUnsupportedHostsAndInvalidBudgets(t *testing.T) {
	_, compare, _ := harnessLab(t)
	calls := 0
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		calls++
		return port.SpawnResult{SessionID: "old-host", Steps: 1}, nil
	}}
	for _, args := range []string{`{"task":"t","max_steps":0}`, `{"task":"t","timeout":601}`, `{"task":"t","max_steps":1.5}`} {
		res, _ := compare.Execute(context.Background(), json.RawMessage(args), env)
		if !res.IsError {
			t.Fatal("invalid budget accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid input spent execution budget")
	}
	res, _ := compare.Execute(context.Background(), json.RawMessage(`{"task":"t","guidance":"g"}`), env)
	if !res.IsError || calls != 1 || !strings.Contains(string(res.Content), "upgrade Magi") {
		t.Fatalf("old host accepted: %s calls=%d", res.Content, calls)
	}
}

func TestHarnessLabMissingOrTruncatedEvidenceIsNotAnAdoption(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			_, compare, _ := harnessLab(t, `{"preferred":"candidate","observed_effect":"helped","reason":"looks complete"}`)
			env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
				return port.SpawnResult{Trial: true, Snapshot: "same", SessionID: "s", Steps: 1}, nil
			}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
				if missing {
					return nil, fmt.Errorf("log unavailable")
				}
				return []port.ChildStep{{Name: "read", Output: "partial", OutputBytes: 10000}}, nil
			}}
			res, _ := compare.Execute(context.Background(), json.RawMessage(`{"task":"t","guidance":"g"}`), env)
			if missing {
				if !res.IsError {
					t.Fatalf("missing evidence accepted: %s", res.Content)
				}
			} else if res.IsError || !strings.Contains(string(res.Content), `"preferred":"inconclusive"`) {
				t.Fatalf("truncation became a confident adoption: %s", res.Content)
			}
		})
	}
}

func TestHarnessLabRepeatedHoldoutAdoptionAndWithdrawal(t *testing.T) {
	for _, regression := range []bool{false, true} {
		t.Run(fmt.Sprint(regression), func(t *testing.T) {
			h, compare, read := harnessLab(t, `{"preferred":"candidate","observed_effect":"helped","reason":"pinned checks improved","score":99}`)
			var specs []port.SpawnSpec
			env := port.ToolEnv{Workdir: h.runtime.Workdir}
			env.Spawn = func(_ context.Context, s port.SpawnSpec) (port.SpawnResult, error) {
				specs = append(specs, s)
				candidate := strings.Contains(s.System, "Candidate procedure")
				status := "failed"
				if candidate {
					status = "passed"
				}
				if regression && len(specs) > 4 {
					if candidate {
						status = "failed"
					} else {
						status = "passed"
					}
				}
				return port.SpawnResult{Trial: true, Snapshot: "same", Model: "m", Provider: "p", SessionID: fmt.Sprint(len(specs)), Steps: 1,
					Metrics:      port.RunMetrics{Calls: 1, UsageReports: 1, Input: 10, Output: 2, TokensKnown: true},
					Verification: &port.TrialVerification{Path: s.VerifyPath, Digest: strings.Repeat("a", 64), Status: status}}, nil
			}
			env.ChildSteps = func(context.Context, string) ([]port.ChildStep, error) {
				return []port.ChildStep{{Name: "bash", Output: "ok", OutputBytes: 2}}, nil
			}
			result, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"training","guidance":"inspect tests first","repetitions":2,"verify_path":"verify.sh","holdouts":[{"task":"unseen","verify_path":"other.sh"}],"max_steps":4}`), env)
			if err != nil || result.IsError {
				t.Fatalf("experiment: %s %v", result.Content, err)
			}
			var report struct {
				ID, Adoption string
				Assessment   struct{ State string }
				Cost         struct {
					Calls       int
					TokensKnown bool `json:"tokens_known"`
				}
				Judgment map[string]any
			}
			if err := json.Unmarshal(result.Content, &report); err != nil {
				t.Fatal(err)
			}
			expected := "eligible"
			if regression {
				expected = "regressed"
			}
			// Six executions and three analyses: the criteria, then the judgment in each order.
			if report.Assessment.State != expected || len(specs) != 6 || report.Cost.Calls != 9 || report.Cost.TokensKnown {
				t.Fatalf("assessment/cost: %s", result.Content)
			}
			if _, ok := report.Judgment["score"]; ok {
				t.Fatal("model score persisted")
			}
			// Alternate the repeated and held-out pair order after fixing guidance.
			if !strings.Contains(specs[2].System, "Candidate procedure") || specs[4].Prompt != "unseen" {
				t.Fatalf("order: %+v", specs)
			}
			handled, err := h.DispatchCommand("s2-engine-adopt", []string{report.ID, "reviewed pinned checks"})
			if !handled || (err != nil) != regression {
				t.Fatalf("adoption: %v %v", handled, err)
			}
			args, _ := json.Marshal(map[string]string{"id": report.ID})
			stored, _ := read.Execute(context.Background(), args, env)
			if !regression && !strings.Contains(string(stored.Content), `"adoption":"adopted"`) {
				t.Fatalf("adoption not stored: %s", stored.Content)
			}
			if regression {
				return
			}
			if _, err := h.DispatchCommand("s2-engine-withdraw", []string{report.ID, "later review"}); err != nil {
				t.Fatal(err)
			}
			before := len(specs)
			reuse, _ := json.Marshal(map[string]string{"task": "training", "reuse_report": report.ID})
			denied, _ := compare.Execute(context.Background(), reuse, env)
			if !denied.IsError || len(specs) != before {
				t.Fatal("withdrawn guidance executed")
			}
		})
	}
}

func TestHarnessLabRegistersAdvisoryExperienceValidator(t *testing.T) {
	r := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: r, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()}, Analyzer: &fakeAnalyzer{}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"s2_search", "s2_validate", "s2_checkpoint"} {
		if _, ok := r.Get(name); !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
}

func TestS2EngineRunsBoundedCandidateAlternativesUntilVerifierPass(t *testing.T) {
	r := builtin.NewRegistry()
	analyzer := &fakeAnalyzer{replies: []string{
		`{"hypothesis":"first","guidance":"try first"}`,
		`{"hypothesis":"second","guidance":"try second"}`,
		`{"preferred":"candidate","observed_effect":"helped","reason":"second verifier passed"}`,
	}}
	h := NewHostWithConfig(HostConfig{ToolSink: r, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()}, Analyzer: analyzer, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	compare, _ := r.Get("s2_compare")
	var specs []port.SpawnSpec
	env := port.ToolEnv{Workdir: h.runtime.Workdir, Spawn: func(_ context.Context, s port.SpawnSpec) (port.SpawnResult, error) {
		specs = append(specs, s)
		passed := strings.Contains(s.System, "try second")
		return port.SpawnResult{Trial: true, Snapshot: "same", Model: "m", Provider: "p", SessionID: fmt.Sprint(len(specs)), Steps: 1, Metrics: port.RunMetrics{Calls: 1, UsageReports: 1, TokensKnown: true}, Verification: &port.TrialVerification{Path: s.VerifyPath, Digest: strings.Repeat("b", 64), Status: map[bool]string{true: "passed", false: "failed"}[passed], Output: map[bool]string{true: "exit 0\n", false: "exit 1\n"}[passed]}}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "bash", Output: "ok"}}, nil
	}}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"fix parser","proposal_attempts":2,"repetitions":2,"verify_path":"verify.sh","max_steps":4}`), env)
	if err != nil || res.IsError {
		t.Fatalf("compare: %s %v", res.Content, err)
	}
	if len(specs) != 5 {
		t.Fatalf("expected baseline, two candidates, and repeat pair, got %d", len(specs))
	}
	if !strings.Contains(specs[3].System, "try second") {
		t.Fatal("repeat did not use the selected candidate guidance")
	}
	var report struct {
		Candidates []struct {
			Guidance string
			Run      struct{ Verification *port.TrialVerification }
		}
		Candidate struct{ Verification *port.TrialVerification }
	}
	if err := json.Unmarshal(res.Content, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Candidates) != 2 || report.Candidate.Verification == nil || report.Candidate.Verification.Status != "passed" {
		t.Fatalf("alternatives not retained/selected: %s", res.Content)
	}
}

func TestS2EngineParallelCandidateExecutionKeepsMetricsSeparate(t *testing.T) {
	r := builtin.NewRegistry()
	analyzer := &fakeAnalyzer{replies: []string{`{"hypothesis":"one","guidance":"one"}`, `{"hypothesis":"two","guidance":"two"}`, `{"preferred":"candidate","observed_effect":"helped","reason":"parallel evidence"}`}}
	h := NewHostWithConfig(HostConfig{ToolSink: r, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()}, Analyzer: analyzer, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	compare, _ := r.Get("s2_compare")
	var mu sync.Mutex
	var specs []port.SpawnSpec
	env := port.ToolEnv{Workdir: h.runtime.Workdir, Spawn: func(ctx context.Context, s port.SpawnSpec) (port.SpawnResult, error) {
		mu.Lock()
		specs = append(specs, s)
		id := len(specs)
		mu.Unlock()
		passed := strings.Contains(s.System, "two")
		return port.SpawnResult{Trial: true, Snapshot: "same", Model: "m", Provider: "p", SessionID: s.Prompt + fmt.Sprint(id), Steps: 1, Metrics: port.RunMetrics{Calls: 1, UsageReports: 1, Input: 11, Output: 3, TokensKnown: true}, Verification: &port.TrialVerification{Path: s.VerifyPath, Digest: strings.Repeat("c", 64), Status: map[bool]string{true: "passed", false: "failed"}[passed], Output: map[bool]string{true: "exit 0\n", false: "exit 1\n"}[passed]}}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "bash", Output: "ok"}}, nil
	}}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"parallel parser","proposal_attempts":2,"execution_mode":"parallel","checkpoint":false,"verify_path":"verify.sh","max_steps":4}`), env)
	if err != nil || res.IsError {
		t.Fatalf("parallel compare: %s %v", res.Content, err)
	}
	if len(specs) != 3 {
		t.Fatalf("expected baseline and two parallel candidates, got %d", len(specs))
	}
	var report struct {
		ExecutionMode string `json:"execution_mode"`
		Candidates    []struct{ Selected bool }
		Cost          struct {
			Calls int `json:"calls"`
		}
	}
	if err := json.Unmarshal(res.Content, &report); err != nil {
		t.Fatal(err)
	}
	if report.ExecutionMode != "parallel" || len(report.Candidates) != 2 || report.Cost.Calls < 3 {
		t.Fatalf("parallel report incomplete: %s", res.Content)
	}
}

func TestS2EngineParallelModeRejectsUnsupportedBudgetBeforeExecution(t *testing.T) {
	_, compare, _ := harnessLab(t)
	calls := 0
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		calls++
		return port.SpawnResult{}, nil
	}}
	for _, raw := range []string{
		`{"task":"t","execution_mode":"parallel","checkpoint":true,"guidance":"g"}`,
		`{"task":"t","execution_mode":"parallel","holdouts":[{"task":"h","verify_path":"h.sh"}],"verify_path":"v.sh","guidance":"g"}`,
		`{"task":"t","execution_mode":"parallel","max_steps":40,"repetitions":3,"verify_path":"v.sh","guidance":"g"}`,
	} {
		res, _ := compare.Execute(context.Background(), json.RawMessage(raw), env)
		if !res.IsError {
			t.Fatalf("unsupported parallel request accepted: %s", res.Content)
		}
	}
	if calls != 0 {
		t.Fatalf("parallel validation spent execution budget: %d", calls)
	}
}
