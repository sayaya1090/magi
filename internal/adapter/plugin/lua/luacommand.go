package lua

import (
	"errors"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// luaCommand adapts a Lua-registered slash command to port.PluginCommand. Like
// luaTool, every Execute serializes on the owning plugin's mutex because a
// gopher-lua LState is not safe for concurrent use.
type luaCommand struct {
	plugin      *plugin
	name        string
	description string
	fn          *lua.LFunction
}

func (c *luaCommand) Name() string        { return c.name }
func (c *luaCommand) Description() string { return c.description }

// Execute calls the Lua function with the arg tokens as an array table. The Lua
// function signals failure by returning a non-empty string (its error message);
// returning nil / nothing means success.
func (c *luaCommand) Execute(args []string) error {
	_, err := c.run("", args, false)
	return err
}

// run is Execute that can also hand back what the command logged while it ran (capture), for the
// conversation session (empty: none named).
//
// The command is looked up under the host lock and run under the plugin lock, and between the two
// the plugin can be unloaded or reloaded, which closes its Lua state. That is checked here, under
// the lock that close() also takes, so the command answers instead of reaching a closed state.
func (c *luaCommand) run(session string, args []string, capture bool) (string, error) {
	c.plugin.mu.Lock()
	defer c.plugin.mu.Unlock()
	if c.plugin.L == nil {
		return "", fmt.Errorf("plugin %q was unloaded or reloaded; run /%s again", c.plugin.name, c.name)
	}
	c.plugin.commandSession = session
	defer func() { c.plugin.commandSession = "" }()
	var out strings.Builder
	if capture {
		c.plugin.capture = &out
		defer func() { c.plugin.capture = nil }()
	}

	L := c.plugin.L
	L.Push(c.fn)
	tbl := L.NewTable()
	for _, a := range args {
		tbl.Append(lua.LString(a))
	}
	L.Push(tbl)
	if err := L.PCall(1, 1, nil); err != nil {
		return out.String(), fmt.Errorf("lua command %q: %w", c.name, err)
	}
	ret := L.Get(-1)
	L.Pop(1)
	if s, ok := ret.(lua.LString); ok && string(s) != "" {
		return out.String(), errors.New(string(s))
	}
	return out.String(), nil
}
