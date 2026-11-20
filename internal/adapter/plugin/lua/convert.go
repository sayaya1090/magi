package lua

import (
	"encoding/json"
	"errors"
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

// structuredToLua respects the wire field names of host-owned measurement structs.
func structuredToLua(L *lua.LState, value any) lua.LValue {
	b, err := json.Marshal(value)
	if err != nil {
		return lua.LNil
	}
	var decoded any
	if json.Unmarshal(b, &decoded) != nil {
		return lua.LNil
	}
	return goToLua(L, decoded)
}

// goToLua converts a decoded JSON value (map/slice/string/float64/bool/nil) into
// a Lua value.
func goToLua(L *lua.LState, v any) lua.LValue {
	switch t := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(t)
	case float64:
		return lua.LNumber(t)
	case int:
		return lua.LNumber(t)
	case int64:
		return lua.LNumber(t)
	case string:
		return lua.LString(t)
	case map[string]any:
		tbl := L.NewTable()
		for k, val := range t {
			tbl.RawSetString(k, goToLua(L, val))
		}
		return tbl
	case []any:
		tbl := L.NewTable()
		for i, val := range t {
			tbl.RawSetInt(i+1, goToLua(L, val))
		}
		return tbl
	case []string:
		tbl := L.NewTable()
		for i, s := range t {
			tbl.RawSetInt(i+1, lua.LString(s))
		}
		return tbl
	default:
		return lua.LNil
	}
}

// luaDepthMax is how deep a table may nest before conversion gives up: past it the value is not
// data a tool takes, and Go's stack, not Lua's limits, would be what ends the walk.
const luaDepthMax = 100

// luaToGo converts a Lua value into a JSON-marshalable Go value. Tables become
// arrays when they have only sequential integer keys, otherwise objects. A table that holds itself,
// or one nested past luaDepthMax, converts to nil; use luaValue to say why.
func luaToGo(lv lua.LValue) any {
	v, err := luaValue(lv)
	if err != nil {
		return nil
	}
	return v
}

// luaValue is luaToGo that says why a value cannot be converted: a table that holds itself (a
// cycle) or one nested past luaDepthMax. A table used twice side by side is not a cycle.
func luaValue(lv lua.LValue) (any, error) {
	c := luaConv{path: map[*lua.LTable]bool{}}
	v := c.value(lv, 0)
	if c.err != nil {
		return nil, c.err
	}
	return v, nil
}

// luaConv walks one value, keeping the tables on the current path.
type luaConv struct {
	path map[*lua.LTable]bool
	err  error
}

func (c *luaConv) value(lv lua.LValue, depth int) any {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case lua.LString:
		return string(v)
	case *lua.LTable:
		return c.table(v, depth)
	default:
		return nil
	}
}

func (c *luaConv) table(t *lua.LTable, depth int) any {
	switch {
	case c.err != nil:
		return nil
	case c.path[t]:
		c.err = errors.New("a table holds itself (cycle); it cannot be converted")
		return nil
	case depth >= luaDepthMax:
		c.err = fmt.Errorf("a table is nested too deep (over %d levels)", luaDepthMax)
		return nil
	}
	c.path[t] = true
	defer delete(c.path, t)
	// Detect array: all keys 1..n.
	n := t.Len()
	isArray := n > 0
	count := 0
	t.ForEach(func(k, _ lua.LValue) {
		count++
		if _, ok := k.(lua.LNumber); !ok {
			isArray = false
		}
	})
	if isArray && count == n {
		arr := make([]any, 0, n)
		for i := 1; i <= n; i++ {
			arr = append(arr, c.value(t.RawGetInt(i), depth+1))
		}
		return arr
	}
	obj := map[string]any{}
	t.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			obj[string(ks)] = c.value(v, depth+1)
		}
	})
	return obj
}
