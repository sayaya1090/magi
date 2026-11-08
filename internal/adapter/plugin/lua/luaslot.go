package lua

import (
	"context"
	"fmt"

	"github.com/sayaya1090/magi/internal/port"
	lua "github.com/yuin/gopher-lua"
)

type luaSlot struct {
	plugin      *plugin
	id          string
	target      string
	title       string
	description string
	queryFn     *lua.LFunction
	executeFn   *lua.LFunction
}

func (s *luaSlot) Query(ctx context.Context) (port.PluginSlot, error) {
	slot := port.PluginSlot{
		ID:          s.id,
		Plugin:      s.plugin.name,
		Target:      s.target,
		Title:       s.title,
		Description: s.description,
	}

	if s.queryFn == nil {
		return slot, nil
	}

	s.plugin.mu.Lock()
	defer s.plugin.mu.Unlock()

	L := s.plugin.L
	L.Push(s.queryFn)
	if err := L.PCall(0, 1, nil); err != nil {
		return slot, fmt.Errorf("plugin %q slot %q query: %w", s.plugin.name, s.id, err)
	}

	ret := L.Get(-1)
	L.Pop(1)

	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return slot, nil
	}

	// status: { label = "...", variant = "...", badge = 1 }
	if stVal := tbl.RawGetString("status"); stVal != lua.LNil {
		if stTbl, ok := stVal.(*lua.LTable); ok {
			st := &port.SlotStatus{}
			if lbl := stTbl.RawGetString("label"); lbl != lua.LNil {
				st.Label = lbl.String()
			}
			if vr := stTbl.RawGetString("variant"); vr != lua.LNil {
				st.Variant = vr.String()
			}
			if bd := stTbl.RawGetString("badge"); bd != lua.LNil {
				if n, ok := bd.(lua.LNumber); ok {
					st.Badge = int(n)
				}
			}
			slot.Status = st
		}
	}

	// items: [ { id = "...", label = "...", value = "...", description = "...", warning = bool } ]
	if itemsVal := tbl.RawGetString("items"); itemsVal != lua.LNil {
		if itemsTbl, ok := itemsVal.(*lua.LTable); ok {
			itemsTbl.ForEach(func(_, v lua.LValue) {
				if itemTbl, ok := v.(*lua.LTable); ok {
					item := port.SlotItem{}
					if id := itemTbl.RawGetString("id"); id != lua.LNil {
						item.ID = id.String()
					}
					if lbl := itemTbl.RawGetString("label"); lbl != lua.LNil {
						item.Label = lbl.String()
					}
					if val := itemTbl.RawGetString("value"); val != lua.LNil {
						item.Value = val.String()
					}
					if desc := itemTbl.RawGetString("description"); desc != lua.LNil {
						item.Description = desc.String()
					}
					if w := itemTbl.RawGetString("warning"); w != lua.LNil {
						item.Warning = lua.LVAsBool(w)
					}
					slot.Items = append(slot.Items, item)
				}
			})
		}
	}

	// actions: [ { id = "...", label = "...", description = "...", variant = "..." } ]
	if actsVal := tbl.RawGetString("actions"); actsVal != lua.LNil {
		if actsTbl, ok := actsVal.(*lua.LTable); ok {
			actsTbl.ForEach(func(_, v lua.LValue) {
				if actTbl, ok := v.(*lua.LTable); ok {
					act := port.SlotAction{}
					if id := actTbl.RawGetString("id"); id != lua.LNil {
						act.ID = id.String()
					}
					if lbl := actTbl.RawGetString("label"); lbl != lua.LNil {
						act.Label = lbl.String()
					}
					if desc := actTbl.RawGetString("description"); desc != lua.LNil {
						act.Description = desc.String()
					}
					if vr := actTbl.RawGetString("variant"); vr != lua.LNil {
						act.Variant = vr.String()
					}
					slot.Actions = append(slot.Actions, act)
				}
			})
		}
	}

	return slot, nil
}

func (s *luaSlot) Execute(ctx context.Context, actionID string) (string, error) {
	if s.executeFn == nil {
		return "", fmt.Errorf("slot %q does not support actions", s.id)
	}

	s.plugin.mu.Lock()
	defer s.plugin.mu.Unlock()

	L := s.plugin.L
	L.Push(s.executeFn)
	L.Push(lua.LString(actionID))
	if err := L.PCall(1, 1, nil); err != nil {
		return "", fmt.Errorf("plugin %q slot %q action %q: %w", s.plugin.name, s.id, actionID, err)
	}

	ret := L.Get(-1)
	L.Pop(1)

	if ret == lua.LNil {
		return "", nil
	}
	return ret.String(), nil
}
