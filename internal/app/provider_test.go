package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
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
		states:         map[session.SessionID]*sessionState{"s1": {meta: session.Session{ID: "s1"}}},
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

// SetAgentRoute → resolveAgentSpec: a route naming a PROFILE sets both the override model and the
// profile provider (providerFor then routes to that backend); a route naming a BARE model sets only
// the model, leaving the provider empty (= default backend); clearing the route restores the agent's
// configured spec. This locks the profile-vs-bare distinction that drives per-agent routing.
func TestAgentRouteBareVsProfileAndClear(t *testing.T) {
	a := &App{
		routeOverrides: map[string]routeOverride{},
		providers:      map[string]port.LLMProvider{},
		profileDefs:    map[string]ProfileDef{},
		cfg: Config{
			ProfileModels: map[string]string{"fast": "fast-model"},
			Agents: map[string]AgentSpec{
				"coder": {Name: "coder", Model: session.ModelRef{Provider: "openai", Model: "cfg-model"}, Provider: "cfgprov"},
			},
		},
	}

	// Baseline: no override → the configured spec verbatim.
	if spec, ok := a.resolveAgentSpec("coder"); !ok || spec.Model.Model != "cfg-model" || spec.Provider != "cfgprov" {
		t.Fatalf("baseline: model=%q provider=%q ok=%v, want cfg-model/cfgprov/true", spec.Model.Model, spec.Provider, ok)
	}

	// Profile route: model AND provider both come from the profile.
	a.SetAgentRoute("coder", "fast")
	if spec, _ := a.resolveAgentSpec("coder"); spec.Model.Model != "fast-model" || spec.Provider != "fast" {
		t.Fatalf("profile route: model=%q provider=%q, want fast-model/fast", spec.Model.Model, spec.Provider)
	}

	// Bare-model route: model overridden, provider CLEARED to empty (default backend) — it must NOT
	// retain the configured "cfgprov", or a bare-model edit would keep routing to the old profile.
	a.SetAgentRoute("coder", "other-model")
	if spec, _ := a.resolveAgentSpec("coder"); spec.Model.Model != "other-model" || spec.Provider != "" {
		t.Fatalf("bare route: model=%q provider=%q, want other-model/empty", spec.Model.Model, spec.Provider)
	}

	// Clearing the route restores the configured spec.
	a.SetAgentRoute("coder", "")
	if spec, _ := a.resolveAgentSpec("coder"); spec.Model.Model != "cfg-model" || spec.Provider != "cfgprov" {
		t.Fatalf("cleared: model=%q provider=%q, want cfg-model/cfgprov", spec.Model.Model, spec.Provider)
	}
}

// New() clones cfg.ProfileModels and initializes routeOverrides to NON-NIL maps, so a runtime profile
// or route edit on an app built from a MINIMAL Config (no maps supplied) must not panic on a nil-map
// write. Guards against a regression in the New()/cloneStringMap init leaving those maps nil.
func TestSetProfileOnFreshAppNoNilMapPanic(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, namedLLM{"d"}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	a.SetProfile(ProfileDef{Name: "p", Model: "m"}) // writes a.cfg.ProfileModels[...] — nil map would panic
	a.SetAgentRoute("agent", "p")                   // writes a.routeOverrides[...] — nil map would panic
	if got := a.Profiles(); len(got) != 1 || got[0].Name != "p" {
		t.Fatalf("profile not recorded on a fresh app: %+v", got)
	}
	if spec, _ := a.resolveAgentSpec("agent"); spec.Model.Model != "m" {
		// resolveAgentSpec only resolves configured agents; "agent" isn't one, so it returns !ok — the
		// route write itself not panicking is what this test locks.
		_ = spec
	}
}
