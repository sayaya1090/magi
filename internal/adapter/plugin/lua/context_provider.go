package lua

import (
	"context"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/port"
)

// lockWithin tries to acquire mu until the deadline (or ctx cancellation),
// polling TryLock — Go mutexes have no deadline-aware acquire.
func lockWithin(mu *sync.Mutex, ctx context.Context, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if mu.TryLock() {
			return true
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// luaContextProvider adapts a Lua function to port.ContextProvider.
type luaContextProvider struct {
	plugin *plugin
	name   string
	fn     *lua.LFunction
}

func (p *luaContextProvider) Provide(ctx context.Context, q port.ContextQuery) ([]port.ContextChunk, error) {
	// Bounded acquisition, NOT a blocking Lock: an observation handler (fireWith)
	// may hold the plugin mutex for a long magi.analyze sidecar call, and a mutex
	// wait is not context-aware — a blocking lock here would stall the turn's
	// context gathering far past its own ctx deadline. Degrading to "no chunks
	// this step" keeps the conversation moving; the provider runs again next step.
	if !lockWithin(&p.plugin.mu, ctx, 250*time.Millisecond) {
		return nil, nil
	}
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
