package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// setupRPE puts the shipped recovery-policy-evidence routine and its evals in place, frozen, and
// returns the rig and the routine's script.
func setupRPE(t *testing.T) (*routineRig, string) {
	t.Helper()
	r := newRoutineRig(t, t.TempDir(), routineFile)
	body, err := os.ReadFile(filepath.Join("..", "..", "plugins", "dummy-plug", "routines", rpe+".md"))
	if err != nil {
		t.Fatal(err)
	}
	r.put(t, ".magi/skills/"+rpe+".md", body)
	dir := ".magi/routines/" + rpe + "/"
	r.put(t, dir+"contract.json", []byte(rpeContract))
	r.put(t, dir+"tasks.json", rpeTasks(""))
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	s := string(body)
	return r, s[strings.Index(s, "```lua\n")+7 : strings.LastIndex(s, "\n```")]
}

// One cycle end to end: a run the person picked, a hypothesis tied to it, a candidate rejected on the
// same tasks, the reason read back, the next candidate written from it and found applicable, adopted
// by the person — and the adopted version still asks before its next run.
func TestARoutineImprovementCycleStartsFromAPickedRunAndFeedsTheNext(t *testing.T) {
	r, script := setupRPE(t)
	r.a.tools.Register(&fakeTool{name: "recall_memory", answer: func(map[string]any) string { return "POLICY-R1 an interrupted call is not re-run" }})
	r.a.tools.Register(&fakeTool{name: "explore", answer: func(map[string]any) string { return "chat_view.ts:746 click" }})
	src := "local input = {questions={\"VSC-1: what does clicking Stop send to the daemon?\"}}\n" + script
	if out, _, failed := r.call(t, "pick", "dummy_plug", map[string]any{"goal": "check VS Code stop path", "script": src}, "allow"); failed {
		t.Fatalf("the run to start from: %q", out)
	}
	if err := r.command(t, "routine-trace"); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`run (r[0-9a-f]{8}) check VS Code stop path`).FindStringSubmatch(r.logs.text())
	if m == nil {
		t.Fatalf("the picked run is not listed: %q", r.logs.text())
	}
	if out, asks, failed := r.call(t, "bad", "routine_improve", map[string]any{"name": rpe, "script": script + "\n",
		"hypothesis": "x", "from_run": "r00000000"}, "allow"); !failed || len(asks) != 0 || !strings.Contains(out, "no run r00000000") {
		t.Fatalf("a run that does not exist cannot be the source: %q", out)
	}
	improve := func(id, cand string, in map[string]any) string {
		t.Helper()
		in["name"], in["script"] = rpe, cand
		out, asks, failed := r.call(t, id, "routine_improve", in, "allow")
		if failed || len(asks) != 0 {
			t.Fatalf("%s: %q %+v", id, out, asks)
		}
		return out
	}
	worse := strings.Replace(script, "Answer with path:line evidence from the code, and say where the code disagrees with the policy.",
		"Trace from the click handler to the daemon command. Answer with path:line evidence from the code.", 1)
	out1 := improve("c1", worse, map[string]any{"from_run": m[1], "problem": "run " + m[1] + " found the click but not the daemon side",
		"hypothesis": "asking for the path from the click handler to the daemon command finds the daemon side"})
	if !strings.Contains(out1, "verdict: rejected") || !strings.Contains(out1, "JB-close") || !strings.Contains(out1, "from run "+m[1]) {
		t.Fatalf("first candidate:\n%s", out1)
	}
	aid := regexp.MustCompile(`recorded as attempt (a[0-9a-f]{8})`).FindStringSubmatch(out1)
	if aid == nil {
		t.Fatalf("no attempt id:\n%s", out1)
	}
	hist, _, failed := r.call(t, "h", "routine_history", map[string]any{"name": rpe}, "allow")
	if failed || !strings.Contains(hist, aid[1]) || !strings.Contains(hist, "JB-close") {
		t.Fatalf("history must give the next candidate the reason: %q", hist)
	}
	if out, _, failed := r.call(t, "bad2", "routine_improve", map[string]any{"name": rpe, "script": script + "\n",
		"hypothesis": "x", "previous": "a00000000"}, "allow"); !failed || !strings.Contains(out, "no attempt a00000000") {
		t.Fatalf("an attempt that does not exist cannot be followed: %q", out)
	}
	better := strings.Replace(script, "Answer with path:line", "Trace from the click handler to the daemon command. Answer with path:line", 1)
	out2 := improve("c2", better, map[string]any{"previous": aid[1], "from_run": m[1],
		"hypothesis": "keep asking where the code disagrees with the policy (JB-close was lost in " + aid[1] + ") and add the click-to-daemon trace"})
	if !strings.Contains(out2, "verdict: applicable") || !strings.Contains(out2, "follows attempt "+aid[1]+" (rejected") {
		t.Fatalf("second candidate:\n%s", out2)
	}
	raw, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi/routines/"+rpe+"/attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(lines))
	}
	t.Logf("attempts.jsonl line 2: %s", lines[1])
	var rec struct {
		SourceRun  struct{ Run string } `json:"source_run"`
		Follows    struct{ Attempt, Verdict string }
		Conditions struct {
			DeclaredModel    string `json:"declared_model"`
			DeclaredRevision string `json:"declared_revision"`
		}
		Effect struct {
			Mean, Low, High float64
			SignP           float64 `json:"sign_p"`
		}
		Cost struct {
			Active, Candidate struct {
				Calls                 int
				SimulatedExploreCalls int `json:"simulated_explore_calls"`
			}
		}
	}
	if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.SourceRun.Run != m[1] || rec.Follows.Attempt != aid[1] || rec.Follows.Verdict != "rejected" || rec.Conditions.DeclaredModel == "" ||
		rec.Conditions.DeclaredRevision == "" || rec.Effect.High <= rec.Effect.Low || rec.Effect.Mean <= 0 || rec.Cost.Candidate.Calls == 0 || rec.Cost.Candidate.SimulatedExploreCalls == 0 {
		t.Fatalf("the attempt lacks its links, conditions, effect or cost: %+v", rec)
	}
	args := map[string]any{"questions": []any{"Q1"}}
	if _, asks, _ := r.call(t, "run1", "run_routine", map[string]any{"name": rpe, "args": args}, "always"); len(asks) != 1 {
		t.Fatalf("the active version's first run asks once: %+v", asks)
	}
	if _, asks, _ := r.call(t, "run2", "run_routine", map[string]any{"name": rpe, "args": args}, "allow"); len(asks) != 0 {
		t.Fatalf("an approved version runs without asking: %+v", asks)
	}
	ver := regexp.MustCompile(`candidate ([0-9a-f]{12})`).FindStringSubmatch(out2)
	if err := r.command(t, "routine-adopt", rpe, ver[1]); err != nil {
		t.Fatal(err)
	}
	_, asks, _ := r.call(t, "run3", "run_routine", map[string]any{"name": rpe, "args": args}, "allow")
	if len(asks) != 1 || !strings.Contains(asks[0].Reason, "changed since it was approved") {
		t.Fatalf("passing the evals and being adopted is not approval: the adopted version must ask: %+v", asks)
	}
}

