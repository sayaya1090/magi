package lua

import (
	"reflect"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestGoToLuaScalars(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	cases := []struct {
		in   any
		want lua.LValue
	}{
		{nil, lua.LNil},
		{true, lua.LBool(true)},
		{float64(1.5), lua.LNumber(1.5)},
		{int(3), lua.LNumber(3)},
		{int64(7), lua.LNumber(7)},
		{"hi", lua.LString("hi")},
		{int32(9), lua.LNil},   // unsupported type → LNil (the default branch)
		{struct{}{}, lua.LNil}, // unsupported type → LNil
	}
	for _, c := range cases {
		if got := goToLua(L, c.in); got != c.want {
			t.Errorf("goToLua(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGoToLuaSlicesAndMap(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// []any → 1-indexed Lua array.
	arr := goToLua(L, []any{"a", float64(2)}).(*lua.LTable)
	if arr.Len() != 2 || arr.RawGetInt(1).String() != "a" || arr.RawGetInt(2) != lua.LNumber(2) {
		t.Fatalf("[]any conversion wrong: len=%d", arr.Len())
	}
	// []string → 1-indexed Lua array.
	ss := goToLua(L, []string{"x", "y"}).(*lua.LTable)
	if ss.Len() != 2 || ss.RawGetInt(1) != lua.LString("x") {
		t.Fatalf("[]string conversion wrong")
	}
	// map[string]any → keyed table.
	m := goToLua(L, map[string]any{"k": "v"}).(*lua.LTable)
	if m.RawGetString("k") != lua.LString("v") {
		t.Fatalf("map conversion wrong")
	}
}

func TestLuaToGoScalars(t *testing.T) {
	if luaToGo(lua.LNil) != nil {
		t.Error("LNil should be nil")
	}
	if luaToGo(lua.LBool(true)) != true {
		t.Error("LBool")
	}
	// Lua numbers are always float64 on the way back, even integral ones.
	if v, ok := luaToGo(lua.LNumber(3)).(float64); !ok || v != 3 {
		t.Errorf("LNumber should be float64(3), got %#v", luaToGo(lua.LNumber(3)))
	}
	if luaToGo(lua.LString("s")) != "s" {
		t.Error("LString")
	}
}

func TestTableToGoArrayVsObject(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Sequential 1..n integer keys → array.
	arr := L.NewTable()
	arr.RawSetInt(1, lua.LNumber(10))
	arr.RawSetInt(2, lua.LNumber(20))
	got := luaToGo(arr)
	if !reflect.DeepEqual(got, []any{float64(10), float64(20)}) {
		t.Fatalf("array table = %#v, want []any{10,20}", got)
	}

	// String keys → object.
	obj := L.NewTable()
	obj.RawSetString("a", lua.LNumber(1))
	obj.RawSetString("b", lua.LString("two"))
	if !reflect.DeepEqual(luaToGo(obj), map[string]any{"a": float64(1), "b": "two"}) {
		t.Fatalf("object table = %#v", luaToGo(obj))
	}

	// Empty table → object (n==0 makes isArray false), NOT an empty array. Documents the
	// chosen semantics so a JSON consumer gets {} not [].
	if got := luaToGo(L.NewTable()); !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("empty table = %#v, want empty map", got)
	}
}

// TestTableToGoMixedDropsIntKeys documents a real edge: a table with BOTH integer and
// string keys is treated as an object, and the object branch keeps only string keys — the
// integer-keyed entries are dropped (JSON objects can't have integer keys).
func TestTableToGoMixedDropsIntKeys(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mixed := L.NewTable()
	mixed.RawSetInt(1, lua.LString("dropped"))
	mixed.RawSetString("kept", lua.LString("v"))
	if got := luaToGo(mixed); !reflect.DeepEqual(got, map[string]any{"kept": "v"}) {
		t.Fatalf("mixed table = %#v, want only the string key", got)
	}
}

// TestRoundTrip: a JSON-shaped Go value survives goToLua → luaToGo unchanged (numbers as
// float64), including nesting.
func TestRoundTrip(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	in := map[string]any{
		"name":  "magi",
		"count": float64(3),
		"flag":  true,
		"tags":  []any{"a", "b"},
		"inner": map[string]any{"x": float64(1)},
		// Tables nested INSIDE an array element exercise the array-branch recursion
		// (tableToGo's array path calling luaToGo on a sub-table).
		"matrix": []any{[]any{float64(1), float64(2)}, map[string]any{"k": "v"}},
	}
	out := luaToGo(goToLua(L, in))
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}
