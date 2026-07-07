package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type namedLLM struct{ id string }

func (n namedLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	return nil, nil
}

func TestProviderFor(t *testing.T) {
	def := namedLLM{"default"}
	fast := namedLLM{"fast"}
	a := &App{
		llm:       def,
		providers: map[string]port.LLMProvider{"fast": fast},
	}

	// No profile → default provider.
	if got := a.providerFor(AgentSpec{Name: "coder"}); got != port.LLMProvider(def) {
		t.Errorf("unrouted agent should use default provider, got %v", got)
	}
	// Routed to a known profile → that provider.
	if got := a.providerFor(AgentSpec{Name: "explore", Provider: "fast"}); got != port.LLMProvider(fast) {
		t.Errorf("agent routed to 'fast' should use the fast provider, got %v", got)
	}
	// Routed to an unknown profile → falls back to default (never nil).
	if got := a.providerFor(AgentSpec{Name: "x", Provider: "missing"}); got != port.LLMProvider(def) {
		t.Errorf("unknown profile should fall back to default, got %v", got)
	}
}

func TestSetAgentRoute(t *testing.T) {
	a := &App{
		routeOverrides: map[string]routeOverride{},
		cfg: Config{
			Model:         session.ModelRef{Provider: "openai", Model: "base-model"},
			Agents:        map[string]AgentSpec{"explore": {Name: "explore"}, "coder": {Name: "coder"}},
			ProfileModels: map[string]string{"fast": "gpt-oss:20b"},
		},
	}

	// Bare model → default backend, model overridden.
	a.SetAgentRoute("coder", "qwen3-coder:30b")
	if spec, _ := a.resolveAgentSpec("coder"); spec.Model.Model != "qwen3-coder:30b" || spec.Provider != "" {
		t.Errorf("bare model route = %+v", spec)
	}

	// Profile name → provider set AND model taken from the profile.
	a.SetAgentRoute("explore", "fast")
	spec, _ := a.resolveAgentSpec("explore")
	if spec.Provider != "fast" || spec.Model.Model != "gpt-oss:20b" {
		t.Errorf("profile route should set provider+model, got %+v", spec)
	}

	// AgentRoutes reflects the edits; unrouted shows the default model.
	routes := map[string]AgentRoute{}
	for _, r := range a.AgentRoutes("") {
		routes[r.Name] = r
	}
	if routes["explore"].Provider != "fast" || routes["explore"].Model != "gpt-oss:20b" {
		t.Errorf("AgentRoutes explore = %+v", routes["explore"])
	}

	// Empty value clears the override → back to config default (inherits).
	a.SetAgentRoute("coder", "")
	if spec, _ := a.resolveAgentSpec("coder"); spec.Model.Model != "" {
		t.Errorf("clearing should drop the override, got %+v", spec)
	}
	if routes := a.AgentRoutes(""); routesByName(routes, "coder").Model != "base-model" {
		t.Errorf("cleared coder should inherit default model, got %q", routesByName(routes, "coder").Model)
	}
}

func routesByName(rs []AgentRoute, name string) AgentRoute {
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	return AgentRoute{}
}

type recordPersister struct {
	routes   map[string]string
	model    string
	profiles map[string]ProfileDef
}

func (r *recordPersister) PersistRoute(agent, value string) error {
	if r.routes == nil {
		r.routes = map[string]string{}
	}
	r.routes[agent] = value
	return nil
}
func (r *recordPersister) PersistModel(modelID string) error { r.model = modelID; return nil }
func (r *recordPersister) PersistProfile(p ProfileDef) error {
	if r.profiles == nil {
		r.profiles = map[string]ProfileDef{}
	}
	r.profiles[p.Name] = p
	return nil
}

// Edits persist through the RoutePersister so they survive restarts.
func TestRoutePersisted(t *testing.T) {
	p := &recordPersister{}
	a := &App{
		routeOverrides: map[string]routeOverride{},
		sessions:       map[session.SessionID]session.Session{"s1": {ID: "s1"}},
		cfg: Config{
			Agents:         map[string]AgentSpec{"coder": {Name: "coder"}},
			RoutePersister: p,
		},
	}
	a.SetAgentRoute("coder", "qwen3")
	if p.routes["coder"] != "qwen3" {
		t.Errorf("route not persisted: %v", p.routes)
	}
	a.SetModel("s1", "big-model")
	if p.model != "big-model" {
		t.Errorf("model not persisted: %q", p.model)
	}
}

// SetProfile registers a runtime profile (builds a provider via the factory),
// makes it routable, and persists it.
func TestSetProfileRuntime(t *testing.T) {
	p := &recordPersister{}
	built := ""
	a := &App{
		routeOverrides: map[string]routeOverride{},
		providers:      map[string]port.LLMProvider{},
		profileDefs:    map[string]ProfileDef{},
		cfg: Config{
			ProfileModels:  map[string]string{},
			RoutePersister: p,
			Agents:         map[string]AgentSpec{"explore": {Name: "explore"}},
			NewProvider: func(d ProfileDef) port.LLMProvider {
				built = d.Name
				return namedLLM{d.Name}
			},
		},
	}
	a.SetProfile(ProfileDef{Name: "fast", BaseURL: "https://fast/v1", Model: "gpt-oss:20b"})

	if built != "fast" {
		t.Errorf("provider factory not called for the new profile")
	}
	if p.profiles["fast"].Model != "gpt-oss:20b" {
		t.Errorf("profile not persisted: %+v", p.profiles)
	}
	// Routable now: an agent routed to "fast" uses its provider + model.
	a.SetAgentRoute("explore", "fast")
	if spec, _ := a.resolveAgentSpec("explore"); spec.Provider != "fast" || spec.Model.Model != "gpt-oss:20b" {
		t.Errorf("new profile not routable: %+v", spec)
	}
	if a.providerFor(AgentSpec{Provider: "fast"}) != port.LLMProvider(namedLLM{"fast"}) {
		t.Errorf("provider for new profile not registered")
	}
}
