package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func (r *routineRig) put(t *testing.T, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(r.s.Workdir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

const rpe = "recovery-policy-evidence"

// The contract: control flow under fixed fixtures. The recalled policy reaches every explore, an
// empty or failed answer is said to be no answer (not taken as "nothing wrong"), and a branch no
// fixture answers is left unverified rather than passed.
const rpeContract = `{"cases":[
 {"id":"policy-reaches-explore","input":{"questions":["Q-A","Q-B"]},
  "fixtures":[{"tool":"recall_memory","text":"POLICY-R1 an interrupted call is not re-run"},{"tool":"explore","text":"x.go:1 found"}],
  "calls":[{"tool":"recall_memory"},{"tool":"explore","contains":"POLICY-R1"},{"tool":"explore","contains":"POLICY-R1"}],
  "print_contains":["## Q-A","## Q-B","x.go:1 found"]},
 {"id":"empty-answer-is-not-a-finding","input":{"questions":["Q-E"]},
  "fixtures":[{"tool":"recall_memory","text":"POLICY-R1"},{"tool":"explore","text":""}],
  "print_contains":["no answer"]},
 {"id":"failed-explore-is-reported","input":{"questions":["Q-F"]},
  "fixtures":[{"tool":"recall_memory","text":"POLICY-R1"},{"tool":"explore","text":"explore broke","ok":false}],
  "print_contains":["no answer"]},
 {"id":"no-explore-fixture","input":{"questions":["Q-U"]},
  "fixtures":[{"tool":"recall_memory","text":"POLICY-R1"}]}
]}`

// The tasks both versions answer, paired. Explore's answers come from fixtures keyed on how the
// question is asked, so a version that asks for the click path gets the daemon-side evidence, and
// one that stops asking where the code disagrees loses the JetBrains close finding (protected).
func rpeTasks(extra string) []byte {
	recall := map[string]any{"tool": "recall_memory", "text": "POLICY-R1 an interrupted call is not re-run"}
	q := func(s string) map[string]any { return map[string]any{"questions": []string{s}} }
	vsc := func(id, split string) map[string]any {
		return map[string]any{"id": id, "split": split, "input": q(id + ": what does clicking Stop send to the daemon?"),
			"fixtures": []any{recall,
				map[string]any{"tool": "explore", "contains": "click handler", "text": "chat_view.ts:746 click -> main.go:1470 daemon command"},
				map[string]any{"tool": "explore", "text": "chat_view.ts:746 click"}},
			"expect": map[string]any{"evidence": []string{"chat_view.ts:746", "main.go:1470"}}}
	}
	tasks := []any{vsc("VSC-1", "dev"), vsc("VSC-2", "dev"), vsc("VSC-3", "holdout"), vsc("VSC-4", "holdout"), vsc("VSC-5", "holdout"), vsc("VSC-6", "holdout"),
		map[string]any{"id": "WEB-1", "split": "dev", "input": q("WEB-1: where is a tool row drawn?"),
			"fixtures": []any{recall, map[string]any{"tool": "explore", "text": "ConversationElement.java:1372 row"}},
			"expect":   map[string]any{"evidence": []string{"ConversationElement.java:1372"}}},
		map[string]any{"id": "JB-close", "split": "holdout", "protected": true, "input": q("JB-close: does closing the tool window keep the turn?"),
			"fixtures": []any{recall,
				map[string]any{"tool": "explore", "contains": "disagrees with the policy", "text": "MagiToolWindow.kt:210 close keeps the turn"},
				map[string]any{"tool": "explore", "text": "nothing found"}},
			"expect": map[string]any{"evidence": []string{"MagiToolWindow.kt:210"}}},
	}
	b, _ := json.Marshal(map[string]any{"tasks": tasks, "note": extra, "model": "fixture-model", "revision": "fixture-revision"})
	return b
}

const rpeAnswerBranch = `  if not a.ok then
    print("(no answer: " .. tostring(a.error) .. ")")
  elseif a.text == "" then
    print("(no answer: empty response, not a finding)")
  else
    print(a.text)
  end`

// One cycle of improvement on recovery-policy-evidence, with no model and no Engram: each candidate
// is checked against the frozen contract, then paired with the active version on the same tasks; the
// tasks it newly solves and the ones it newly breaks are judged apart. A better candidate is left
// applicable, one that breaks a protected task is rejected, one whose gain is too thin is held, one
// that breaks the contract or widens its tools is rejected. Every attempt is recorded and read back
// for the next one; only the person adopts, and can roll back.
func TestRoutineCandidatesAreCheckedComparedAndJudgedWithTheHistoryKept(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	body, err := os.ReadFile(filepath.Join("..", "..", "plugins", "dummy-plug", "routines", rpe+".md"))
	if err != nil {
		t.Fatal(err)
	}
	r.put(t, ".magi/skills/"+rpe+".md", body)
	dir := ".magi/routines/" + rpe + "/"
	r.put(t, dir+"contract.json", []byte(rpeContract))
	r.put(t, dir+"tasks.json", rpeTasks(""))
	s := string(body)
	script := s[strings.Index(s, "```lua\n")+7 : strings.LastIndex(s, "\n```")]
	improve := func(id, cand string, extra map[string]any) (string, bool) {
		in := map[string]any{"name": rpe, "script": cand, "problem": "VS Code click paths are missed",
			"hypothesis": "asking for the path from the click handler to the daemon command finds the daemon side"}
		for k, v := range extra {
			in[k] = v
		}
		out, asks, failed := r.call(t, id, "routine_improve", in, "allow")
		if len(asks) != 0 {
			t.Fatalf("%s: comparing a candidate asked the person: %+v", id, asks)
		}
		return out, failed
	}
	if out, failed := improve("i0", script, nil); !failed || !strings.Contains(out, "not frozen") {
		t.Fatalf("before the evals are frozen nothing is compared: %q", out)
	}
	if err := r.command(t, "routine-evals-freeze", rpe); err != nil {
		t.Fatal(err)
	}
	better := strings.Replace(script, "Answer with path:line", "Trace from the click handler to the daemon command. Answer with path:line", 1)
	worse := strings.Replace(script, "Answer with path:line evidence from the code, and say where the code disagrees with the policy.",
		"Trace from the click handler to the daemon command. Answer with path:line evidence from the code.", 1)
	thin := strings.Replace(script, `"\n\nQuestion: " .. tostring(q) ..`,
		`"\n\nQuestion: " .. tostring(q) .. (tostring(q):find("VSC-1", 1, true) and " Trace from the click handler to the daemon command." or "") ..`, 1)
	pooledOnly := strings.Replace(script, `"\n\nQuestion: " .. tostring(q) ..`,
		`"\n\nQuestion: " .. tostring(q) .. (tostring(q):find("VSC%-[1-4]:") and " Trace from the click handler to the daemon command." or "") ..`, 1)
	broken := strings.Replace(script, rpeAnswerBranch, "  print(a.text)", 1)
	for name, c := range map[string]string{"better": better, "worse": worse, "thin": thin, "broken": broken} {
		if c == script {
			t.Fatalf("fixture %s did not change the script", name)
		}
	}
	cases := []struct {
		id, src string
		extra   map[string]any
		want    []string
	}{
		{"better", better, nil, []string{"verdict: applicable", "solved 6", "regressed 0"}},
		{"worse", worse, nil, []string{"verdict: rejected", "regressed 1", "JB-close"}},
		{"thin", thin, nil, []string{"verdict: held", "solved 1", "too thin"}},
		{"pooled-only", pooledOnly, nil, []string{"verdict: held", "solved 4", "holdout evidence too thin"}},
		{"broken", broken, nil, []string{"verdict: rejected", "contract", "empty-answer-is-not-a-finding"}},
		{"scope", script + "\n", map[string]any{"tools": []string{"explore", "recall_memory", "grep"}}, []string{"verdict: rejected", "grep"}},
	}
	outs := map[string]string{}
	for _, c := range cases {
		out, failed := improve(c.id, c.src, c.extra)
		if failed {
			t.Fatalf("%s: %q", c.id, out)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Fatalf("%s: want %q in:\n%s", c.id, w, out)
			}
		}
		outs[c.id] = out
	}
	raw, err := os.ReadFile(filepath.Join(r.s.Workdir, dir+"attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 6 {
		t.Fatalf("want 6 attempts recorded, got %d:\n%s", len(lines), raw)
	}
	wantVerdicts := []string{"applicable", "rejected", "held", "held", "rejected", "rejected"}
	for i, line := range lines {
		var rec struct {
			Hypothesis string
			Decision   struct{ Verdict string }
			Tasks      []struct{ ID, Transition string }
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("attempt %d: %v: %s", i+1, err, line)
		}
		if rec.Decision.Verdict != wantVerdicts[i] || rec.Hypothesis == "" {
			t.Fatalf("attempt %d: verdict %q (want %q), hypothesis %q", i+1, rec.Decision.Verdict, wantVerdicts[i], rec.Hypothesis)
		}
		if i == 1 {
			found := false
			for _, tk := range rec.Tasks {
				if tk.ID == "JB-close" && tk.Transition == "regressed" {
					found = true
				}
			}
			if !found {
				t.Fatalf("the regressing attempt must record JB-close as regressed: %s", line)
			}
		}
	}
	if out, _, failed := r.call(t, "h1", "routine_history", map[string]any{"name": rpe}, "allow"); failed ||
		!strings.Contains(out, "rejected") || !strings.Contains(out, "JB-close") {
		t.Fatalf("the next attempt must be able to read why one was rejected: %q", out)
	}
	ver := regexp.MustCompile(`candidate ([0-9a-f]{12})`)
	bv, wv := ver.FindStringSubmatch(outs["better"]), ver.FindStringSubmatch(outs["worse"])
	if bv == nil || wv == nil || bv[1] == wv[1] {
		t.Fatalf("versions: %v %v", bv, wv)
	}
	if err := r.command(t, "routine-adopt", rpe, wv[1]); err == nil {
		t.Fatal("a rejected candidate was adopted")
	}
	versionPath := dir + "versions/" + bv[1] + ".md"
	version, err := os.ReadFile(filepath.Join(r.s.Workdir, versionPath))
	if err != nil {
		t.Fatal(err)
	}
	r.put(t, versionPath, append(append([]byte(nil), version...), '\n'))
	if err := r.command(t, "routine-adopt", rpe, bv[1]); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("adoption must reject modified candidate bytes: %v", err)
	}
	r.put(t, versionPath, version)
	if err := r.command(t, "routine-adopt", rpe, bv[1]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatalf("adopting did not make the candidate active:\n%s", r.skill(t, rpe))
	}
	if err := r.command(t, "routine-rollback", rpe); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.skill(t, rpe), "Trace from the click handler") {
		t.Fatalf("rolling back did not restore the earlier version:\n%s", r.skill(t, rpe))
	}
	r.put(t, dir+"tasks.json", rpeTasks("edited after freezing"))
	if out, failed := improve("t1", better, nil); !failed || !strings.Contains(out, "changed since") {
		t.Fatalf("evals changed after freezing must stop the comparison: %q", out)
	}
}

