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
	// Withdrawn where neither /proc nor lsof can answer — see withheldHere, which is the other half
	// of this branch and must stay its exact complement.
	if portOwnerSupported {
		r.Register(PortOwner{})
	}
	r.Register(TodoWrite{})
	r.Register(Label{})
	r.Register(Council{})
	r.Register(WebFetch{})
	r.Register(WebSearch{})
	r.Register(Remember{})
	r.Register(Skill{})
	r.Register(RecallContext{})
	r.Register(RecallMemory{})
	r.Register(SearchSessions{})
	r.Register(Schedule{})
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
// ⚠ **This answers "is there a tool by that name", not "is one registered here".** They are
// different questions and this one is deliberately the wider: a policy literal is checked against
// it, and `port_owner` is withdrawn where neither /proc nor lsof can answer (registry above). On
// those platforms the registered set does not hold it — and a check built on the registered set
// then reports a correct, deliberate decision as a stale literal. Measured 2026-09-11 on Windows:
// `"port_owner" names no tool`, about a tool this binary has and chose not to offer.
//
// Which is the right reading for the callers: a policy that says port_owner is dangerous is not
// wrong on a machine that withholds it, it is simply not consulted there. What the check is for is
// a name NOTHING answers to anywhere — a rename left behind, a typo — and that is still caught,
// because a withheld tool is listed here and a misspelt one is listed nowhere.
//
// Whether a tool is offered on this machine is `Default().List()`, and TestDefaultRegistry asks
// that separately.
func KnownNames() map[string]bool {
	r := Default()
	RegisterOrchestration(r, false)
	out := map[string]bool{}
	for _, t := range r.List() {
		out[t.Name()] = true
	}
	for _, t := range withheldHere() {
		out[t.Name()] = true
	}
	return out
}

// withheldHere is the tools this binary has and this platform does not register.
//
// The exact complement of the conditional branch in Default: a tool named in both would be offered
// AND reported as withheld, and one named in neither would vanish from KnownNames the moment its
// platform stopped registering it — which is the bug this pair exists to close. A test holds the
// two against each other rather than trusting that whoever edits one remembers the other.
func withheldHere() []port.Tool {
	if portOwnerSupported {
		return nil
	}
	return []port.Tool{PortOwner{}}
}
