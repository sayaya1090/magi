package lua

import (
	"context"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/port"
)

// luaContextProvider adapts a Lua function to port.ContextProvider.
type luaContextProvider struct {
	plugin *plugin
	name   string
	fn     *lua.LFunction
}

func (p *luaContextProvider) Provide(ctx context.Context, q port.ContextQuery) ([]port.ContextChunk, error) {
	p.plugin.mu.Lock()
	defer p.plugin.mu.Unlock()

	L := p.plugin.L
	if L == nil {
		return nil, nil // plugin unloaded
	}

	// Build query table
	queryTable := L.NewTable()
	L.SetField(queryTable, "session_id", lua.LString(q.SessionID))
	L.SetField(queryTable, "workdir", lua.LString(q.Workdir))
	L.SetField(queryTable, "prompt", lua.LString(q.Prompt))

	// Call Lua function
	if err := L.CallByParam(lua.P{Fn: p.fn, NRet: 1, Protect: true}, queryTable); err != nil {
		return nil, err
	}

	result := L.Get(-1)
	L.Pop(1)

	// Parse result array
	var chunks []port.ContextChunk
	if resultTable, ok := result.(*lua.LTable); ok {
		resultTable.ForEach(func(k, v lua.LValue) {
			if chunkTable, ok := v.(*lua.LTable); ok {
				source := chunkTable.RawGetString("source")
				text := chunkTable.RawGetString("text")
				if sourceStr, ok := source.(lua.LString); ok {
					if textStr, ok := text.(lua.LString); ok {
						chunks = append(chunks, port.ContextChunk{
							Source: string(sourceStr),
							Text:   string(textStr),
						})
					}
				}
			}
		})
	}

	return chunks, nil
}
