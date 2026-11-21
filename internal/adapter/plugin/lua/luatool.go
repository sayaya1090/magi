package lua

import (
	"context"
	"encoding/json"
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// luaTool adapts a Lua-registered tool to port.Tool. Because a gopher-lua LState
// is not safe for concurrent use, every Execute serializes on the owning
// plugin's mutex.
type luaTool struct {
	plugin      *plugin
	name        string
	description string
	schema      json.RawMessage
	fn          *lua.LFunction
	// prepare and finish stage a tool that runs one child: see luatool_staged.go.
	prepare, finish *lua.LFunction
	meta            port.ToolMetadata
	background      bool
}

func (t *luaTool) Name() string            { return t.name }
func (t *luaTool) Description() string     { return t.description }
func (t *luaTool) Schema() json.RawMessage { return t.schema }

// Meta reports what the plugin declared about this tool: whether it runs a child agent, which
// group it belongs to in the /subagents list, and whether it is hidden from the main agent.
func (t *luaTool) Meta() port.ToolMetadata { return t.meta }

// reentered is the answer to a call into this tool's plugin from a chain that already holds its lock.
func (t *luaTool) reentered() session.ToolResult {
	return errResult(fmt.Sprintf("lua tool %q: plugin %q is already running a tool further up this call; "+
		"a call back into it would wait on itself forever, so it is refused", t.name, t.plugin.name))
}

// Execute calls the Lua function with the decoded args and converts its return
// value(s) into a ToolResult. The Lua function may return:
//
//	return "text"            -- success, text content
//	return { ... }           -- success, JSON content
//	return content, true     -- second value true marks an error
func (t *luaTool) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if t.background {
		var options struct {
			Background bool `json:"background"`
		}
		if json.Unmarshal(raw, &options) == nil && options.Background {
			if heldBy(ctx, t.plugin) {
				return t.reentered(), nil
			}
			if env.StartBackground == nil {
				return errResult("background execution is only available for a top-level tool call"), nil
			}
			start := env.StartBackground
			env.StartBackground, env.ObserveBackground = nil, nil
			err := start(func(jobCtx context.Context) port.BackgroundCompletion {
				result, err := t.executeCall(jobCtx, raw, env, true)
				if err != nil {
					return port.BackgroundCompletion{Exit: 1, Detail: err.Error()}
				}
				var text string
				if json.Unmarshal(result.Content, &text) != nil {
					text = string(result.Content)
				}
				status := port.BackgroundCompletion{Detail: text}
				if result.IsError {
					status.Exit = 1
				}
				return status
			})
			if err != nil {
				return errResult(err.Error()), nil
			}
			return session.ToolResult{Content: json.RawMessage(`"Started in the background. The result will arrive in a completion notice."`), Background: true}, nil
		}
	}
	return t.execute(ctx, raw, env)
}

func (t *luaTool) execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	return t.executeCall(ctx, raw, env, false)
}

func (t *luaTool) executeCall(ctx context.Context, raw json.RawMessage, env port.ToolEnv, background bool) (session.ToolResult, error) {
	if t.prepare != nil {
		return t.executeStaged(ctx, raw, env)
	}
	if heldBy(ctx, t.plugin) {
		return t.reentered(), nil
	}
	t.plugin.mu.Lock()
	defer t.plugin.mu.Unlock()

	if err := ctx.Err(); background && err != nil {
		return errResult(err.Error()), nil
	}
	L := t.plugin.L
	if L == nil {
		return errResult(pluginGone), nil
	}
	// Expose the current workdir/permissions to bridge calls for THIS call only —
	// restore afterwards so observation handlers and context providers (which run
	// outside any tool call) keep the load-time seeded env instead of whatever
	// the last tool call happened to leave behind.
	prevEnv, prevCtx := t.plugin.env, t.plugin.callCtx
	t.plugin.env, t.plugin.callCtx = env, holding(ctx, t.plugin)
	t.plugin.newCall()
	defer func() { t.plugin.env, t.plugin.callCtx = prevEnv, prevCtx }()

	var args any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return errResult(err.Error()), nil
		}
	}

	L.Push(t.fn)
	L.Push(goToLua(L, args))
	if err := L.PCall(1, 2, nil); err != nil {
		return errResult(fmt.Sprintf("lua tool %q: %v", t.name, err)), nil
	}
	ret := L.Get(-2)
	isErr := lua.LVAsBool(L.Get(-1))
	L.Pop(2)

	content := luaToGo(ret)
	var b []byte
	switch c := content.(type) {
	case string:
		b, _ = json.Marshal(c)
	default:
		b, _ = json.Marshal(c)
	}
	return session.ToolResult{Content: b, IsError: isErr}, nil
}

func errResult(msg string) session.ToolResult {
	b, _ := json.Marshal(msg)
	return session.ToolResult{Content: b, IsError: true}
}
