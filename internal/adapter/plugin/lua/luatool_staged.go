package lua

import (
	"context"
	"encoding/json"
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A plugin's LState is not safe for concurrent use, so every Lua call holds the plugin's lock. A
// tool whose execute calls magi.spawn holds it for the whole child turn, and two calls to it run
// one after the other however the loop schedules them. A staged tool splits the call:
//
//	prepare(args) -> spawn spec   (Lua, under the lock)
//	                 the child    (Go, the lock released — nothing touches the LState)
//	finish(result) -> tool result (Lua, under the lock)
//
// prepare may refuse instead of returning a spec: anything that is not a table, or a second value
// true, is the tool's result as it stands. Without finish, the child's text is the result and its
// error the tool's error.
func (t *luaTool) executeStaged(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if heldBy(ctx, t.plugin) {
		return t.reentered(), nil
	}
	var args any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return errResult(err.Error()), nil
		}
	}
	if env.Spawn == nil {
		return errResult(fmt.Sprintf("lua tool %q: spawn is only available inside a tool call", t.name)), nil
	}
	sp, early, done := t.prepareSpawn(ctx, env, args)
	if done {
		return early, nil
	}
	// review runs on the child's goroutine and calls into Lua: it takes the lock the stage gave up,
	// with THIS call's env and ctx swapped in as prepare and finish have them (magi.child_steps, the
	// reason a review exists, needs the call's env), and it refuses once the plugin is gone.
	if review := sp.Review; review != nil {
		sp.Review = func(round int, text string, steps int, sessionID string) (why string, err error) {
			if !t.withCall(ctx, env, func(*lua.LState) { why, err = review(round, text, steps, sessionID) }) {
				return "", fmt.Errorf("lua tool %q: %s", t.name, pluginGone)
			}
			return why, err
		}
	}
	res, err := env.Spawn(ctx, sp)
	if err != nil && res.Err == "" {
		res.Err = "spawn: " + err.Error()
	}
	return t.finishSpawn(ctx, env, res), nil
}

// pluginGone is why a stage could not run: the child ran with the plugin's lock released, and in that
// time the plugin was unloaded or reloaded, which closes its Lua state.
const pluginGone = "its plugin was unloaded while the call was in flight"

// withCall runs f under the plugin's lock with this call's env and ctx swapped in, as Execute does.
// It reports false, without running f, when the plugin's Lua state is gone (closed by an unload or a
// reload while the lock was released for the child).
func (t *luaTool) withCall(ctx context.Context, env port.ToolEnv, f func(L *lua.LState)) bool {
	t.plugin.mu.Lock()
	defer t.plugin.mu.Unlock()
	if t.plugin.L == nil {
		return false
	}
	prevEnv, prevCtx := t.plugin.env, t.plugin.callCtx
	t.plugin.env, t.plugin.callCtx = env, holding(ctx, t.plugin)
	defer func() { t.plugin.env, t.plugin.callCtx = prevEnv, prevCtx }()
	f(t.plugin.L)
	return true
}

func (t *luaTool) prepareSpawn(ctx context.Context, env port.ToolEnv, args any) (sp port.SpawnSpec, early session.ToolResult, done bool) {
	ran := t.withCall(ctx, env, func(L *lua.LState) {
		L.Push(t.prepare)
		L.Push(goToLua(L, args))
		if err := L.PCall(1, 2, nil); err != nil {
			early, done = errResult(fmt.Sprintf("lua tool %q: %v", t.name, err)), true
			return
		}
		ret, isErr := L.Get(-2), lua.LVAsBool(L.Get(-1))
		L.Pop(2)
		if tbl, ok := ret.(*lua.LTable); ok && !isErr {
			sp = t.plugin.spawnSpecFrom(L, tbl)
			return
		}
		early, done = luaResult(ret, isErr), true
	})
	if !ran {
		return sp, errResult(fmt.Sprintf("lua tool %q: %s", t.name, pluginGone)), true
	}
	return sp, early, done
}

func (t *luaTool) finishSpawn(ctx context.Context, env port.ToolEnv, res port.SpawnResult) session.ToolResult {
	var out session.ToolResult
	ran := t.withCall(ctx, env, func(L *lua.LState) {
		if t.finish == nil {
			if res.Err != "" {
				out = errResult(res.Err)
				return
			}
			out = luaResult(lua.LString(res.Text), false)
			return
		}
		L.Push(t.finish)
		L.Push(spawnResultTable(L, res))
		if err := L.PCall(1, 2, nil); err != nil {
			out = errResult(fmt.Sprintf("lua tool %q: %v", t.name, err))
			return
		}
		ret, isErr := L.Get(-2), lua.LVAsBool(L.Get(-1))
		L.Pop(2)
		out = luaResult(ret, isErr)
	})
	if !ran {
		// The child's work is not thrown away with the plugin: what it said goes back with the reason.
		got := res.Text
		if res.Err != "" {
			got = "error: " + res.Err
		}
		return errResult(fmt.Sprintf("lua tool %q: %s; the child returned: %s", t.name, pluginGone, got))
	}
	return out
}

func luaResult(ret lua.LValue, isErr bool) session.ToolResult {
	b, _ := json.Marshal(luaToGo(ret))
	return session.ToolResult{Content: b, IsError: isErr}
}
