package lua

import (
	"errors"
	"fmt"

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
	c.plugin.mu.Lock()
	defer c.plugin.mu.Unlock()

	L := c.plugin.L
	L.Push(c.fn)
	tbl := L.NewTable()
	for _, a := range args {
		tbl.Append(lua.LString(a))
	}
	L.Push(tbl)
	if err := L.PCall(1, 1, nil); err != nil {
		return fmt.Errorf("lua command %q: %w", c.name, err)
	}
	ret := L.Get(-1)
	L.Pop(1)
	if s, ok := ret.(lua.LString); ok && string(s) != "" {
		return errors.New(string(s))
	}
	return nil
}
