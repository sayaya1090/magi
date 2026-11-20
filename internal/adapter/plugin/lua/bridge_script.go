package lua

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

const (
	scriptTimeoutDefault = 60 * time.Second
	scriptTimeoutMax     = 300 * time.Second
	scriptOutputDefault  = 16000
	scriptOutputMax      = 64000
)

// magi.run_script(source, {timeout=seconds, output=bytes}) -> output, err
//
// Runs source — code the MODEL wrote — in a Lua state of its own, never the plugin's: no os, io,
// modules or loaders, and none of magi.*. What it may do is call_tool(name, args), which runs a tool
// through the host's ToolEnv.CallTool (the permission gate, the record, the read-only set), print,
// and json_encode/json_decode. Only what it printed comes back, so what the calls returned stays out
// of the model's context. A failed script still hands back what it printed before it failed.
func (p *plugin) bridgeRunScript(L *lua.LState) int {
	p.requireCap(L, "call_tools")
	src := L.CheckString(1)
	opts := L.OptTable(2, L.NewTable())
	if p.env.CallTool == nil {
		return fail(L, "run_script: only available inside a tool call")
	}
	timeout := scriptTimeoutDefault
	if n, ok := opts.RawGetString("timeout").(lua.LNumber); ok && n > 0 {
		timeout = min(time.Duration(float64(n)*float64(time.Second)), scriptTimeoutMax)
	}
	limit := scriptOutputDefault
	if n, ok := opts.RawGetString("output").(lua.LNumber); ok && n > 0 {
		limit = min(int(n), scriptOutputMax)
	}
	declared := make(map[string]bool, len(p.manifest.CallTools))
	for _, n := range p.manifest.CallTools {
		declared[n] = true
	}
	host := p.env.CallTool
	call := func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		if !declared[name] {
			return session.ToolResult{}, fmt.Errorf("%s is not declared in call_tools (plugin.toml)", name)
		}
		return host(ctx, name, args)
	}
	out, err := execScript(p.spawnCtx(), call, p.env.EmitProgress, scriptHooksOf(L, opts), src, timeout, limit)
	L.Push(lua.LString(out))
	if err != nil {
		L.Push(lua.LString(err.Error()))
		return 2
	}
	return 1
}

type toolCaller func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error)

// scriptParallel bounds how many calls one call_all runs at once.
const scriptParallel = 8

// toolText is what a call hands the script: its text, or the reason it failed.
func toolText(res session.ToolResult, err error) (string, bool) {
	if err != nil {
		return err.Error(), false
	}
	text := string(res.Content)
	var s string
	if json.Unmarshal(res.Content, &s) == nil {
		text = s
	}
	return text, !res.IsError
}

// stagesEvery is the least time between two stage reports magi.progress sends for one call, unless
// the caller forces one: a fan-out of a hundred quick reads would otherwise report a hundred times.
const stagesEvery = 100 * time.Millisecond

// scriptHooks is what a plugin lends a script it runs, and hears back. The host knows no plugin's
// words: globals are the plugin's own functions, onCall hears the facts of each call.
type scriptHooks struct {
	globals map[string]func(args []any) (any, error)
	onCall  func(ev map[string]any)
}

// magi.progress(text[, {stages={...}, force=bool}]) — a note on where this call is, and optionally
// its steps ({id=, label=, kind=, state=, done=, total=, running=} each) that screens keep in place
// and update. A list identical to the last one is not sent again, and between two unforced reports at
// least stagesEvery passes.
func (p *plugin) bridgeProgress(L *lua.LState) int {
	text := L.OptString(1, "")
	var stages []event.ProgressStage
	force := false
	if opts, ok := L.Get(2).(*lua.LTable); ok {
		force = lua.LVAsBool(opts.RawGetString("force"))
		if t, ok := opts.RawGetString("stages").(*lua.LTable); ok {
			raw, err := json.Marshal(luaToGo(t))
			if err == nil {
				err = json.Unmarshal(raw, &stages)
			}
			if err != nil {
				L.RaiseError("magi.progress: stages are not a list of steps: %v", err)
				return 0
			}
		}
	}
	if len(stages) == 0 {
		if p.env.EmitProgress != nil && text != "" {
			p.env.EmitProgress(text)
		}
		return 0
	}
	key, err := json.Marshal(stages)
	if err != nil {
		L.RaiseError("magi.progress: %v", err)
		return 0
	}
	if string(key) == p.progSent || (!force && time.Since(p.progAt) < stagesEvery) {
		return 0
	}
	p.progSent, p.progAt = string(key), time.Now()
	switch {
	case p.env.EmitStages != nil:
		p.env.EmitStages(text, stages)
	case p.env.EmitProgress != nil && text != "":
		p.env.EmitProgress(text)
	}
	return 0
}

