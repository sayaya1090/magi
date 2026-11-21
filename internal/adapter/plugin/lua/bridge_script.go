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
	"github.com/sayaya1090/magi/internal/port"
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
	// A dry run reaches no tool, so it needs no call to hang off; a real one does.
	if p.env.CallTool == nil && !lua.LVAsBool(opts.RawGetString("dry")) {
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
	// tools = {...} narrows this run to a subset of what the plugin declared: a script run for one
	// purpose (a routine the person approved for these tools) cannot reach the plugin's others.
	if lt, ok := opts.RawGetString("tools").(*lua.LTable); ok {
		only := map[string]bool{}
		for i := 1; i <= lt.Len(); i++ {
			n := lt.RawGetInt(i).String()
			if !declared[n] {
				return fail(L, "run_script: "+n+" is not declared in call_tools (plugin.toml)")
			}
			only[n] = true
		}
		declared = only
	}
	dry := lua.LVAsBool(opts.RawGetString("dry"))
	host := p.env.CallTool
	// child = "task line": run the script in a child session the host opens for it, so its calls are
	// recorded there and not in this conversation's log. A host without children runs it on the call.
	var child *port.ChildRun
	fileFixture := opts.RawGetString("file_fixture")
	fixtureFiles, isolated := fileFixture.(*lua.LTable)
	if fileFixture != lua.LNil && !isolated {
		return fail(L, "run_script: file_fixture must be a table")
	}
	if isolated && (dry || p.env.FileExperiment == nil) {
		return fail(L, "run_script: isolated file experiment unavailable")
	}
	scopeOption := opts.RawGetString("explore_read_scope")
	exploreScope, scoped := scopeOption.(lua.LString)
	if scopeOption != lua.LNil && (!scoped || exploreScope == "") {
		return fail(L, "run_script: explore_read_scope must be a nonempty directory")
	}
	if label, ok := opts.RawGetString("child").(lua.LString); ok && p.env.OpenChild != nil && !dry && !isolated {
		ch, err := p.env.OpenChild(p.spawnCtx(), string(label))
		if err != nil {
			return fail(L, "run_script: "+err.Error())
		}
		child, host = &ch, ch.CallTool
	}
	// dry = true: no tool runs. Each call the script makes is recorded, in order, and answered with an
	// empty text; the record comes back as a third value, {{tool=, args=<json>}, ...}. What a script
	// WOULD do can be compared with what was meant without doing any of it.
	//
	// fixtures = { {tool=, contains=, text=, ok=}, ... } answers a dry call instead: the first fixture of
	// the call's tool whose contains (if any) is in the call's JSON arguments gives its text, as a failed
	// result when ok=false. Its record carries fixture=<index>; a call no fixture answers still gets an
	// empty text and is recorded unverified=true, so a script that branched on it is not taken as checked.
	type fixture struct {
		tool, contains, text string
		failed               bool
	}
	var fixtures []fixture
	if lt, ok := opts.RawGetString("fixtures").(*lua.LTable); ok {
		for i := 1; i <= lt.Len(); i++ {
			ft, ok := lt.RawGetInt(i).(*lua.LTable)
			if !ok {
				return fail(L, fmt.Sprintf("run_script: fixture %d is not a table", i))
			}
			f := fixture{tool: lua.LVAsString(ft.RawGetString("tool")), contains: lua.LVAsString(ft.RawGetString("contains")),
				text: lua.LVAsString(ft.RawGetString("text"))}
			if v := ft.RawGetString("ok"); v != lua.LNil {
				f.failed = !lua.LVAsBool(v)
			}
			if f.tool == "" {
				return fail(L, fmt.Sprintf("run_script: fixture %d has no tool", i))
			}
			fixtures = append(fixtures, f)
		}
	}
	var dryMu sync.Mutex
	var dryCalls []any
	call := func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		if !declared[name] {
			return session.ToolResult{}, fmt.Errorf("%s is not declared in call_tools (plugin.toml)", name)
		}
		if dry {
			rec := map[string]any{"tool": name, "args": string(args)}
			res := session.ToolResult{Content: json.RawMessage(`""`)}
			rec["unverified"] = true
			for i, f := range fixtures {
				if f.tool == name && strings.Contains(string(args), f.contains) {
					text, _ := json.Marshal(f.text)
					res = session.ToolResult{Content: text, IsError: f.failed}
					delete(rec, "unverified")
					rec["fixture"] = i + 1
					break
				}
			}
			dryMu.Lock()
			dryCalls = append(dryCalls, rec)
			dryMu.Unlock()
			return res, nil
		}
		if name == "explore" && exploreScope != "" {
			ctx = port.WithExploreReadScope(ctx, string(exploreScope))
		}
		return host(ctx, name, args)
	}
	// serial = {...} names tools that never run alongside another call in this run, whatever the host
	// would allow. A dry run is wholly serial, so its record is in the order the script asked.
	safe := p.env.ParallelToolSafe
	if lt, ok := opts.RawGetString("serial").(*lua.LTable); ok {
		one := map[string]bool{}
		for i := 1; i <= lt.Len(); i++ {
			one[lt.RawGetInt(i).String()] = true
		}
		inner := safe
		safe = func(name string) bool { return !one[name] && inner != nil && inner(name) }
	}
	if dry || isolated {
		safe = func(string) bool { return false }
	}
	if isolated {
		files := map[string]string{}
		valid := true
		fixtureFiles.ForEach(func(key, val lua.LValue) {
			name, keyOK := key.(lua.LString)
			body, valOK := val.(lua.LString)
			if !keyOK || !valOK {
				valid = false
				return
			}
			files[string(name)] = string(body)
		})
		if !valid {
			return fail(L, "run_script: file_fixture must map paths to text")
		}
		var out string
		var scriptErr error
		var failedCalls int
		state, err := p.env.FileExperiment(p.spawnCtx(), files, func(run port.FileExperimentCall) {
			host = func(ctx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
				result, err := run(ctx, name, args)
				if err != nil || result.IsError {
					failedCalls++
				}
				return result, err
			}
			out, scriptErr = execScript(p.spawnCtx(), call, nil, safe, scriptHooksOf(L, opts), src, timeout, limit)
		})
		if err != nil {
			return fail(L, "run_script: "+err.Error())
		}
		L.Push(lua.LString(out))
		if scriptErr != nil {
			L.Push(lua.LString(scriptErr.Error()))
		} else {
			L.Push(lua.LNil)
		}
		hashes := L.NewTable()
		for path, hash := range state {
			hashes.RawSetString(path, lua.LString(hash))
		}
		L.Push(hashes)
		L.Push(lua.LNumber(failedCalls))
		return 4
	}
	out, err := execScript(p.spawnCtx(), call, p.env.EmitProgress, safe, scriptHooksOf(L, opts), src, timeout, limit)
	if child != nil {
		child.Close(out, err)
	}
	L.Push(lua.LString(out))
	if dry {
		if err != nil {
			L.Push(lua.LString(err.Error()))
		} else {
			L.Push(lua.LNil)
		}
		L.Push(goToLua(L, dryCalls))
		return 3
	}
	if err != nil {
		L.Push(lua.LString(err.Error()))
		return 2
	}
	return 1
}

