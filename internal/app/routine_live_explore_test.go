package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRoutineLiveExploreReportsRegressionAndHoldoutReuse(t *testing.T) {
	r, script := setupRPE(t)
	before := r.skill(t, rpe)
	r.put(t, "scope/evidence.go", []byte("package evidence\n"))
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
			ProtectedRegressions int `json:"protected_regressions"`
			HoldoutUse           int `json:"holdout_use"`
			Rows                 []struct {
				Transition string
				Candidate  struct {
					Invalid []string
					Missing []string
					Calls   int `json:"explore_calls"`
				}
			}
		}
		if failed || len(asks) != 0 || json.Unmarshal([]byte(out), &report) != nil ||
			report.ProtectedRegressions != 1 || report.HoldoutUse != i+1 || len(report.Rows) != 1 ||
			report.Rows[0].Transition != "regressed" || report.Rows[0].Candidate.Calls != 1 ||
			strings.Join(report.Rows[0].Candidate.Invalid, "") != "evidence.go:99" ||
			strings.Join(report.Rows[0].Candidate.Missing, "") != "evidence.go:1" {
			t.Fatalf("unexpected comparison %s: %s", id, out)
		}
	}
	if r.skill(t, rpe) != before {
		t.Fatal("comparison modified the active routine")
	}
	log, err := os.ReadFile(filepath.Join(r.s.Workdir, dir, "live-explore.jsonl"))
	if err != nil || strings.Count(string(log), `"phase":"completed"`) != 2 {
		t.Fatalf("comparison ledger: %s (%v)", log, err)
	}
}
