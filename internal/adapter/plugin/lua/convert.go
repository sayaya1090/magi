package lua

import (
	lua "github.com/yuin/gopher-lua"
)

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

// luaToGo converts a Lua value into a JSON-marshalable Go value. Tables become
// arrays when they have only sequential integer keys, otherwise objects.
func luaToGo(lv lua.LValue) any {
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
		return tableToGo(v)
	default:
		return nil
	}
}

func tableToGo(t *lua.LTable) any {
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
			arr = append(arr, luaToGo(t.RawGetInt(i)))
		}
		return arr
	}
	obj := map[string]any{}
	t.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			obj[string(ks)] = luaToGo(v)
		}
	})
	return obj
}
