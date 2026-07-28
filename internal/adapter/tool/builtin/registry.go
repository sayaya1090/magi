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
	r.Register(Council{})
	r.Register(WebFetch{})
	r.Register(WebSearch{})
	r.Register(Remember{})
	r.Register(Skill{})
	r.Register(RecallContext{})
	r.Register(RecallMemory{})
	return r
}

// RegisterOrchestration adds the tools that only exist when a human is on the other end.
//
// They are omitted in a headless run: with no one to answer a multiple-choice question (ask_user)
// or send a mid-turn interjection (route_interjection), they can never fire and only add weight to
// the model's tool list — which a weak model pays for on every request.
//
// It lives here, beside Default, because every caller that builds a working registry needs the same
// set and a second copy drifts silently: a list maintained by hand cannot fail a build when a tool
// is added to one copy and not the other, and one of these copies had already fallen two tools
// behind before this function existed.
func RegisterOrchestration(r *Registry, headless bool) {
	if !headless {
		r.Register(AskUser{})           // multiple-choice question to the human user
		r.Register(RouteInterjection{}) // route a mid-turn user interjection
	}
}

// KnownNames returns every name a built-in tool answers to, headless-only tools included.
//
// Policy code elsewhere decides what to do about a tool by writing its name as a literal — which
// tools count as acting, which are dangerous, which fetch an external fact. Those literals are
// unverifiable on their own: a name that no tool answers to reads exactly like one that does, and a
// tool renamed or removed leaves the literal behind as vocabulary nothing can ever match. This is
// what lets a test say which of those literals are real.
//
// Only built-ins are listed. Plugin and MCP tools register at runtime under names this package
// cannot know, so absence here means "not a built-in", not "not a tool".
func KnownNames() map[string]bool {
	r := Default()
	RegisterOrchestration(r, false)
	out := map[string]bool{}
	for _, t := range r.List() {
		out[t.Name()] = true
	}
	return out
}
