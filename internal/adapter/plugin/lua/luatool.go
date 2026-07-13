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
}

func (t *luaTool) Name() string            { return t.name }
func (t *luaTool) Description() string     { return t.description }
func (t *luaTool) Schema() json.RawMessage { return t.schema }

// Execute calls the Lua function with the decoded args and converts its return
// value(s) into a ToolResult. The Lua function may return:
//
//	return "text"            -- success, text content
//	return { ... }           -- success, JSON content
//	return content, true     -- second value true marks an error
func (t *luaTool) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	t.plugin.mu.Lock()
	defer t.plugin.mu.Unlock()

	L := t.plugin.L
	// Expose the current workdir/permissions to bridge calls for THIS call only —
	// restore afterwards so observation handlers and context providers (which run
	// outside any tool call) keep the load-time seeded env instead of whatever
	// the last tool call happened to leave behind.
	prevEnv := t.plugin.env
	t.plugin.env = env
	defer func() { t.plugin.env = prevEnv }()

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