// scriptJSON is a script value as JSON, refusing a table that holds itself or nests past reason.
func scriptJSON(lv lua.LValue) ([]byte, error) {
	v, err := luaValue(lv)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
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
			v, err := luaValue(t)
			var raw []byte
			if err == nil {
				raw, err = json.Marshal(v)
			}
			if err == nil {
				var wire []struct {
					event.ProgressStage
					Graph bool `json:"graph"`
				}
				err = json.Unmarshal(raw, &wire)
				for _, step := range wire {
					if step.Graph && step.After == nil {
						step.After = new([]string)
						*step.After = []string{}
					}
					stages = append(stages, step.ProgressStage)
				}
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

func execScript(parent context.Context, call toolCaller, note func(string), parallelSafe func(string) bool, hooks scriptHooks, src string, timeout time.Duration, limit int) (string, error) {
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
	installScriptGraph(S, ctx, call, parallelSafe, fire)

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
			raw, err := scriptJSON(t)
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
				raw, err := scriptJSON(t)
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
		workers := scriptParallel
		if parallelSafe != nil {
			for _, job := range jobs {
				if !parallelSafe(job.name) {
					workers = 1
					break
				}
			}
		}
		slots := make(chan struct{}, workers)
		var wg sync.WaitGroup
		var done int32
		for i, j := range jobs {
			work := func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				emit := func(ev map[string]any) {
					if workers == 1 {
						fire(ev)
					} else {
						evs <- ev
					}
				}
				emit(map[string]any{"phase": "start", "tool": j.name, "batch": batch, "index": i + 1, "size": len(jobs)})
				text, ok := toolText(call(ctx, j.name, j.args))
				emit(map[string]any{"phase": "end", "tool": j.name, "batch": batch, "index": i + 1, "size": len(jobs), "ok": ok})
				answers[i] = answer{text, ok}
				note(fmt.Sprintf("call_all %d/%d", atomic.AddInt32(&done, 1), len(jobs)))
			}
			if workers == 1 {
				work()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				work()
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
		raw, err := scriptJSON(L.Get(1))
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
				v, err := luaValue(L.Get(i + 1))
				if err != nil {
					L.RaiseError("%s: argument %d: %v", name, i+1, err)
					return 0
				}
				args[i] = v
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

// magi.approve{reason=, detail={...}} -> true | false | (nil, err)
//
// Asks the person, through the conversation's permission prompt, to approve what the plugin shows:
// reason is the prompt's line, detail its arguments. Only inside a tool call. Approving runs nothing
// and grants nothing else — the calls the plugin makes afterwards pass their own permission gate.
func (p *plugin) bridgeApprove(L *lua.LState) int {
	if p.env.Approve == nil {
		return fail(L, "approve: only available inside a tool call")
	}
	spec := L.CheckTable(1)
	reason := ""
	if v := spec.RawGetString("reason"); v != lua.LNil {
		reason = v.String()
	}
	detail := json.RawMessage(`{}`)
	if t, ok := spec.RawGetString("detail").(*lua.LTable); ok {
		raw, err := scriptJSON(t)
		if err != nil {
			return fail(L, "approve: "+err.Error())
		}
		detail = raw
	}
	ok, err := p.env.Approve(reason, detail)
	if err != nil {
		return fail(L, "approve: "+err.Error())
	}
	L.Push(lua.LBool(ok))
	return 1
}
