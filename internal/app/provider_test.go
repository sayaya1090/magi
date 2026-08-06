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

	// providerFor returns the routed backend behind a metering wrapper (usage_meter.go); routing is
	// what this test is about, so unwrap to compare the backend itself.
	// No profile → default provider.
	if got := unwrapProvider(a.providerFor(AgentSpec{Name: "coder"})); got != port.LLMProvider(def) {
		t.Errorf("unrouted agent should use default provider, got %v", got)
	}
	// Routed to a known profile → that provider.
	if got := unwrapProvider(a.providerFor(AgentSpec{Name: "explore", Provider: "fast"})); got != port.LLMProvider(fast) {
		t.Errorf("agent routed to 'fast' should use the fast provider, got %v", got)
	}
	// Routed to an unknown profile → falls back to default (never nil).
	if got := unwrapProvider(a.providerFor(AgentSpec{Name: "x", Provider: "missing"})); got != port.LLMProvider(def) {
		t.Errorf("unknown profile should fall back to default, got %v", got)
	}
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

// A model change persists through the RoutePersister so it survives a restart.
func TestRoutePersisted(t *testing.T) {
	p := &recordPersister{}
	a := &App{
		states: map[session.SessionID]*sessionState{"s1": {meta: session.Session{ID: "s1"}}},
		cfg: Config{
			Agents:         map[string]AgentSpec{"coder": {Name: "coder"}},
			RoutePersister: p,
		},
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
		providers:   map[string]port.LLMProvider{},
		profileDefs: map[string]ProfileDef{},
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
	if unwrapProvider(a.providerFor(AgentSpec{Provider: "fast"})) != port.LLMProvider(namedLLM{"fast"}) {
		t.Errorf("provider for new profile not registered")
	}
}

// New() clones cfg.ProfileModels to a NON-NIL map, so a runtime profile edit on an app built from a
// MINIMAL Config (no maps supplied) must not panic on a nil-map write.
func TestSetProfileOnFreshAppNoNilMapPanic(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, namedLLM{"d"}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	a.SetProfile(ProfileDef{Name: "p", Model: "m"}) // writes a.cfg.ProfileModels[...] — nil map would panic
	if got := a.Profiles(); len(got) != 1 || got[0].Name != "p" {
		t.Fatalf("profile not recorded on a fresh app: %+v", got)
	}
}
