package app

import "testing"

// Go's decoder aborts the WHOLE document on the first type mismatch, so a single field answered
// with the wrong shape used to discard everything beside it. These lock the tolerant readings:
// what matters in each case is not the odd field itself but that its SIBLINGS survive.

func TestParsePlanTolerantFields(t *testing.T) {
	cases := []struct {
		name          string
		blob          string
		wantSteps     int
		wantFirst     string
		wantReason    string
		wantEstimated int
	}{
		{
			name:      "reason as a list keeps the steps",
			blob:      `{"steps":[{"title":"build it","strategy":"solo"},{"title":"test it","strategy":"solo"}],"reason":["it is small","one pass is enough"]}`,
			wantSteps: 2, wantFirst: "build it", wantReason: "it is small; one pass is enough",
		},
		{
			name:      "quoted estimated_steps keeps the plan",
			blob:      `{"steps":[{"title":"build it","strategy":"solo"}],"estimated_steps":"8"}`,
			wantSteps: 1, wantFirst: "build it", wantEstimated: 8,
		},
		{
			name:      "contest as a list keeps the steps",
			blob:      `{"steps":[{"title":"build it","strategy":"solo"}],"contest":["the task never asked for it"]}`,
			wantSteps: 1, wantFirst: "build it",
		},
		{
			name:      "a single step object instead of an array",
			blob:      `{"steps":{"title":"build it","strategy":"solo"},"reason":"one step"}`,
			wantSteps: 1, wantFirst: "build it", wantReason: "one step",
		},
		{
			name:      "a bare-string step keeps its siblings",
			blob:      `{"steps":[{"title":"build it","strategy":"solo"},"test it",{"title":"ship it","strategy":"solo"}]}`,
			wantSteps: 3, wantFirst: "build it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parsePlan(tc.blob)
			if len(p.Steps) != tc.wantSteps {
				t.Fatalf("steps = %d, want %d (%+v)", len(p.Steps), tc.wantSteps, p)
			}
			if tc.wantFirst != "" && p.Steps[0].Title != tc.wantFirst {
				t.Errorf("first title = %q, want %q", p.Steps[0].Title, tc.wantFirst)
			}
			if tc.wantReason != "" && p.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", p.Reason, tc.wantReason)
			}
			if tc.wantEstimated != 0 && p.EstimatedSteps != tc.wantEstimated {
				t.Errorf("estimated_steps = %d, want %d", p.EstimatedSteps, tc.wantEstimated)
			}
		})
	}
}

// The bare-string step keeps the model's words rather than dropping the step: an executed plan that
// silently lost a step is worse than one that carries it as a titled step with no strategy.
func TestParsePlanBareStringStepKeepsItsText(t *testing.T) {
	p := parsePlan(`{"steps":["write the parser","test the parser"]}`)
	if len(p.Steps) != 2 {
		t.Fatalf("steps = %d, want 2 (%+v)", len(p.Steps), p)
	}
	if p.Steps[0].Title != "write the parser" || p.Steps[1].Title != "test the parser" {
		t.Errorf("titles not preserved: %+v", p.Steps)
	}
}

// A well-formed plan must still parse exactly as before — the tolerance is a fallback, not a rewrite.
func TestParsePlanCleanReplyUnchanged(t *testing.T) {
	p := parsePlan(`{"steps":[{"title":"a","strategy":"delegate","task":"do a","agent":"coder"}],"reason":"why","estimated_steps":4}`)
	if len(p.Steps) != 1 || p.Steps[0].Title != "a" || p.Steps[0].Strategy != "delegate" ||
		p.Steps[0].Task != "do a" || p.Steps[0].Agent != "coder" || p.Reason != "why" || p.EstimatedSteps != 4 {
		t.Fatalf("clean plan changed shape: %+v", p)
	}
}

func TestParseSpecMineTolerantFields(t *testing.T) {
	// One line answers `requirement` with a list; every OTHER line must survive it.
	blob := `{"lines":[` +
		`{"surface":"port 5328","requirement":["bind it","keep it open"],"construct":"socket","kind":"hard"},` +
		`{"surface":"GetVal","requirement":"returns the stored value","construct":"grpc","kind":"hard"}` +
		`],"final":"use protoc"}`
	res, ok := parseSpecMine(blob)
	if !ok {
		t.Fatalf("reply did not parse: %s", blob)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("lines = %d, want 2 (%+v)", len(res.Lines), res)
	}
	if got := string(res.Lines[0].Requirement); got != "bind it; keep it open" {
		t.Errorf("listed requirement = %q", got)
	}
	if got := string(res.Lines[1].Surface); got != "GetVal" {
		t.Errorf("sibling line lost: %q", got)
	}
	if got := string(res.Final); got != "use protoc" {
		t.Errorf("final = %q", got)
	}
}

// `final` given as a list is joined rather than costing every mined line.
func TestParseSpecMineFinalAsList(t *testing.T) {
	res, ok := parseSpecMine(`{"lines":[{"surface":"s","requirement":"r","construct":"c","kind":"semantic"}],"final":["run protoc","then start the server"]}`)
	if !ok || len(res.Lines) != 1 {
		t.Fatalf("parse failed: ok=%v %+v", ok, res)
	}
	if got := string(res.Final); got != "run protoc; then start the server" {
		t.Errorf("final = %q", got)
	}
}
