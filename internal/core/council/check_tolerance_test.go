package council

import (
	"encoding/json"
	"testing"
)

// A check array is authored as ONE document: Go aborts the whole thing on the first type mismatch,
// so a single odd field used to discard every other check with it — and the run then has no
// executable contract at all, indistinguishable from a model that proposed none.
func TestDeliverableCheckTolerantFields(t *testing.T) {
	cases := []struct {
		name              string
		blob              string
		wantCmd, wantExp  string
		wantStep, wantDlv string
	}{
		{
			name:     "expect as an exit-code number",
			blob:     `{"step":2,"deliverable":"the binary builds","command":"make","expect":0}`,
			wantStep: "2", wantDlv: "the binary builds", wantCmd: "make", wantExp: "0",
		},
		{
			name:     "command enumerated as a list",
			blob:     `{"step":"1","command":["cd /app","make test"]}`,
			wantStep: "1", wantCmd: "cd /app; make test",
		},
		{
			name:     "deliverable as a list",
			blob:     `{"step":1,"deliverable":["server.py","the running server"],"command":"ls"}`,
			wantStep: "1", wantDlv: "server.py; the running server", wantCmd: "ls",
		},
		{
			// Never the command: an invented command would be executed. Inert, and dropped by MergeChecks.
			name:    "a bare string is read as the deliverable, not the command",
			blob:    `"make test"`,
			wantDlv: "make test",
		},
		{
			name:     "a clean check is unchanged",
			blob:     `{"step":"3","deliverable":"d","command":"c","expect":"e"}`,
			wantStep: "3", wantDlv: "d", wantCmd: "c", wantExp: "e",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c DeliverableCheck
			if err := json.Unmarshal([]byte(tc.blob), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if c.Step != tc.wantStep || c.Deliverable != tc.wantDlv || c.Command != tc.wantCmd || c.Expect != tc.wantExp {
				t.Fatalf("got %+v, want step=%q deliverable=%q command=%q expect=%q",
					c, tc.wantStep, tc.wantDlv, tc.wantCmd, tc.wantExp)
			}
		})
	}
}

// The point of the tolerance: one odd check must not cost the others.
func TestDeliverableCheckArraySurvivesOneOddElement(t *testing.T) {
	blob := `[` +
		`{"step":1,"deliverable":"a","command":"make a"},` +
		`{"step":2,"deliverable":"b","command":"make b","expect":0},` +
		`"make c",` +
		`{"step":4,"deliverable":"d","command":"make d"}` +
		`]`
	var checks []DeliverableCheck
	if err := json.Unmarshal([]byte(blob), &checks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(checks) != 4 {
		t.Fatalf("checks = %d, want 4: %+v", len(checks), checks)
	}
	if checks[1].Expect != "0" || checks[2].Command != "" || checks[2].Deliverable != "make c" || checks[3].Deliverable != "d" {
		t.Fatalf("elements not preserved: %+v", checks)
	}
}

// An element that is neither an object nor a string still fails, and that failure is the ARRAY's:
// there is nothing to recover from it, and inventing a check would be worse than reporting none.
func TestDeliverableCheckUnreadableElementStillErrors(t *testing.T) {
	var c DeliverableCheck
	if err := json.Unmarshal([]byte(`12`), &c); err == nil {
		t.Fatalf("a bare number should not become a check, got %+v", c)
	}
}

// The inert check a bare string produces must not reach execution: MergeChecks drops it, so a line
// the model wrote as prose never becomes a command that fails forever against correct work.
func TestMergeChecksDropsBareStringCheck(t *testing.T) {
	var checks []DeliverableCheck
	if err := json.Unmarshal([]byte(`["the server answers on 5328",{"step":1,"deliverable":"d","command":"make"}]`), &checks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	merged := MergeChecks([]Verdict{{Checks: checks}})
	if len(merged) != 1 || merged[0].Command != "make" {
		t.Fatalf("merged = %+v, want only the runnable check", merged)
	}
}

// A typed check carries no `command` — the runner builds the invocation from `source`+`assert`. Every
// place that used "no command" as shorthand for "nothing to run" would drop the entire typed set, so
// the merge must decide on what the check VERIFIES, not on which field happens to be filled.
func TestMergeChecksKeepsTypedChecks(t *testing.T) {
	var checks []DeliverableCheck
	raw := `[{"step":"1","deliverable":"build log","source":"/app/build.log","assert":"matches ^Done\\.$"},
	         {"step":"1","deliverable":"no errors","source":"/app/build.log","assert":"absent error"},
	         {"step":"2","deliverable":"tests","command":"make test"},
	         {"deliverable":"nothing to run"}]`
	if err := json.Unmarshal([]byte(raw), &checks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	merged := MergeChecks([]Verdict{{Checks: checks}})
	if len(merged) != 3 {
		t.Fatalf("merged %d checks (%+v), want the 2 typed + 1 command and not the empty one", len(merged), merged)
	}
	// Two typed checks over the SAME source differ only by their assertion: dedupe keyed on the
	// command alone would collapse them into one and silently halve the contract.
	if merged[0].Assert == merged[1].Assert {
		t.Fatalf("the two typed checks collapsed: %+v", merged)
	}
	if merged[0].Source != "/app/build.log" || merged[0].Assert != `matches ^Done\.$` {
		t.Fatalf("typed fields lost in the merge: %+v", merged[0])
	}
}

// The typed fields go through the same tolerant unmarshal as the rest of the shape, so a weak model
// that sends a number or a one-element array where a string belongs still yields a runnable check.
func TestDeliverableCheckTypedFieldTolerance(t *testing.T) {
	var c DeliverableCheck
	if err := json.Unmarshal([]byte(`{"source":["/app/out.log"],"assert":"nonempty"}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Source != "/app/out.log" || c.Assert != "nonempty" {
		t.Fatalf("got %+v", c)
	}
}
