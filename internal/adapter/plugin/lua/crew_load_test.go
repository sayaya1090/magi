package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// The crew example loads and its tool registers. An example that does not load is a document that
// looks like code.
func TestCrewExampleLoadsAndRegisters(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(s string) { t.Log(s) }})
	root, err := filepath.Abs("../../../../plugins/examples/crew")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "init.lua")); err != nil {
		t.Fatalf("example not found: %v", err)
	}
	if _, err := h.Load(context.Background(), root); err != nil {
		t.Fatalf("the crew example does not load: %v", err)
	}
	tool, ok := sink.Get("crew_work")
	if !ok {
		t.Fatal("crew_work did not register")
	}
	m := port.ToolMetaOf(tool)
	if !m.Subagent || !m.DefaultOff || m.Group != "crew" {
		t.Errorf("metadata = %+v; want a subagent, off by default, in group crew", m)
	}
}

// The whole loop actually fits together.
//
// This is what the example is FOR. magi ships spawn, isolation, review, footprints, merge and
// restore as separate pieces, and whether they compose was nobody's test until something wrote
// them all in one place. Here a stub host plays every seam: the worker refuses to verify on its
// first ending, the review sends it back, it verifies on the second, and the work is merged.
func TestCrewLoopHoldsTogether(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(s string) { t.Log(s) }})
	root, _ := filepath.Abs("../../../../plugins/examples/crew")
	if _, err := h.Load(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	tool, _ := sink.Get("crew_work")

	var (
		sawClone  string
		sawGroups []string
		rounds    []string
		merged    string
		restored  string
		round     int
	)
	env := port.ToolEnv{
		Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
			sawClone, sawGroups = sp.Workspace, sp.Groups
			if sp.Prompt != "make the parser handle unary minus" {
				t.Errorf("the task was rewritten on the way down: %q", sp.Prompt)
			}
			for {
				round++
				more, err := sp.Review(round, "I believe it is done", round*5, "child-1")
				if err != nil {
					return port.SpawnResult{}, err
				}
				rounds = append(rounds, more)
				if more == "" {
					return port.SpawnResult{SessionID: "child-1", Text: "fixed the parser",
						Workspace: "/tmp/ws", BaseCommit: "aaaaaaa", HeadCommit: "bbbbbbb"}, nil
				}
				if round > 4 {
					t.Fatal("the review never accepted")
				}
			}
		},
		ChildSteps: func(_ context.Context, sid string) ([]port.ChildStep, error) {
			if round == 1 {
				// It edited and never ran anything — the false finish this tree sees most.
				return []port.ChildStep{{Name: "edit", Args: []byte(`{"path":"parser.go"}`)}}, nil
			}
			return []port.ChildStep{
				{Name: "edit", Args: []byte(`{"path":"parser.go"}`)},
				{Name: "bash", Args: []byte(`{"command":"go test ./..."}`)},
			}, nil
		},
		MergeChild:   func(_ context.Context, sid string) error { merged = sid; return nil },
		RestoreChild: func(_ context.Context, sid string) ([]port.RestoredPath, error) { restored = sid; return nil, nil },
	}

	res, err := tool.Execute(context.Background(),
		[]byte(`{"task":"make the parser handle unary minus","verify":"go test ./..."}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := resultString(t, res)

	if sawClone != "clone" {
		t.Errorf("the worker was not given its own checkout: workspace=%q", sawClone)
	}
	if len(sawGroups) != 1 || sawGroups[0] != "crew" {
		t.Errorf("the worker was spawned with groups %v, want [crew]", sawGroups)
	}
	// Round one is refused BECAUSE the footprint shows no verification — not because the worker
	// said the wrong thing. That distinction is the point of reading footprints at all.
	if len(rounds) != 2 || rounds[0] == "" {
		t.Fatalf("the review did not send the unverified ending back: %#v", rounds)
	}
	if !strings.Contains(rounds[0], "검증을 돌린 흔적이 없다") {
		t.Errorf("the refusal did not name the missing verification: %q", rounds[0])
	}
	if merged != "child-1" {
		t.Errorf("accepted work was not merged (merged=%q)", merged)
	}
	if restored != "" {
		t.Errorf("accepted work was also restored away (restored=%q)", restored)
	}
	if !strings.Contains(out, "fixed the parser") {
		t.Errorf("the worker's answer did not reach the caller: %q", out)
	}
}

// A worker that does not finish has its work put back, and what could not be put back is named.
func TestCrewRestoresAWorkerThatDidNotFinish(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	root, _ := filepath.Abs("../../../../plugins/examples/crew")
	if _, err := h.Load(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	tool, _ := sink.Get("crew_work")

	merged := false
	env := port.ToolEnv{
		Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
			return port.SpawnResult{SessionID: "child-2", Text: "got partway",
				Err: "child stopped: context deadline exceeded"}, nil
		},
		ChildSteps: func(context.Context, string) ([]port.ChildStep, error) { return nil, nil },
		MergeChild: func(context.Context, string) error { merged = true; return nil },
		RestoreChild: func(context.Context, string) ([]port.RestoredPath, error) {
			return []port.RestoredPath{
				{Path: "a.go", Restored: true, How: "journal"},
				{Path: "big.bin", Reason: "magi never held this file's contents"},
			}, nil
		},
	}
	res, _ := tool.Execute(context.Background(), []byte(`{"task":"do it"}`), env)
	out := resultString(t, res)

	if merged {
		t.Error("unfinished work was merged")
	}
	// Half a restore reported as a clean one is worse than none.
	if !strings.Contains(out, "big.bin") {
		t.Errorf("what could not be put back was not named: %q", out)
	}
	if !strings.Contains(out, "context deadline exceeded") {
		t.Errorf("why the worker stopped did not reach the caller: %q", out)
	}
}
