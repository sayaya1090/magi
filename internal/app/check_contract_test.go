package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// A check whose source is a file the work must leave behind is only meetable if the agent DOING
// the work is told the file will be read. The delegated worker was told (workerChecklist, in its
// brief); the solo agent — the default path — was not, so it never produced the file and the check
// failed on a missing source while the step advanced anyway.
func TestSoloCheckContractNamesEachSourceAndAssertion(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "5", Deliverable: "build output log", Source: "/tmp/build.log", Assert: "contains exit=0"},
		{Step: "6", Deliverable: "smoke test output", Source: "/tmp/smoke.log", Assert: "contains OK"},
	}
	got := soloCheckContract(checks, AgentSpec{Name: "solo"}) // nil Tools == allowed everything
	for _, want := range []string{
		"/tmp/build.log", "contains exit=0", "/tmp/smoke.log", "contains OK",
		"step 5", "step 6", "YOU RUN, THE CHECK READS", "substitute_check",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("solo contract is missing %q:\n%s", want, got)
		}
	}
	if soloCheckContract(nil, AgentSpec{Name: "solo"}) != "" {
		t.Error("no checks must render nothing, not an empty heading")
	}
}

// The repair route is a tool call, so it may only be named to an agent that can reach it — the
// same allowlist discipline the nudges follow. Without it the item still has to be reported, not
// silently dropped, so the obligation survives while the unreachable instruction goes.
func TestCheckContractRepairRouteFollowsAllowlist(t *testing.T) {
	noSub := AgentSpec{Name: "worker", Tools: []string{"read", "write", "bash", "report"}}
	c := []council.DeliverableCheck{{Step: "1", Deliverable: "log", Source: "/tmp/a.log", Assert: "contains ok"}}

	got := soloCheckContract(c, noSub)
	if strings.Contains(got, "substitute_check") {
		t.Errorf("an agent without the tool must not be told to call it:\n%s", got)
	}
	if !strings.Contains(got, "blocked/failed") || !strings.Contains(got, "/tmp/a.log") {
		t.Errorf("dropping the tool must not drop the obligation:\n%s", got)
	}
	// A check with no assertion gates nothing either way — say so, and adapt only the repair route.
	none := []council.DeliverableCheck{{Step: "1", Deliverable: "log"}}
	if s := soloCheckContract(none, noSub); !strings.Contains(s, "carries no assertion") || strings.Contains(s, "substitute_check") {
		t.Errorf("ungated item wrong for an agent without the tool:\n%s", s)
	}
	if s := soloCheckContract(none, AgentSpec{Name: "solo"}); !strings.Contains(s, "carries no assertion") || !strings.Contains(s, "substitute_check") {
		t.Errorf("ungated item should offer the tool when it is reachable:\n%s", s)
	}
}

// Both execution paths render the SAME obligation: they drifted once already (the solo path had no
// rendering at all), and a second copy of the wording is how that happens again.
func TestWorkerAndSoloShareTheCheckObligation(t *testing.T) {
	checks := []council.DeliverableCheck{{Step: "1", Deliverable: "log", Source: "/tmp/a.log", Assert: "contains ok"}}
	worker := workerChecklist(checks, 0)
	solo := soloCheckContract(checks, AgentSpec{Name: "solo"})
	rules := checkContractRules(true)
	if !strings.Contains(worker, rules) {
		t.Errorf("worker checklist no longer renders the shared rules:\n%s", worker)
	}
	if !strings.Contains(solo, rules) {
		t.Errorf("solo contract no longer renders the shared rules:\n%s", solo)
	}
	if !strings.Contains(worker, "the gate will read /tmp/a.log and require: contains ok") {
		t.Errorf("worker checklist lost its item rendering:\n%s", worker)
	}
}
