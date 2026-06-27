package eval

import (
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/app"
)

// plannerEvalAgents is the agent set the pre-flight planner draws from: the
// planner itself plus the read-only explorers it may fan out to.
func plannerEvalAgents() map[string]app.AgentSpec {
	ro := []string{"read", "grep", "glob", "list", "ask", "report"}
	return map[string]app.AgentSpec{
		"planner": {Name: "planner", System: "You are a planning router. Decide whether the user's task should be " +
			"investigated by PARALLEL read-only explorers or handled SOLO. Output ONLY a JSON object: " +
			`{"parallel": bool, "reason": string, "groups": [{"agent": string, "focus": string, "question": string}]}. ` +
			"Set parallel=true ONLY when the task clearly splits into 2+ INDEPENDENT investigation areas, each non-trivial; " +
			"otherwise parallel=false. 'agent' must be explore, locator, or analyst. At most 5 groups. Each 'question' is a " +
			"concrete READ-ONLY investigation, not an implementation step.", Tools: ro},
		"explore": {Name: "explore", System: "Read-only explorer. Investigate the assigned area with read/grep/glob/list and report concrete findings (file:line). Never modify files.", Tools: ro},
		"locator": {Name: "locator", System: "Code-search specialist. Locate relevant files/symbols/usages and report file:line with brief context. Never modify files.", Tools: ro},
		"analyst": {Name: "analyst", System: "Deep-reasoning advisor. Analyze the assigned concern and report findings. Never modify files.", Tools: ro},
	}
}

// TestPlannerAB measures whether the pre-flight planner (fan out read-only
// explorers, inject findings) improves defect coverage on the complex multi-file
// audit versus a lone solo pass. Gated on MAGI_EVAL_BASE like the other A/Bs.
func TestPlannerAB(t *testing.T) {
	base := os.Getenv("MAGI_EVAL_BASE")
	if base == "" {
		t.Skip("set MAGI_EVAL_BASE/_MODEL/_KEY to run the planner A/B")
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	key := os.Getenv("MAGI_EVAL_KEY")
	if key == "" {
		key = os.Getenv("MAGI_API_KEY")
	}
	llm := openai.New(base, key)
	plat := platform.New()

	const trials = 3
	const prompt = "Audit this Go service for ALL concrete defects across security, concurrency, correctness, robustness, and resource management. There are multiple files. Report every issue you find with the file name and a one-line explanation."
	arms := []arm{
		{name: "planner-off", planner: false, system: soloSystem},
		{name: "planner-on ", planner: true, system: soloSystem},
	}

	type agg struct {
		cov, spawns, n int
		dur            time.Duration
		tokOut         int
	}
	totals := map[string]*agg{}
	for i := 0; i < trials; i++ {
		for _, am := range arms {
			reply, r := runReview(t, llm, model, plat, prompt, am)
			cov, found := coverage(reply)
			t.Logf("trial %d %s cov=%2d/10 spawns=%d dur=%s tok-out=%d %v",
				i+1, am.name, cov, r.Spawns, r.Dur.Round(time.Second), r.TokOut, found)
			a := totals[am.name]
			if a == nil {
				a = &agg{}
				totals[am.name] = a
			}
			a.cov += cov
			a.spawns += r.Spawns
			a.dur += r.Dur
			a.tokOut += r.TokOut
			a.n++
		}
	}
	t.Log("=== PLANNER A/B (pre-flight parallel exploration vs solo, 10 planted defects) ===")
	for _, am := range arms {
		a := totals[am.name]
		n := float64(a.n)
		t.Logf("%s  avg-coverage=%.2f/10  avg-dur=%s  avg-tok-out=%.0f  avg-spawns=%.1f",
			am.name, float64(a.cov)/n, (a.dur / time.Duration(a.n)).Round(time.Second),
			float64(a.tokOut)/n, float64(a.spawns)/n)
	}
}
