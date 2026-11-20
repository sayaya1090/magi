package lua

import (
	"runtime/debug"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// A script that hands a table holding itself, or one nested past any sane depth, to json_encode,
// call_tool or call_all gets nil and a reason back; the call never reaches the host and the process
// keeps running. The same table used twice side by side is not a cycle.
func TestRunScriptRefusesACyclicOrBottomlessTable(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(64 << 20))
	var calls []recordedCall
	got := runScript(t, scriptTool(t), recordingEnv(&calls), map[string]any{"src": `
      local t = {}; t.self = t
      local r, why = json_encode(t); print("encode", tostring(r), why)
      r, why = call_tool("read", t); print("tool", tostring(r), why)
      r, why = call_all({ { "read", t } }); print("all", tostring(r), why)
      local deep = {}; local at = deep
      for i = 1, 500 do at.next = {}; at = at.next end
      r, why = json_encode(deep); print("deep", tostring(r), why)
      local s = { 1 }
      print("shared", json_encode({ a = s, b = s }))`})
	lines := strings.Split(got, "\n")
	for _, w := range []string{"encode\tnil\t", "tool\tnil\t", "all\tnil\t"} {
		if !strings.Contains(got, w) {
			t.Errorf("want a line starting %q in %q", w, got)
		}
	}
	for _, l := range lines {
		if (strings.HasPrefix(l, "encode\t") || strings.HasPrefix(l, "tool\t") || strings.HasPrefix(l, "all\t")) && !strings.Contains(l, "cycle") {
			t.Errorf("a cyclic table was not named as one: %q", l)
		}
		if strings.HasPrefix(l, "deep\t") && !(strings.HasPrefix(l, "deep\tnil\t") && strings.Contains(l, "deep")) {
			t.Errorf("a bottomless table was not refused: %q", l)
		}
	}
	if !strings.Contains(got, `shared	{"a":[1],"b":[1]}`) {
		t.Errorf("a table used twice side by side is not a cycle: %q", got)
	}
	if len(calls) != 0 {
		t.Errorf("a call with a cyclic argument reached the host: %+v", calls)
	}
}

// Every other caller of luaToGo (a tool's return, a hook's answer) gets a value back, never a crash.
func TestLuaToGoStopsAtACycle(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(64 << 20))
	L := lua.NewState()
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetString("self", tbl)
	if _, err := luaValue(tbl); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("luaValue: want a cycle error, got %v", err)
	}
	_ = luaToGo(tbl)
}