// newCall forgets the last stages magi.progress sent: a tool call starts with none.
func (p *plugin) newCall() { p.progSent, p.progAt = "", time.Time{} }

// scriptHooksOf reads globals and on_call from run_script's options. Each runs on the plugin's own
// Lua state, on THIS goroutine: the one running the script, which is inside the tool call that holds
// the plugin's lock — so it is called straight, as a spawn's review is.
func scriptHooksOf(L *lua.LState, opts *lua.LTable) scriptHooks {
	var h scriptHooks
	call := func(fn *lua.LFunction, args []any) (any, error) {
		vals := make([]lua.LValue, len(args))
		for i, a := range args {
			vals[i] = goToLua(L, a)
		}
		if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, vals...); err != nil {
			return nil, err
		}
		ret := L.Get(-1)
		L.Pop(1)
		return luaToGo(ret), nil
	}
	if t, ok := opts.RawGetString("globals").(*lua.LTable); ok {
		h.globals = map[string]func([]any) (any, error){}
		t.ForEach(func(k, v lua.LValue) {
			name, isName := k.(lua.LString)
			fn, isFn := v.(*lua.LFunction)
			if isName && isFn {
				h.globals[string(name)] = func(args []any) (any, error) { return call(fn, args) }
			}
		})
	}
	if fn, ok := opts.RawGetString("on_call").(*lua.LFunction); ok {
		h.onCall = func(ev map[string]any) { _, _ = call(fn, []any{ev}) }
	}
	return h
}

