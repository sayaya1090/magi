package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type trialJudge struct{}

func (trialJudge) Analyze(_ context.Context, _, text, _ string) (string, error) {
	return `{"preferred":"candidate","observed_effect":"helped","reason":"the candidate tool output contains candidate; this does not establish correctness"}`, nil
}

// Run the actual Lua strategy over the real app loop, builtin write/bash,
// JSONL evidence and pinned Git checkouts. Only model responses are scripted.
func TestHarnessLabRealExecutionKeepsBothArtifactsOutsideParent(t *testing.T) {
	var script [][]port.ProviderEvent
	for _, body := range []string{"baseline", "candidate"} {
		args, _ := json.Marshal(map[string]string{"path": "result.txt", "content": body})
		script = append(script,
			[]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "write", Name: "write", Args: args}}, {Type: port.ProviderFinish}},
			[]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "tamper", Name: "write", Args: json.RawMessage(`{"path":"verify.sh","content":"exit 0\n"}`)}}, {Type: port.ProviderFinish}},
			[]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "check", Name: "bash", Args: json.RawMessage(`{"command":"cat result.txt"}`)}}, {Type: port.ProviderFinish}},
			[]port.ProviderEvent{{Type: port.ProviderText, Text: "wrote and read the artifact"}, {Type: port.ProviderFinish}})
	}
	a, parent, _ := spawnApp(t, &fakeLLM{steps: script})
	parent.Workdir = gitRepo(t)
	if err := os.WriteFile(filepath.Join(parent.Workdir, "verify.sh"), []byte("test \"$(cat result.txt)\" = candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), Analyzer: trialJudge{}, DataDir: t.TempDir(),
		Runtime: luaplugin.RuntimeInfo{Workdir: parent.Workdir}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "plugins", "s2-engine")); err != nil {
		t.Fatal(err)
	}
	tool, ok := a.tools.Get("s2_compare")
	if !ok {
		t.Fatal("tool not loaded")
	}
	hooks := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent}, "experiment", tool.Name())
	defer hooks.Close()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"write result.txt and read it back","guidance":"read back the written file","max_steps":10,"verify_path":"verify.sh"}`), port.ToolEnv{
		Workdir: parent.Workdir, Spawn: hooks.Spawn, ChildSteps: hooks.Steps,
	})
	if err != nil || res.IsError {
		t.Fatalf("comparison: %s %v", res.Content, err)
	}
	var record struct {
		Status              string
		Baseline, Candidate struct {
			Workspace, Snapshot, Session string
			Verification                 *port.TrialVerification
			Calls                        []struct{ Output string }
		}
	}
	if err := json.Unmarshal(res.Content, &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != "compared" || record.Baseline.Snapshot != record.Candidate.Snapshot || record.Baseline.Snapshot == "" {
		t.Fatalf("unmatched experiment: %s", res.Content)
	}
	if record.Baseline.Verification == nil || record.Candidate.Verification == nil || record.Baseline.Verification.Status != "failed" || record.Candidate.Verification.Status != "passed" || record.Baseline.Verification.Digest != record.Candidate.Verification.Digest {
		t.Fatalf("candidate-edited checker bypassed pinned verification: %s", res.Content)
	}
	for i, run := range []struct{ dir, want string }{{record.Baseline.Workspace, "baseline"}, {record.Candidate.Workspace, "candidate"}} {
		t.Cleanup(func() { os.RemoveAll(filepath.Dir(run.dir)) })
		b, err := os.ReadFile(filepath.Join(run.dir, "result.txt"))
		if err != nil || string(b) != run.want {
			t.Fatalf("artifact %d: %q %v", i, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(parent.Workdir, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("experiment modified parent: %v", err)
	}
	if !strings.Contains(string(res.Content), "exit 0") || !strings.Contains(string(res.Content), "candidate") {
		t.Fatalf("actual tool result absent: %s", res.Content)
	}
}

// Scenario: repeated training -> unseen task -> manual adoption -> search ->
// explicit reuse -> withdrawal. Also exercise a holdout regression through real
// bash verification, rather than injecting verifier outcomes into the plugin.
func TestHarnessLabScenarioLifecycle(t *testing.T) {
	for _, regression := range []bool{false, true} {
		name := "adopt_reuse_withdraw"
		if regression {
			name = "holdout_regression_blocks_adoption"
		}
		t.Run(name, func(t *testing.T) {
			var script [][]port.ProviderEvent
			for _, body := range []string{"baseline", "candidate", "candidate", "baseline", "candidate", "baseline", "baseline", "candidate"} {
				args, _ := json.Marshal(map[string]string{"path": "result.txt", "content": body})
				script = append(script,
					[]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "write", Name: "write", Args: args}}, {Type: port.ProviderFinish}},
					[]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "check", Name: "bash", Args: json.RawMessage(`{"command":"cat result.txt"}`)}}, {Type: port.ProviderFinish}},
					[]port.ProviderEvent{{Type: port.ProviderText, Text: "artifact written and inspected"}, {Type: port.ProviderFinish}})
			}
			a, parent, _ := spawnApp(t, &fakeLLM{steps: script})
			parent.Workdir = gitRepo(t)
			for path, want := range map[string]string{"verify.sh": "candidate", "holdout.sh": "candidate"} {
				if regression && path == "holdout.sh" {
					want = "baseline"
				}
				if err := os.WriteFile(filepath.Join(parent.Workdir, path), []byte("test \"$(cat result.txt)\" = "+want+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), Analyzer: trialJudge{}, DataDir: t.TempDir(), Runtime: luaplugin.RuntimeInfo{Workdir: parent.Workdir}, Logf: func(string) {}})
			if _, err := h.Load(context.Background(), filepath.Join("..", "..", "plugins", "s2-engine")); err != nil {
				t.Fatal(err)
			}
			compare, _ := a.tools.Get("s2_compare")
			type run struct {
				Workspace, Arm, Phase string
				Verification          *port.TrialVerification
			}
			type report struct {
				ID, Status, Adoption string
				Assessment           struct{ State string }
				Runs                 []run
			}
			execute := func(args []byte) report {
				t.Helper()
				hooks := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent}, newID(), compare.Name())
				defer hooks.Close()
				result, err := compare.Execute(context.Background(), args, port.ToolEnv{Workdir: parent.Workdir, Spawn: hooks.Spawn, ChildSteps: hooks.Steps})
				if err != nil || result.IsError {
					t.Fatalf("experiment: %s %v", result.Content, err)
				}
				var r report
				if err := json.Unmarshal(result.Content, &r); err != nil {
					t.Fatal(err)
				}
				for _, run := range r.Runs {
					dir := run.Workspace
					t.Cleanup(func() { cleanupTrialCheckout(t, dir) })
				}
				return r
			}
			r := execute([]byte(`{"task":"write and inspect the training artifact","guidance":"inspect the expected output before writing","verify_path":"verify.sh","repetitions":2,"holdouts":[{"task":"write and inspect a separate artifact case","verify_path":"holdout.sh"}],"max_steps":8}`))
			want := "eligible"
			if regression {
				want = "regressed"
			}
			if r.Status != "compared" || r.Assessment.State != want || len(r.Runs) != 6 {
				t.Fatalf("unexpected scenario result: %+v", r)
			}
			handled, err := h.DispatchCommand("s2-engine-adopt", []string{r.ID, "reviewed all six executions"})
			if !handled || (err != nil) != regression {
				t.Fatalf("adoption handled=%v err=%v", handled, err)
			}
			t.Logf("six real executions: assessment=%s; adoption accepted=%v", r.Assessment.State, err == nil)
			if regression {
				return
			}
			search, _ := a.tools.Get("s2_search")
			found, err := search.Execute(context.Background(), json.RawMessage(`{"query":"training artifact"}`), port.ToolEnv{Workdir: parent.Workdir})
			if err != nil || found.IsError || !strings.Contains(string(found.Content), r.ID) || !strings.Contains(string(found.Content), `"adoption":"adopted"`) {
				t.Fatalf("adopted experience not searchable: %s %v", found.Content, err)
			}
			reuseArgs, _ := json.Marshal(map[string]any{"task": "write and inspect another artifact", "reuse_report": r.ID, "verify_path": "verify.sh", "max_steps": 8})
			reused := execute(reuseArgs)
			if reused.Status != "compared" || len(reused.Runs) != 2 {
				t.Fatalf("reuse failed: %+v", reused)
			}
			if _, err := h.DispatchCommand("s2-engine-withdraw", []string{r.ID, "withdraw after review"}); err != nil {
				t.Fatal(err)
			}
			denied, err := compare.Execute(context.Background(), reuseArgs, port.ToolEnv{Workdir: parent.Workdir})
			if err != nil || !denied.IsError || !strings.Contains(string(denied.Content), "not adopted") {
				t.Fatalf("withdrawn guidance reused: %s %v", denied.Content, err)
			}
			other, err := search.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{Workdir: t.TempDir()})
			if err != nil || other.IsError || strings.Contains(string(other.Content), r.ID) {
				t.Fatalf("experience leaked across workspace: %s %v", other.Content, err)
			}
			if _, err := os.Stat(filepath.Join(parent.Workdir, "result.txt")); !os.IsNotExist(err) {
				t.Fatalf("parent modified: %v", err)
			}
			t.Log("search, adopted reuse, withdrawal, workspace isolation and parent preservation passed")
		})
	}
}

func TestHarnessLabCheckpointBranchesRealExecution(t *testing.T) {
	for _, kind := range []string{"git", "directory"} {
		t.Run(kind, func(t *testing.T) {
			var script [][]port.ProviderEvent
			for _, body := range []string{"baseline", "candidate"} {
				args, _ := json.Marshal(map[string]string{"path": "artifact.txt", "content": body})
				script = append(script, []port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "write", Name: "write", Args: args}}, {Type: port.ProviderFinish}}, []port.ProviderEvent{{Type: port.ProviderText, Text: "artifact written"}, {Type: port.ProviderFinish}})
			}
			ctx := context.Background()
			a, s, _ := spawnApp(t, &fakeLLM{steps: script})
			s.Workdir = t.TempDir()
			if kind == "git" {
				s.Workdir = gitRepo(t)
			}
			a.cfg.StoreDir = t.TempDir()
			os.WriteFile(filepath.Join(s.Workdir, "verify.sh"), []byte("test \"$(cat artifact.txt)\" = candidate\n"), 0600)
			host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), Analyzer: trialJudge{}, DataDir: t.TempDir(), Runtime: luaplugin.RuntimeInfo{Workdir: s.Workdir}, Logf: func(string) {}})
			if _, err := host.Load(ctx, filepath.Join("..", "..", "plugins", "s2-engine")); err != nil {
				t.Fatal(err)
			}
			control, _ := a.tools.Get("s2_checkpoint")
			ops := a.checkpointFn(0, s)
			call := func(args any) port.CheckpointResult {
				t.Helper()
				raw, _ := json.Marshal(args)
				result, err := control.Execute(ctx, raw, port.ToolEnv{Workdir: s.Workdir, Checkpoint: ops})
				if err != nil || result.IsError {
					t.Fatalf("checkpoint tool: %s %v", result.Content, err)
				}
				var r port.CheckpointResult
				if err := json.Unmarshal(result.Content, &r); err != nil {
					t.Fatal(err)
				}
				return r
			}
			start := call(map[string]string{"action": "begin", "kind": kind})
			t.Cleanup(func() { os.RemoveAll(start.Recovery) })
			compare, _ := a.tools.Get("s2_compare")
			hooks := a.spawnFnFor(0, s, event.Actor{Kind: event.ActorAgent}, newID(), compare.Name())
			defer hooks.Close()
			result, err := compare.Execute(ctx, json.RawMessage(`{"task":"write artifact.txt","guidance":"inspect expected output first","verify_path":"verify.sh","max_steps":6}`), port.ToolEnv{Workdir: s.Workdir, Checkpoint: ops, Spawn: hooks.Spawn, ChildSteps: hooks.Steps})
			if err != nil || result.IsError {
				t.Fatalf("comparison: %s %v", result.Content, err)
			}
			var r struct {
				Baseline, Candidate struct {
					Branch, Workspace string
					Verification      *port.TrialVerification
				}
			}
			if err := json.Unmarshal(result.Content, &r); err != nil {
				t.Fatal(err)
			}
			if r.Baseline.Branch == "" || r.Candidate.Branch == "" || r.Baseline.Workspace == r.Candidate.Workspace || r.Baseline.Verification.Status != "failed" || r.Candidate.Verification.Status != "passed" {
				t.Fatalf("invalid branches: %s", result.Content)
			}
			call(map[string]string{"action": "adopt", "branch": r.Candidate.Branch})
			next := call(map[string]string{"action": "fork"})
			data, _ := os.ReadFile(filepath.Join(next.Path, "artifact.txt"))
			if string(data) != "candidate" {
				t.Fatalf("next checkpoint: %s", data)
			}
			call(map[string]string{"action": "finish"})
			if _, err := os.Stat(filepath.Join(s.Workdir, "artifact.txt")); !os.IsNotExist(err) {
				t.Fatal("premature publication")
			}
			a.finishCheckpoints(ctx, s.ID)
			data, _ = os.ReadFile(filepath.Join(s.Workdir, "artifact.txt"))
			if string(data) != "candidate" {
				t.Fatalf("final publication failed: %s", data)
			}
			status := call(map[string]string{"action": "status"})
			if status.Phase != "cleaned" {
				t.Fatalf("cleanup failed: %+v", status)
			}
		})
	}
}
