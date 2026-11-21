package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Improving a routine runs on its own once a person has delegated it: a candidate that regresses is
// rejected and only recorded (nobody is told), one that passes is applied, recorded apart from run
// approval, announced in one line and undone by /routine-rollback. Without a delegation, or outside
// what was delegated (a routine that writes, evals changed since), a passing candidate is left for a
// person. Nothing here needs Engram or a model.
func TestARoutineImprovesItselfWithinTheScopeAPersonDelegatedOnce(t *testing.T) {
	r, script := setupRPE(t)
	r.a.tools.Register(&fakeTool{name: "recall_memory", answer: func(map[string]any) string { return "POLICY-R1" }})
	r.a.tools.Register(&fakeTool{name: "explore", answer: func(map[string]any) string { return "x.go:1" }})
	args := map[string]any{"questions": []any{"Q1"}}
	if _, asks, _ := r.call(t, "run0", "run_routine", map[string]any{"name": rpe, "args": args}, "always"); len(asks) != 1 {
		t.Fatalf("the active version's first run asks once: %+v", asks)
	}
	worse := strings.Replace(script, "Answer with path:line evidence from the code, and say where the code disagrees with the policy.",
		"Trace from the click handler to the daemon command. Answer with path:line evidence from the code.", 1)
	better := strings.Replace(script, "Answer with path:line", "Trace from the click handler to the daemon command. Answer with path:line", 1)
	improve := func(id, cand string) string {
		t.Helper()
		out, asks, failed := r.call(t, id, "routine_improve", map[string]any{"name": rpe, "script": cand, "hypothesis": "trace the click path"}, "allow")
		if failed || len(asks) != 0 {
			t.Fatalf("%s: %q %+v", id, out, asks)
		}
		return out
	}
	active := func() bool { return strings.Contains(r.skill(t, rpe), "Trace from the click handler") }

	before := r.logs.text()
	if out := improve("w0", worse); !strings.Contains(out, "verdict: rejected") || strings.Contains(out, "auto-applied") {
		t.Fatalf("undelegated regression: %q", out)
	}
	if r.logs.text() != before {
		t.Fatalf("a rejection was announced: %q", strings.TrimPrefix(r.logs.text(), before))
	}
	if out := improve("b0", better); !strings.Contains(out, "verdict: applicable") || !strings.Contains(out, "a person may adopt") || active() {
		t.Fatalf("without a delegation a passing candidate is left for a person: %q", out)
	}

	r.put(t, ".magi/skills/writer.md", []byte(writerRoutine))
	if err := r.command(t, "routine-auto", "writer"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("a routine that writes cannot be delegated: %v", err)
	}
	if err := r.command(t, "routine-auto", rpe, "explore-text", "clients"); err != nil {
		t.Fatal(err)
	}

	before = r.logs.text()
	if out := improve("w1", worse); !strings.Contains(out, "verdict: rejected") || strings.Contains(out, "auto-applied") || active() {
		t.Fatalf("a delegated regression is rejected, not applied: %q", out)
	}
	if r.logs.text() != before {
		t.Fatalf("a delegated rejection was announced: %q", strings.TrimPrefix(r.logs.text(), before))
	}
	out := improve("b1", better)
	if !strings.Contains(out, "verdict: applicable") || !strings.Contains(out, "auto-applied") || !active() {
		t.Fatalf("a delegated passing candidate is applied: %q", out)
	}
	if !strings.Contains(r.logs.text(), "[routine] auto-applied") || !strings.Contains(r.logs.text(), "/routine-rollback "+rpe) {
		t.Fatalf("the person is told in one line, with how to undo it: %q", r.logs.text())
	}
	dec, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi/routines/"+rpe+"/decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"action":"auto-apply delegated"`, `"action":"auto-applied"`, `"by":"auto"`, `"run_approval":"not granted`} {
		if !strings.Contains(string(dec), want) {
			t.Fatalf("decisions lack %s:\n%s", want, dec)
		}
	}
	_, asks, _ := r.call(t, "run1", "run_routine", map[string]any{"name": rpe, "args": args}, "allow")
	if len(asks) != 1 || !strings.Contains(asks[0].Reason, "changed since it was approved") || !strings.Contains(string(asks[0].Args), `"explore_read_scope":"clients"`) {
		t.Fatalf("applying is not run approval: the applied version asks: %+v", asks)
	}
	if err := r.command(t, "routine-rollback", rpe); err != nil || active() {
		t.Fatalf("rollback: %v", err)
	}

	if err := r.command(t, "routine-auto", rpe, "explore-text", "clients"); err != nil {
		t.Fatal(err)
	}
	if out := improve("rolled-back", better); !strings.Contains(out, "was rolled back by a person") || active() {
		t.Fatalf("renewing delegation must not undo a rollback: %q", out)
	}
	distinct := better + "\n-- independently versioned candidate"

	r.put(t, ".magi/routines/"+rpe+"/tasks.json", rpeTasks("edited and frozen again"))
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	if out := improve("b2", distinct); !strings.Contains(out, "verdict: applicable") || !strings.Contains(out, "outside the delegated scope") || active() {
		t.Fatalf("evals changed since the delegation: left for a person: %q", out)
	}
	if err := r.command(t, "routine-auto", rpe, "explore-text", "clients"); err != nil {
		t.Fatal(err)
	}
	if out := improve("distinct", distinct); !strings.Contains(out, "\nauto-applied ") || !active() {
		t.Fatalf("a distinct version can still be applied: %q", out)
	}

	if err := r.command(t, "routine-auto", rpe, "off"); err != nil {
		t.Fatal(err)
	}
}
