package lua

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slowEnv answers every call after a pause and keeps the most calls it saw running at once.
func slowEnv(pause time.Duration, peak *int32) port.ToolEnv {
	var running int32
	return port.ToolEnv{CallTool: func(_ context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			m := atomic.LoadInt32(peak)
			if n <= m || atomic.CompareAndSwapInt32(peak, m, n) {
				break
			}
		}
		time.Sleep(pause)
		atomic.AddInt32(&running, -1)
		if name == "grep" {
			return session.ToolResult{Content: json.RawMessage(`"no match"`), IsError: true}, nil
		}
		if name == "write" {
			return session.ToolResult{}, errors.New("write is not open to a tool's script")
		}
		b, _ := json.Marshal(name + " " + string(args))
		return session.ToolResult{Content: b}, nil
	}}
}

// call_all runs its calls at the same time and hands the results back in the order they were
// asked for; a call that fails does not stop the others.
func TestCallAllRunsCallsTogetherInOrder(t *testing.T) {
	var peak int32
	start := time.Now()
	got := runScript(t, scriptTool(t), slowEnv(200*time.Millisecond, &peak), map[string]any{"src": `
      local rs = call_all({ {"read", {path="a"}}, {name="grep", args={pattern="x"}}, {"write", {}}, {"list"} })
      for i, r in ipairs(rs) do print(i, tostring(r.ok), r.text or r.error) end`})
	if took := time.Since(start); took > 700*time.Millisecond {
		t.Fatalf("four 200ms calls took %s: they did not run together", took)
	}
	want := "1\ttrue\tread {\"path\":\"a\"}\n2\tfalse\tno match\n3\tfalse\twrite is not open to a tool's script\n4\ttrue\tlist {}\n|ERR:nil"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if peak < 2 {
		t.Fatalf("at most %d call ran at once", peak)
	}
}

// No more than eight run at once, however many are asked for.
func TestCallAllRunsAtMostEightAtOnce(t *testing.T) {
	var peak int32
	got := runScript(t, scriptTool(t), slowEnv(50*time.Millisecond, &peak), map[string]any{"src": `
      local calls = {}
      for i = 1, 20 do calls[i] = {"list", {path = tostring(i)}} end
      local rs = call_all(calls)
      print(#rs, rs[20].text)`})
	if got != "20\tlist {\"path\":\"20\"}\n|ERR:nil" {
		t.Fatalf("got %q", got)
	}
	if peak > 8 || peak < 2 {
		t.Fatalf("%d calls ran at once; want between 2 and 8", peak)
	}
}

// A malformed batch is refused in words, before anything runs.
func TestCallAllRefusesAMalformedBatch(t *testing.T) {
	var peak int32
	got := runScript(t, scriptTool(t), slowEnv(0, &peak), map[string]any{"src": `
      local rs, err = call_all({ {"read"}, {} })
      print(tostring(rs), err)`})
	if !strings.Contains(got, "nil\tcall_all: call 2 has no tool name") || peak != 0 {
		t.Fatalf("got %q, peak %d", got, peak)
	}
}

// A script says where it is: progress(text) and each call_all completion reach the host's
// progress channel, so a client can show the step and how far along it is.
func TestAScriptReportsItsProgress(t *testing.T) {
	var peak int32
	env := slowEnv(10*time.Millisecond, &peak)
	var mu sync.Mutex
	var notes []string
	env.EmitProgress = func(s string) { mu.Lock(); notes = append(notes, s); mu.Unlock() }
	got := runScript(t, scriptTool(t), env, map[string]any{"src": `
      progress("step 2/3: listing")
      call_all({ {"list"}, {"list"}, {"list"} })
      print("ok")`})
	if got != "ok\n|ERR:nil" {
		t.Fatalf("got %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(notes, "|")
	if len(notes) == 0 || notes[0] != "step 2/3: listing" || !strings.Contains(all, "call_all 3/3") {
		t.Fatalf("progress notes: %q", notes)
	}
}