func execScript(parent context.Context, call toolCaller, note func(string), hooks scriptHooks, src string, timeout time.Duration, limit int) (string, error) {
	if note == nil {
		note = func(string) {}
	}
	fire := func(ev map[string]any) {
		if hooks.onCall != nil {
			hooks.onCall(ev)
		}
	}
	batches := 0
	S := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer S.Close()
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.TabLibName, lua.OpenTable}, {lua.StringLibName, lua.OpenString}, {lua.MathLibName, lua.OpenMath}} {
		S.Push(S.NewFunction(lib.open))
		S.Push(lua.LString(lib.name))
		S.Call(1, 0)
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "collectgarbage"} {
		S.SetGlobal(name, lua.LNil)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	S.SetContext(ctx)

	var out strings.Builder
	cut := false
	S.SetGlobal("print", S.NewFunction(func(L *lua.LState) int {
		parts := make([]string, L.GetTop())
		for i := range parts {
			parts[i] = L.ToStringMeta(L.Get(i + 1)).String()
		}
		line := strings.Join(parts, "\t") + "\n"
		if cut {
			return 0
		}
		if out.Len()+len(line) > limit {
			cut = true
			return 0
		}
		out.WriteString(line)
		return 0
	}))
	S.SetGlobal("call_tool", S.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		args := json.RawMessage(`{}`)
		if t, ok := L.Get(2).(*lua.LTable); ok {
			raw, err := json.Marshal(luaToGo(t))
			if err != nil {
				return fail(L, "call_tool: "+err.Error())
			}
			args = raw
		}
		fire(map[string]any{"phase": "start", "tool": name, "batch": 0, "index": 1, "size": 1})
		text, ok := toolText(call(ctx, name, args))
		fire(map[string]any{"phase": "end", "tool": name, "batch": 0, "index": 1, "size": 1, "ok": ok})
		if !ok {
			return fail(L, text)
		}
		L.Push(lua.LString(text))
		return 1
	}))
	S.SetGlobal("progress", S.NewFunction(func(L *lua.LState) int {
		note(L.CheckString(1))
		return 0
	}))
	// call_all({ {name, args} | {name=, args=}, ... }) -> { {ok=, text=} | {ok=false, error=} } in the
	// order asked. The calls run together, at most scriptParallel at once; one that fails does not
	// stop the rest. The Lua state is touched only here, before and after the goroutines.
	S.SetGlobal("call_all", S.NewFunction(func(L *lua.LState) int {
		list := L.CheckTable(1)
		type job struct {
			name string
			args json.RawMessage
		}
		jobs := make([]job, list.Len())
		for i := range jobs {
			item, ok := list.RawGetInt(i + 1).(*lua.LTable)
			if !ok {
				return fail(L, fmt.Sprintf("call_all: call %d is not a table", i+1))
			}
			nv := item.RawGetString("name")
			if nv == lua.LNil {
				nv = item.RawGetInt(1)
			}
			name, ok := nv.(lua.LString)
			if !ok || name == "" {
				return fail(L, fmt.Sprintf("call_all: call %d has no tool name", i+1))
			}
			av := item.RawGetString("args")
			if av == lua.LNil {
				av = item.RawGetInt(2)
			}
			jobs[i] = job{name: string(name), args: json.RawMessage(`{}`)}
			if t, ok := av.(*lua.LTable); ok {
				raw, err := json.Marshal(luaToGo(t))
				if err != nil {
					return fail(L, fmt.Sprintf("call_all: call %d: %v", i+1, err))
				}
				jobs[i].args = raw
			}
		}
		type answer struct {
			text string
			ok   bool
		}
		answers := make([]answer, len(jobs))
		batches++
		batch := batches
		evs := make(chan map[string]any, 2*len(jobs))
		slots := make(chan struct{}, scriptParallel)
		var wg sync.WaitGroup
		var done int32
		for i, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				evs <- map[string]any{"phase": "start", "tool": j.name, "batch": batch, "index": i + 1, "size": len(jobs)}
				text, ok := toolText(call(ctx, j.name, j.args))
				evs <- map[string]any{"phase": "end", "tool": j.name, "batch": batch, "index": i + 1, "size": len(jobs), "ok": ok}
				answers[i] = answer{text, ok}
				note(fmt.Sprintf("call_all %d/%d", atomic.AddInt32(&done, 1), len(jobs)))
			}()
		}
		// The calls' facts reach the plugin here, on the script's goroutine, never from a worker.
		finished := make(chan struct{})
		go func() { wg.Wait(); close(finished) }()
	drain:
		for {
			select {
			case ev := <-evs:
				fire(ev)
			case <-finished:
				for {
					select {
					case ev := <-evs:
						fire(ev)
					default:
						break drain
					}
				}
			}
		}
		out := L.NewTable()
		for _, a := range answers {
			row := L.NewTable()
			row.RawSetString("ok", lua.LBool(a.ok))
			if a.ok {
				row.RawSetString("text", lua.LString(a.text))
			} else {
				row.RawSetString("error", lua.LString(a.text))
			}
			out.Append(row)
		}
		L.Push(out)
		return 1
	}))
	S.SetGlobal("json_encode", S.NewFunction(func(L *lua.LState) int {
		raw, err := json.Marshal(luaToGo(L.Get(1)))
		if err != nil {
			return fail(L, "json_encode: "+err.Error())
		}
		L.Push(lua.LString(raw))
		return 1
	}))
	S.SetGlobal("json_decode", S.NewFunction(func(L *lua.LState) int {
		var v any
		if err := json.Unmarshal([]byte(L.CheckString(1)), &v); err != nil {
			return fail(L, "json_decode: "+err.Error())
		}
		L.Push(goToLua(L, v))
		return 1
	}))

	for name, fn := range hooks.globals {
		S.SetGlobal(name, S.NewFunction(func(L *lua.LState) int {
			args := make([]any, L.GetTop())
			for i := range args {
				args[i] = luaToGo(L.Get(i + 1))
			}
			ret, err := fn(args)
			if err != nil {
				L.RaiseError("%s: %v", name, err)
				return 0
			}
			L.Push(goToLua(L, ret))
			return 1
		}))
	}
	err := S.DoString(src)
	if cut {
		fmt.Fprintf(&out, "[output cut at %d bytes]\n", limit)
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return out.String(), fmt.Errorf("script stopped at its time limit (%s)", timeout)
		}
		return out.String(), fmt.Errorf("script failed: %v", err)
	}
	return out.String(), nil
}

// magi.session_id() -> the conversation this tool call belongs to, "" outside a tool call. A plugin
// that keeps state per conversation (the dummy plug's mode) keys it on this.
func (p *plugin) bridgeSessionID(L *lua.LState) int {
	L.Push(lua.LString(p.env.SessionID))
	return 1
}