func TestRoutineCallSavingsRequireHoldoutEvidence(t *testing.T) {
	for _, tc := range []struct{ split, verdict string }{{"dev", "held"}, {"holdout", "applicable"}} {
		t.Run(tc.split, func(t *testing.T) {
			baseline := strings.Replace(routineFile, "local text", `call_tool("read", {path=input.file})`+"\nlocal text", 1)
			r := newRoutineRig(t, t.TempDir(), baseline)
			const dir = ".magi/routines/readit/"
			r.put(t, dir+"contract.json", []byte(`{"cases":[{"id":"reads","input":{"file":"notes.txt"},"fixtures":[{"tool":"read","text":"hello"}],"print_contains":["read notes.txt true"]}]}`))
			var tasks []map[string]any
			for _, file := range []string{"notes.txt", "README.md", "build.log", "settings.toml"} {
				tasks = append(tasks, map[string]any{"id": file, "split": tc.split, "input": map[string]string{"file": file},
					"fixtures": []map[string]string{{"tool": "read", "text": "hello"}}, "expect": map[string][]string{"evidence": {"read " + file + " true"}}})
			}
			data, err := json.Marshal(map[string]any{"tasks": tasks})
			if err != nil {
				t.Fatal(err)
			}
			r.put(t, dir+"tasks.json", data)
			if err := r.command(t, "routine-evals-freeze", "readit"); err != nil {
				t.Fatal(err)
			}
			script := routineFile[strings.Index(routineFile, "\x60\x60\x60lua\n")+7 : strings.LastIndex(routineFile, "\n\x60\x60\x60")]
			out, _, failed := r.call(t, "cost", "routine_improve", map[string]any{"name": "readit", "script": script, "hypothesis": "remove redundant read"}, "allow")
			if failed || !strings.Contains(out, "verdict: "+tc.verdict) {
				t.Fatalf("want %s, got %q", tc.verdict, out)
			}
		})
	}
}
