package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// A graph is declared in dependency order. Only the coordinator touches Lua or emits hooks.
func installScriptGraph(L *lua.LState, ctx context.Context, call toolCaller, safe func(string) bool, fire func(map[string]any)) {
	type task struct {
		id, label, tool string
		args            json.RawMessage
		after           []int
		state           string
	}
	type result struct {
		index int
		text  string
		ok    bool
	}
	graphID := 0
	L.SetGlobal("call_graph", L.NewFunction(func(L *lua.LState) int {
		list := L.CheckTable(1)
		if list.Len() == 0 || list.Len() > 200 {
			L.RaiseError("call_graph: expected 1 to 200 tasks")
			return 0
		}
		tasks := make([]task, list.Len())
		ids := make(map[string]int, len(tasks))
		for i := range tasks {
			entry, ok := list.RawGetInt(i + 1).(*lua.LTable)
			if !ok {
				L.RaiseError("call_graph: task %d must be a table", i+1)
				return 0
			}
			id, _ := entry.RawGetString("id").(lua.LString)
			tool, _ := entry.RawGetString("tool").(lua.LString)
			if id == "" || tool == "" {
				L.RaiseError("call_graph: task %d needs id and tool", i+1)
				return 0
			}
			if _, exists := ids[string(id)]; exists {
				L.RaiseError("call_graph: duplicate id %s", id)
				return 0
			}
			label, _ := entry.RawGetString("label").(lua.LString)
			if label == "" {
				label = id
			}
			t := task{id: string(id), label: string(label), tool: string(tool), state: "waiting", args: json.RawMessage(`{}`)}
			if args := entry.RawGetString("args"); args != lua.LNil {
				if _, ok := args.(*lua.LTable); !ok {
					L.RaiseError("call_graph: %s args must be a table", id)
					return 0
				}
				raw, err := scriptJSON(args)
				if err != nil {
					L.RaiseError("call_graph: %s args: %v", id, err)
					return 0
				}
				t.args = raw
			}
			if after := entry.RawGetString("after"); after != lua.LNil {
				deps, ok := after.(*lua.LTable)
				if !ok {
					L.RaiseError("call_graph: %s after must be a list", id)
					return 0
				}
				for j := 1; j <= deps.Len(); j++ {
					dep, ok := deps.RawGetInt(j).(lua.LString)
					index, exists := ids[string(dep)]
					if !ok || !exists {
						L.RaiseError("call_graph: %s depends on an unknown or later task %s", id, dep)
						return 0
					}
					t.after = append(t.after, index)
				}
			}
			tasks[i], ids[t.id] = t, i
		}
		graphID++
		publish := func() {
			graph := make([]any, 0, len(tasks))
			for _, t := range tasks {
				deps := make([]any, 0, len(t.after))
				for _, index := range t.after {
					deps = append(deps, tasks[index].id)
				}
				graph = append(graph, map[string]any{"id": t.id, "label": t.label, "tool": t.tool, "state": t.state, "after": deps})
			}
			fire(map[string]any{"phase": "graph", "graph_id": graphID, "graph": graph})
		}
		publish()
		out := L.NewTable()
		finished := make(chan result, scriptParallel)
		active, remaining := 0, len(tasks)
		exclusive := false
		var diagnostics []string
		for remaining > 0 {
			for i := range tasks {
				t := &tasks[i]
				if t.state != "waiting" {
					continue
				}
				ready := true
				var blockedBy []string
				for _, index := range t.after {
					parent := tasks[index]
					ready = ready && parent.state == "done"
					if parent.state == "failed" || parent.state == "skipped" {
						blockedBy = append(blockedBy, parent.id)
					}
				}
				if err := ctx.Err(); err != nil || len(blockedBy) > 0 {
					reason := "blocked by " + strings.Join(blockedBy, ", ")
					if err != nil {
						reason = err.Error()
					}
					diagnostics = append(diagnostics, fmt.Sprintf("%s skipped: %s", t.id, reason))
					t.state = "skipped"
					remaining--
					publish()
					continue
				}
				if !ready || exclusive || active == scriptParallel {
					continue
				}
				serial := safe == nil || !safe(t.tool)
				if serial && active != 0 {
					break
				}
				t.state = "running"
				active++
				exclusive = serial
				publish()
				go func(index int, name string, args json.RawMessage) {
					text, ok := toolText(call(ctx, name, args))
					finished <- result{index, text, ok}
				}(i, t.tool, t.args)
			}
			if active == 0 {
				break
			}
			r := <-finished
			t := &tasks[r.index]
			active--
			remaining--
			exclusive = false
			if r.ok {
				t.state = "done"
				out.RawSetString(t.id, lua.LString(r.text))
			} else {
				t.state = "failed"
				diagnostics = append(diagnostics, fmt.Sprintf("%s: %s", t.id, r.text))
			}
			publish()
		}
		if len(diagnostics) > 0 {
			L.RaiseError("call_graph: %s", strings.Join(diagnostics, "\n"))
			return 0
		}
		if err := ctx.Err(); err != nil {
			L.RaiseError("call_graph: %v", err)
			return 0
		}
		L.Push(out)
		return 1
	}))
}
