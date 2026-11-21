package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
)

// scriptedAnalyzer stands in for the sidecar model: it hands out prepared replies in order and keeps
// what it was asked.
type scriptedAnalyzer struct {
	mu      sync.Mutex
	replies []string
	prompts []string
}

func (s *scriptedAnalyzer) Analyze(_ context.Context, _, text, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, text)
	if len(s.replies) == 0 {
		return "", errors.New("no more replies")
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return r, nil
}

func candidateReply(hypothesis, script string) string {
	return "HYPOTHESIS: " + hypothesis + "\n```lua\n" + script + "\n```\n"
}

func setupRPEWith(t *testing.T, an luaplugin.Analyzer) (*routineRig, string) {
	t.Helper()
	r := newRoutineRigAnalyzing(t, t.TempDir(), routineFile, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second}, an)
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

func rpeVariants(script string) (worse, better string) {
	worse = strings.Replace(script, "Answer with path:line evidence from the code, and say where the code disagrees with the policy.",
		"Trace from the click handler to the daemon command. Answer with path:line evidence from the code.", 1)
	better = strings.Replace(script, "Answer with path:line", "Trace from the click handler to the daemon command. Answer with path:line", 1)
	return
}

// The routine improves itself: each round a candidate is generated from the active script, the
// problem and the earlier attempts (holdout task ids hidden), compared, and the next round follows the
// last attempt. A rejected round is only recorded; the first applicable one is applied under the
// delegation and the rounds stop.
func TestARoutineGeneratesComparesAndAppliesItsOwnCandidates(t *testing.T) {
	an := &scriptedAnalyzer{}
	r, script := setupRPEWith(t, an)
	worse, better := rpeVariants(script)
	an.replies = []string{candidateReply("trace the click path", worse),
		candidateReply("trace the click path and keep asking where the code disagrees", better)}
	if err := r.command(t, "routine-auto", rpe, "explore-text"); err != nil {
		t.Fatal(err)
	}
	before := r.logs.text()
	out, asks, failed := r.call(t, "ai", "routine_autoimprove", map[string]any{"name": rpe, "problem": "VS Code click paths are missed", "rounds": 3}, "allow")
	if failed || len(asks) != 0 || !strings.Contains(out, "round 1: rejected") || !strings.Contains(out, "round 2: applicable") || !strings.Contains(out, "auto-applied") {
		t.Fatalf("rounds: %q %+v", out, asks)
	}
	if len(an.prompts) != 2 {
		t.Fatalf("the rounds stop at the first applicable candidate: %d generations", len(an.prompts))
	}
	p2 := an.prompts[1]
	if !strings.Contains(p2, "rejected") || !strings.Contains(p2, "<holdout task>") || strings.Contains(p2, "JB-close") {
		t.Fatalf("the next generation reads why the last one was rejected, with holdout tasks hidden:\n%s", p2)
	}
	if !strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatal("the applicable candidate was not applied")
	}
	if n := strings.Count(strings.TrimPrefix(r.logs.text(), before), "[routine]"); n != 1 {
		t.Fatalf("only the application is announced, got %d notices: %q", n, strings.TrimPrefix(r.logs.text(), before))
	}
	raw, err := os.ReadFile(filepath.Join(r.s.Workdir, ".magi/routines/"+rpe+"/attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var first, second struct {
		ID      string
		Follows struct{ Attempt string }
	}
	if len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &first) != nil || json.Unmarshal([]byte(lines[1]), &second) != nil || second.Follows.Attempt != first.ID {
		t.Fatalf("the second attempt must follow the first: %s", raw)
	}
}

// Delegation covers data reach too: a candidate that passes new arguments to read, grep, glob, list or
// recall_memory is left for a person even when it is applicable; one that reaches the same data is applied.
func TestADelegatedCandidateThatReachesNewDataIsLeftForAPerson(t *testing.T) {
	r, script := setupRPE(t)
	_, better := rpeVariants(script)
	reachy := strings.Replace(better, `input.query or "recovery policy"`, `input.query or "recovery policy and every credential"`, 1)
	if reachy == better {
		t.Fatal("fixture did not change")
	}
	if err := r.command(t, "routine-auto", rpe, "explore-text"); err != nil {
		t.Fatal(err)
	}
	improve := func(id, cand string) string {
		out, _, failed := r.call(t, id, "routine_improve", map[string]any{"name": rpe, "script": cand, "hypothesis": "h"}, "allow")
		if failed {
			t.Fatalf("%s: %q", id, out)
		}
		return out
	}
	if out := improve("r1", reachy); !strings.Contains(out, "verdict: applicable") || !strings.Contains(out, "not auto-applied") ||
		!strings.Contains(out, "data reach") || strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatalf("new data reach must be left for a person: %q", out)
	}
	if out := improve("r2", better); !strings.Contains(out, "auto-applied") {
		t.Fatalf("the same data reach is applied: %q", out)
	}
}

// explore takes free text, so a changed question may reach other data. A delegation does not cover
// that unless the person allowed it: under a plain /routine-auto, a candidate whose only change is its
// explore question is applicable but left for a person; /routine-auto <name> explore-text allows it.
func TestAnExploreOnlyChangeIsLeftForAPersonUnlessDelegated(t *testing.T) {
	r, script := setupRPE(t)
	_, better := rpeVariants(script)
	if err := r.command(t, "routine-auto", rpe); err != nil {
		t.Fatal(err)
	}
	out, _, failed := r.call(t, "e1", "routine_improve", map[string]any{"name": rpe, "script": better, "hypothesis": "h"}, "allow")
	if failed || !strings.Contains(out, "verdict: applicable") || !strings.Contains(out, "not auto-applied") ||
		!strings.Contains(out, "data reach differs (explore questions changed)") || strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatalf("an explore-only change under a plain delegation must be left for a person: %q", out)
	}
	if err := r.command(t, "routine-auto", rpe, "explore-text"); err != nil {
		t.Fatal(err)
	}
	out, _, _ = r.call(t, "e2", "routine_improve", map[string]any{"name": rpe, "script": better, "hypothesis": "h"}, "allow")
	if !strings.Contains(out, "auto-applied") || !strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatalf("with explore-text delegated, the same change is applied: %q", out)
	}
	dec, _ := os.ReadFile(filepath.Join(r.s.Workdir, ".magi/routines/"+rpe+"/decisions.jsonl"))
	if !strings.Contains(string(dec), `"explore_text":true`) {
		t.Fatalf("the delegation must record that explore questions may change:\n%s", dec)
	}
}
