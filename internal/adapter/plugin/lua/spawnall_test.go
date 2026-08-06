package lua

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// Several children run AT ONCE, not one after another.
//
// A tool call holds the plugin's lock for its whole duration because the Lua state is not
// concurrency-safe, so the parallelism cannot be in Lua — it has to be in Go, entered once and
// left once. This is the test that the concurrency is real rather than a loop wearing its name.
func TestSpawnAllRunsChildrenAtTheSameTime(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local rs = magi.spawn_all{
      { prompt = "one",   workspace = "clone" },
      { prompt = "two",   workspace = "clone" },
      { prompt = "three", workspace = "clone" },
    }
    local out = {}
    for _, r in ipairs(rs) do out[#out+1] = r.session_id .. "|" .. r.text .. "|" .. r.err end
    return table.concat(out, "\n")`)

	var inFlight, peak int32
	var mu sync.Mutex
	var prompts []string
	env := port.ToolEnv{Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(120 * time.Millisecond) // long enough that a sequential loop could not overlap
		atomic.AddInt32(&inFlight, -1)
		mu.Lock()
		prompts = append(prompts, sp.Prompt)
		mu.Unlock()
		if sp.Workspace != "clone" {
			t.Errorf("workspace did not cross: %q", sp.Workspace)
		}
		return port.SpawnResult{SessionID: "child-" + sp.Prompt, Text: "did " + sp.Prompt}, nil
	}}

	start := time.Now()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	elapsed := time.Since(start)

	if peak < 2 {
		t.Errorf("at most %d child ran at a time — this is a loop, not parallelism", peak)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("three 120ms children took %v; sequential would be ~360ms", elapsed)
	}
	if len(prompts) != 3 {
		t.Fatalf("the host saw %d children, want 3", len(prompts))
	}
	// Results come back in the order GIVEN, whatever order they finished in — a caller that wrote
	// three specs has to be able to tell which answer is which.
	out := resultString(t, res)
	for i, want := range []string{"child-one|did one", "child-two|did two", "child-three|did three"} {
		if !strings.Contains(out, want) {
			t.Errorf("row %d missing %q in:\n%s", i, want, out)
		}
	}
	if strings.Index(out, "did one") > strings.Index(out, "did three") {
		t.Errorf("the rows came back out of order:\n%s", out)
	}
}

// One child failing does not throw away the others. They did real work and the caller has to be
// able to keep it; each row says for itself how it ended.
func TestSpawnAllKeepsTheChildrenThatWorked(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local rs = magi.spawn_all{ { prompt = "good" }, { prompt = "bad" } }
    local out = {}
    for _, r in ipairs(rs) do out[#out+1] = r.text .. "/" .. r.err end
    return table.concat(out, " ~ ")`)

	env := port.ToolEnv{Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
		if sp.Prompt == "bad" {
			return port.SpawnResult{SessionID: "c2", Text: "got partway", Err: "child stopped: deadline"}, nil
		}
		return port.SpawnResult{SessionID: "c1", Text: "finished"}, nil
	}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := resultString(t, res)
	if !strings.Contains(out, "finished/") {
		t.Errorf("the child that worked was lost: %q", out)
	}
	if !strings.Contains(out, "deadline") {
		t.Errorf("the failure was not reported per row: %q", out)
	}
}

// review is REFUSED rather than ignored. It re-enters Lua, and several children finishing at once
// would call it from several goroutines into one interpreter — the thing the lock exists to stop.
// Ignoring it silently would leave a caller reading each child's own account as though a judge had
// checked it.
func TestSpawnAllRefusesAReviewInsteadOfDroppingIt(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local v, err = magi.spawn_all{ { prompt = "x", review = function() return nil end } }
    return tostring(v) .. " / " .. tostring(err)`)

	called := false
	env := port.ToolEnv{Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		called = true
		return port.SpawnResult{}, nil
	}}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	out := resultString(t, res)
	if called {
		t.Error("it spawned anyway, with the review quietly dropped")
	}
	if !strings.HasPrefix(out, "nil / ") {
		t.Errorf("it did not refuse: %q", out)
	}
	if !strings.Contains(out, "child_steps") {
		t.Errorf("the refusal does not say what to do instead: %q", out)
	}
}

// Outside a tool call there is no env, and it says so rather than spawning against a stale one.
func TestSpawnAllRefusesOutsideAToolCall(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local v, err = magi.spawn_all{ { prompt = "x" } }
    return tostring(v) .. " / " .. tostring(err)`)
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
	if out := resultString(t, res); !strings.Contains(out, "only available inside a tool call") {
		t.Errorf("outside a tool call it said: %s", out)
	}
}
