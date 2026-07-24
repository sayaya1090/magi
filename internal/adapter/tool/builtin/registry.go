package builtin

import (
	"sort"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
)

// Registry is a concurrency-safe port.ToolRegistry.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]port.Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]port.Tool)}
}

// Register adds or replaces a tool by name.
func (r *Registry) Register(t port.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Unregister removes a tool by name (used by the plugin host on unload/reload).
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (port.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools, sorted by name (deterministic).
func (r *Registry) List() []port.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]port.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Default returns a Registry populated with all built-in tools.
func Default() *Registry {
	r := NewRegistry()
	r.Register(Read{})
	r.Register(Write{})
	r.Register(Edit{})
	r.Register(MultiEdit{})
	r.Register(Grep{})
	r.Register(Glob{})
	r.Register(List{})
	r.Register(Bash{})
	r.Register(WaitFor{})
	r.Register(BashOutput{})
	r.Register(BashKill{})
	r.Register(BashInput{})
	r.Register(PortOwner{})
	r.Register(TodoWrite{})
	r.Register(WebFetch{})
	r.Register(WebSearch{})
	r.Register(Remember{})
	r.Register(Skill{})
	r.Register(FindContext{})
	r.Register(Tabulate{})
	r.Register(CountMatches{})
	r.Register(CountLines{})
	r.Register(GroupBy{})
	r.Register(RecallContext{})
	r.Register(RecallMemory{})
	r.Register(AstGrep{})
	r.Register(LspDiag{})
	r.Register(Lsp{}) // merged definition/references/symbols (kind-selected)
	return r
}
