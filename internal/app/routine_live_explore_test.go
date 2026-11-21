package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

type comparisonModel struct {
	*scriptLLM
	beforeFirst func()
}

func (m *comparisonModel) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if m.call == 0 {
		m.beforeFirst()
	}
	return m.scriptLLM.StreamChat(ctx, req)
}

func TestRoutineLiveExploreReportsRegressionAndHoldoutReuse(t *testing.T) {
	r, script := setupRPE(t)
	before := r.skill(t, rpe)
	r.put(t, "scope/evidence.go", []byte("package evidence\n"))
	if _, err := gitRun(context.Background(), r.s.Workdir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := commitAll(context.Background(), r.s.Workdir, "fixture"); err != nil {
		t.Fatal(err)
	}
	dir := ".magi/routines/" + rpe + "/"
	r.put(t, dir+"tasks.json", []byte(`{"tasks":[{"id":"protected","split":"holdout","protected":true,"input":{"questions":["find it"]},"expect":{"evidence":["evidence.go:1"]}}]}`))
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	r.a.llm = &scriptLLM{steps: [][]port.ProviderEvent{
		asideText("evidence.go:1"), asideText("evidence.go:99"),
		asideText("evidence.go:1"), asideText("evidence.go:99"),
	}}
	if _, err := r.host.Load(context.Background(), filepath.Join("..", "..", "plugins", "explore")); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"first", "second"} {
		out, asks, failed := r.call(t, id, "routine_compare_explore", map[string]any{
			"name": rpe, "directory": "scope", "split": "holdout",
			"script": script + "\nprint(\"evidence.go:1\")",
		}, "allow")
		var report struct {
			SourceTree           string `json:"source_tree"`
			ProtectedRegressions int    `json:"protected_regressions"`
			HoldoutUse           int    `json:"holdout_use"`
			Rows                 []struct {
				Transition string
				Active     struct {
					ModelCalls int `json:"model_calls"`
				}
				Candidate struct {
					Invalid []string
					Missing []string
					Calls   int `json:"explore_calls"`
				}
			}
		}
		if failed || len(asks) != 0 || json.Unmarshal([]byte(out), &report) != nil ||
			report.ProtectedRegressions != 1 || report.HoldoutUse != i+1 || len(report.Rows) != 1 ||
			report.SourceTree == "" || report.Rows[0].Active.ModelCalls != 1 ||
			report.Rows[0].Transition != "regressed" || report.Rows[0].Candidate.Calls != 1 ||
			strings.Join(report.Rows[0].Candidate.Invalid, "") != "evidence.go:99" ||
			strings.Join(report.Rows[0].Candidate.Missing, "") != "evidence.go:1" {
			t.Fatalf("unexpected comparison %s: %s", id, out)
		}
	}
	r.put(t, dir+"tasks.json", []byte(`{"tasks":[{"id":"development","input":{"questions":["find it"]},"expect":{"evidence":["evidence.go:1"]}}]}`))
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	withUsage := append([]port.ProviderEvent{
		{Type: port.ProviderUsage, Usage: &event.Usage{In: 123, Out: 5}},
	}, asideText("evidence.go:1")...)
	r.a.llm = &comparisonModel{
		scriptLLM: &scriptLLM{steps: [][]port.ProviderEvent{withUsage, asideText("evidence.go:99")}},
		beforeFirst: func() {
			if err := os.WriteFile(filepath.Join(r.s.Workdir, "scope", "evidence.go"), nil, 0600); err != nil {
				t.Error(err)
			}
			r.a.mu.Lock()
			r.a.llm = &scriptLLM{steps: [][]port.ProviderEvent{asideText("evidence.go:1")}}
			r.a.mu.Unlock()
		},
	}
	aa, _, failed := r.call(t, "aa", "routine_compare_explore", map[string]any{
		"name": rpe, "directory": "scope", "aa": true,
	}, "allow")
	if failed || !strings.Contains(aa, `"mode":"A/A"`) || strings.Contains(aa, "holdout_use") ||
		!strings.Contains(aa, `"transition":"regressed"`) || !strings.Contains(aa, `"input_tokens":123`) ||
		!strings.Contains(aa, `"tokens_known":true`) {
		t.Fatalf("A/A should expose variation without using holdout: %s", aa)
	}

	r.put(t, "scope/evidence.go", []byte("package evidence\n"))
	r.put(t, dir+"tasks.json", []byte(`{"tasks":[{"id":"uncertain","input":{"questions":["what is known?"]},"expect":{"evidence":["evidence.go:1"],"conclusion":"UNVERIFIED"}}]}`))
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	r.a.llm = &scriptLLM{steps: [][]port.ProviderEvent{
		asideText("evidence.go:1\nCONCLUSION: SUPPORTED"),
		asideText("CONCLUSION: UNVERIFIED"),
	}}
	report, _, failed := r.call(t, "conclusion", "routine_compare_explore", map[string]any{
		"name": rpe, "directory": "scope", "script": script,
	}, "allow")
	var statuses struct {
		Rows []struct {
			Active, Candidate struct {
				Citation   string `json:"citation_status"`
				Conclusion string `json:"conclusion_status"`
			}
		}
	}
	if failed || json.Unmarshal([]byte(report), &statuses) != nil || len(statuses.Rows) != 1 ||
		statuses.Rows[0].Active.Citation != "pass" || statuses.Rows[0].Active.Conclusion != "fail" ||
		statuses.Rows[0].Candidate.Citation != "fail" || statuses.Rows[0].Candidate.Conclusion != "pass" {
		t.Fatalf("the two dimensions must be independent: %s", report)
	}

	if r.skill(t, rpe) != before {
		t.Fatal("comparison modified the active routine")
	}
	log, err := os.ReadFile(filepath.Join(r.s.Workdir, dir, "live-explore.jsonl"))
	if err != nil || strings.Count(string(log), `"phase":"completed"`) != 4 {
		t.Fatalf("comparison ledger: %s (%v)", log, err)
	}
}
