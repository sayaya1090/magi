package lua

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// dummyPlugRun loads the shipped plugin, enters its mode and runs one script, keeping every list of
// stages the plugin reported while it ran.
func dummyPlugRun(t *testing.T, script string, pause time.Duration) [][]event.ProgressStage {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "dummy-plug"))
	if err != nil {
		t.Fatal(err)
	}
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, ContextReg: &fakeContextReg{}, Notify: func(string, string) {}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load dummy-plug: %v", err)
	}
	get := func(name string) port.Tool {
		tl, ok := sink.Get(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		return tl
	}
	var peak int32
	env := slowEnv(pause, &peak)
	env.SessionID = "stages"
	var mu sync.Mutex
	var reports [][]event.ProgressStage
	env.EmitStages = func(_ string, st []event.ProgressStage) {
		mu.Lock()
		reports = append(reports, append([]event.ProgressStage(nil), st...))
		mu.Unlock()
	}
	ctx := context.Background()
	raw, _ := json.Marshal(map[string]string{"script": script})
	if _, err := get("dummy_plug_run").Execute(ctx, raw, env); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return append([][]event.ProgressStage(nil), reports...)
}

func stagesJSON(st []event.ProgressStage) string { b, _ := json.Marshal(st); return string(b) }

// A planned run shows every step from its first report, each with an id that stays put, so a screen
// keeps its nodes and only updates what changed. A call_all fills its step's counts while it runs.
func TestDummyPlugReportsItsPlannedStepsFromTheStart(t *testing.T) {
	reports := dummyPlugRun(t, `plan({"목록", "읽기", "확인"}) stage("목록") stage("읽기") call_all({ {"read"}, {"read"}, {"read"} }) stage("확인") print("ok")`, 200*time.Millisecond)
	if len(reports) < 3 {
		t.Fatalf("want a report per change, got %d", len(reports))
	}
	first := reports[0]
	if len(first) != 3 || first[0].State != "running" || first[1].State != "waiting" || first[2].State != "waiting" {
		t.Fatalf("the first report must show every planned step: %s", stagesJSON(first))
	}
	for i, r := range reports {
		if len(r) != 3 || r[0].ID != "s1" || r[1].ID != "s2" || r[2].ID != "s3" {
			t.Fatalf("report %d changed the steps or their ids: %s", i, stagesJSON(r))
		}
		if i > 0 && stagesJSON(r) == stagesJSON(reports[i-1]) {
			t.Fatalf("report %d repeats the one before it", i)
		}
	}
	fanned := false
	for _, r := range reports {
		if s := r[1]; s.State == "running" && s.Running > 0 && s.Total == 3 && s.Kind == "tool" {
			fanned = true
		}
	}
	if !fanned {
		t.Errorf("no report showed the read step with calls in flight")
	}
	last := reports[len(reports)-1]
	for _, s := range last {
		if s.State != "done" || s.Running != 0 {
			t.Errorf("a finished run left a step not done: %s", stagesJSON(last))
		}
	}
	if last[1].Done != 3 || last[1].Total != 3 || last[1].Label != "읽기" {
		t.Errorf("the read step lost its count or name: %s", stagesJSON(last))
	}
	for _, w := range []string{"시작", "끝", "{{"} {
		if strings.Contains(stagesJSON(last), w) {
			t.Errorf("a fixed word %q is in the report; the screen says those", w)
		}
	}
}

// Each step says what kind of call it runs; the screen picks the icon.
func TestDummyPlugNamesEachStepsKind(t *testing.T) {
	reports := dummyPlugRun(t, `call_all({ {"explore"} }) call_all({ {"recall_memory"} })`, 0)
	if len(reports) == 0 {
		t.Fatal("no report")
	}
	last := reports[len(reports)-1]
	if len(last) != 2 || last[0].Kind != "subagent" || last[0].Label != "explore" || last[1].Kind != "harness" {
		t.Fatalf("kinds: %s", stagesJSON(last))
	}
}

// A script that fails in a step leaves that step failed.
func TestDummyPlugMarksTheStepAScriptFailedIn(t *testing.T) {
	reports := dummyPlugRun(t, `plan({"a", "b"}) stage("a") error("boom")`, 0)
	if len(reports) == 0 {
		t.Fatal("no report")
	}
	last := reports[len(reports)-1]
	if len(last) != 2 || last[0].State != "failed" || last[1].State != "waiting" {
		t.Fatalf("want a failed and b still waiting: %s", stagesJSON(last))
	}
}