// Comparing a routine that writes reaches no file: both versions run dry on fixtures, so the
// workspace is not touched, and the result says its effects were not evaluated.
func TestComparingAWritingRoutineTouchesNoFile(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, ".magi/skills/writer.md", []byte(writerRoutine))
	dir := ".magi/routines/writer/"
	r.put(t, dir+"contract.json", []byte(`{"cases":[{"id":"writes-then-says","input":{"text":"hi"},"fixtures":[{"tool":"write","text":"ok"}],"calls":[{"tool":"write","contains":"out.txt"}],"print_contains":["wrote"]}]}`))
	r.put(t, dir+"tasks.json", []byte(`{"tasks":[{"id":"W-1","split":"holdout","input":{"text":"hi"},"fixtures":[{"tool":"write","text":"ok"}],"expect":{"evidence":["wrote"]}}]}`))
	if err := r.command(t, "routine-evals-freeze", "writer"); err != nil {
		t.Fatal(err)
	}
	script := writerRoutine[strings.Index(writerRoutine, "```lua\n")+7 : strings.LastIndex(writerRoutine, "\n```")]
	cand := strings.Replace(script, `"wrote"`, `"wrote it"`, 1)
	out, asks, failed := r.call(t, "wc", "routine_improve", map[string]any{"name": "writer", "script": cand, "hypothesis": "say more"}, "allow")
	if failed || len(asks) != 0 || !strings.Contains(out, "writes (write): compared dry only") || !strings.Contains(out, "contract: pass 1") {
		t.Fatalf("comparing a writing routine: %q %+v", out, asks)
	}
	if _, err := os.Stat(filepath.Join(r.s.Workdir, "out.txt")); err == nil {
		t.Fatal("comparing a writing routine wrote to the workspace")
	}
}
