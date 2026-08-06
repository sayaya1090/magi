package lua

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// child_steps answers only inside a tool call, and only with what the host gives it. The plugin
// reads a child's tool calls; it does not get a session log to roam.
func TestChildStepsHandsThePluginTheCallsAndTheFailingOutput(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local r = magi.spawn{prompt = "build"}
    local steps = magi.child_steps(r.session_id)
    local out = {}
    for _, s in ipairs(steps) do
      out[#out+1] = s.name .. "|" .. tostring(s.failed) .. "|" .. tostring(s.args.path or "")
        .. "|" .. tostring(s.output_bytes) .. "|" .. s.output
    end
    return table.concat(out, "\n")`)

	env := port.ToolEnv{
		Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
			return port.SpawnResult{SessionID: "child-1", Text: "gave up"}, nil
		},
		ChildSteps: func(_ context.Context, sid string) ([]port.ChildStep, error) {
			if sid != "child-1" {
				t.Errorf("the plugin asked for %q, not the child it spawned", sid)
			}
			return []port.ChildStep{
				{Name: "read", Args: json.RawMessage(`{"path":"/nope"}`), Failed: true,
					Output: "open /nope: no such file\n  at read", OutputBytes: 32},
				{Name: "list", Args: json.RawMessage(`{"path":"."}`), OutputBytes: 900},
			}, nil
		},
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := resultString(t, res)

	// The failing call arrives with its output intact, newlines and all — that raw text is why the
	// loop is running another round, and it is the one thing it cannot reconstruct.
	if !strings.Contains(got, "open /nope: no such file\n  at read") {
		t.Errorf("the failing output did not reach the plugin verbatim:\n%s", got)
	}
	// Arguments arrive as a table the plugin can read a field from, not as JSON text to parse.
	if !strings.Contains(got, "read|true|/nope|") {
		t.Errorf("the call, its verdict and its arguments did not arrive together:\n%s", got)
	}
	// The succeeding call carries its size but not its bytes.
	if !strings.Contains(got, "list|false|.|900|") {
		t.Errorf("the succeeding call did not arrive with a size and no body:\n%s", got)
	}
}

// Both are tool-call-only, and both SAY SO. They follow the bridge's convention of returning
// (nil, message) rather than raising, so the second return value is where the reason lives — a
// plugin that only checked the first would see nil and could not tell "no children" from "not
// allowed to ask". Reached from an event handler there is no env, and answering out of whatever
// env the last tool call left behind would hand over another call's children.
func TestSpawnAndChildStepsRefuseOutsideAToolCall(t *testing.T) {
	for _, body := range []string{`magi.spawn{prompt="x"}`, `magi.child_steps("child-1")`} {
		_, tl := spawnPlugin(t, `local v, err = `+body+`
      return tostring(v) .. " / " .. tostring(err)`)
		res, err := tl.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		got := resultString(t, res)
		if !strings.HasPrefix(got, "nil / ") {
			t.Errorf("%s returned a value outside a tool call: %s", body, got)
		}
		if !strings.Contains(got, "only available inside a tool call") {
			t.Errorf("%s outside a tool call gave no reason: %s", body, got)
		}
	}
}

// The plugin answers the child the way the council answers the main agent.
//
// review runs on the goroutine that is already inside this tool call and already holding the
// plugin's lock, so it must be called straight rather than through another bridge entry — a
// re-entry would deadlock the plugin against itself on its own callback.
func TestTheReviewCallbackReachesLuaAndItsAnswerComesBack(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local seen = {}
    local r = magi.spawn{ prompt = "build", review = function(round, text, steps)
      seen[#seen+1] = round .. ":" .. text .. ":" .. steps
      if round == 1 then return "not done: tests still red" end
      return nil
    end }
    return table.concat(seen, "|") .. " => " .. (r.text or "")`)

	round := 0
	env := port.ToolEnv{Spawn: func(_ context.Context, sp port.SpawnSpec) (port.SpawnResult, error) {
		if sp.Review == nil {
			t.Fatal("the host was handed no review — the callback never crossed the bridge")
		}
		// Stand in for the run loop: ask, and keep asking while it refuses.
		for {
			round++
			more, err := sp.Review(round, "I think I am done", round*4, "child-1")
			if err != nil {
				return port.SpawnResult{}, err
			}
			if more == "" {
				return port.SpawnResult{Text: "finished on round " + strconv.Itoa(round)}, nil
			}
			if round > 4 {
				t.Fatal("the Lua review never accepted")
			}
		}
	}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := resultString(t, res)
	// Lua saw the round number, the child's text and its step count.
	if !strings.Contains(got, "1:I think I am done:4") {
		t.Errorf("the review did not receive round/text/steps: %s", got)
	}
	// And its refusal came back to the host, then its acceptance ended it.
	if !strings.Contains(got, "finished on round 2") {
		t.Errorf("the refusal or the acceptance did not cross back: %s", got)
	}
}
