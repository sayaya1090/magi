package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Runtime agent/model/profile routing and permission config: reads and mutations of the
// per-agent provider routes, session model overrides, profiles, and the global permission
// mode. Split out of app.go; behavior unchanged.

// Permission returns the current tool-permission policy.
func (a *App) Permission() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.permPolicy
}

// SetPermission updates the permission policy at runtime (ask|auto|allow|deny).
func (a *App) SetPermission(p string) {
	a.mu.Lock()
	a.permPolicy = p
	a.mu.Unlock()
}

// agentFor returns the AgentSpec for a session, falling back to a default built
// from the global system prompt with access to all tools.
func (a *App) agentFor(s session.Session) AgentSpec {
	if spec, ok := a.resolveAgentSpec(s.Agent); ok {
		return spec
	}
	return AgentSpec{Name: orDefault(s.Agent, "default"), System: a.cfg.System}
}

// resolveAgentSpec looks up an agent's configured spec and applies any runtime
// routing override (from the /route menu). Used by both agentFor (top-level) and
// spawn (subagents) so overrides take effect everywhere.
func (a *App) resolveAgentSpec(name string) (AgentSpec, bool) {
	spec, ok := a.cfg.Agents[name]
	if !ok {
		return AgentSpec{}, false
	}
	a.mu.Lock()
	ov, has := a.routeOverrides[name]
	a.mu.Unlock()
	if has {
		if ov.model != "" {
			spec.Model = session.ModelRef{Provider: "openai", Model: ov.model}
		}
		spec.Provider = ov.provider
	}
	return spec, true
}

// AgentRoutes returns each configured agent's current effective routing (model +
// profile), for the /route editor. Sorted by name. Unrouted agents inherit the
// SESSION's live model (the single source of truth that SetModel updates), not the
// static config default — so a runtime model change is reflected here too.
func (a *App) AgentRoutes(sid session.SessionID) []AgentRoute {
	names := a.AgentNames()
	a.mu.Lock()
	sessModel := a.cfg.Model.Model
	if s, ok := a.metaLocked(sid); ok && s.Model.Model != "" {
		sessModel = s.Model.Model
	}
	a.mu.Unlock()
	out := make([]AgentRoute, 0, len(names))
	for _, n := range names {
		spec, _ := a.resolveAgentSpec(n)
		m := spec.Model.Model
		if m == "" {
			m = sessModel // unrouted agents inherit the session's current model
		}
		out = append(out, AgentRoute{Name: n, Model: m, Provider: spec.Provider})
	}
	return out
}

// SetModel changes a session's active (default) model at runtime. Session-scoped:
// it updates the cached session so the next loop iteration uses it.
func (a *App) SetModel(sid session.SessionID, modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return
	}
	a.mu.Lock()
	if s, ok := a.metaLocked(sid); ok {
		s.Model = session.ModelRef{Provider: "openai", Model: modelID}
		a.stateLocked(sid).meta = s
	}
	p := a.cfg.RoutePersister
	a.mu.Unlock()
	if p != nil {
		_ = p.PersistModel(modelID) // best-effort
	}
	// Broadcast the change on the bus so any observer (the TUI header, the /route
	// editor) re-reads the model from one signal — regardless of whether this came
	// from the plugin set_model bridge, the /route edit, or reload_config.
	d, _ := json.Marshal(event.ModelChangedData{Model: modelID})
	a.publishTransient(sid, event.TypeModelChanged, event.Actor{Kind: event.ActorSystem, ID: "route"}, d)
}

// SetUserLabel sets the display name shown for the user in the transcript (e.g. an
// authenticated username an SSO plugin injects via magi.set_user_label) and
// broadcasts it so the TUI re-reads from one signal — mirroring SetModel. An empty
// label is ignored (the UI keeps its "you" fallback).
func (a *App) SetUserLabel(sid session.SessionID, label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	a.mu.Lock()
	a.stateLocked(sid).userLabel = label
	a.mu.Unlock()
	d, _ := json.Marshal(event.UserLabelData{Label: label})
	a.publishTransient(sid, event.TypeUserLabelChanged, event.Actor{Kind: event.ActorSystem, ID: "plugin"}, d)
}

// UserLabel returns the display label set for a session's user, or "" if none was
// set (the TUI then falls back to "you").
func (a *App) UserLabel(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.userLabel
	}
	return ""
}

// SessionModel returns the active model name for a session, or "" if unknown. The
// TUI uses it to refresh its header after a plugin reload_config changes the model.
func (a *App) SessionModel(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.metaLocked(sid); ok {
		return s.Model.Model
	}
	return ""
}

// ListModels returns the default backend's model catalog (GET /models via the
// gateway), for the /route editor's suggest box. The port.LLMProvider interface
// carries only StreamChat, so this reaches ListModels through an optional type
// assertion — a provider that doesn't implement it (or a nil provider) yields
// (nil, nil), and the editor falls back to configured profiles / free text.
func (a *App) ListModels(ctx context.Context) ([]string, error) {
	lister, ok := a.llm.(interface {
		ListModels(context.Context) ([]string, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListModels(ctx)
}

// SetAgentRoute applies a runtime routing edit for an agent. A value naming a
// configured profile routes the agent to that backend (provider+model); any
// other value is a bare model on the default backend; empty clears the override.
func (a *App) SetAgentRoute(name, value string) {
	value = strings.TrimSpace(value)
	a.mu.Lock()
	if value == "" {
		delete(a.routeOverrides, name)
	} else if mdl, isProfile := a.cfg.ProfileModels[value]; isProfile {
		a.routeOverrides[name] = routeOverride{model: mdl, provider: value}
	} else {
		a.routeOverrides[name] = routeOverride{model: value}
	}
	p := a.cfg.RoutePersister
	a.mu.Unlock()
	if p != nil {
		_ = p.PersistRoute(name, value) // best-effort
	}
}

// Profiles returns the defined LLM profiles, sorted by name, for the editor.
func (a *App) Profiles() []ProfileDef {
	a.mu.Lock()
	defer a.mu.Unlock()
	names := make([]string, 0, len(a.profileDefs))
	for n := range a.profileDefs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ProfileDef, 0, len(names))
	for _, n := range names {
		out = append(out, a.profileDefs[n])
	}
	return out
}

// SetProfile adds or updates a named LLM profile at runtime: it builds the
// provider (so routing to it works this session), records the definition, and
// persists it to [llm.profiles.<name>]. A no-op if the name is empty.
func (a *App) SetProfile(p ProfileDef) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return
	}
	a.mu.Lock()
	if a.profileDefs == nil {
		a.profileDefs = map[string]ProfileDef{}
	}
	a.profileDefs[p.Name] = p
	a.cfg.ProfileModels[p.Name] = p.Model
	if a.NewProviderFn() != nil {
		a.providers[p.Name] = a.cfg.NewProvider(p)
	}
	persist := a.cfg.RoutePersister
	a.mu.Unlock()
	if persist != nil {
		_ = persist.PersistProfile(p) // best-effort
	}
}

// NewProviderFn returns the configured provider factory (nil-safe helper).
func (a *App) NewProviderFn() ProviderFactory { return a.cfg.NewProvider }

func cloneProviders(m map[string]port.LLMProvider) map[string]port.LLMProvider {
	out := make(map[string]port.LLMProvider, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneProfileDefs(m map[string]ProfileDef) map[string]ProfileDef {
	out := make(map[string]ProfileDef, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// providerFor returns the LLM provider for an agent: its named profile's backend
// (per-agent endpoint/key routing) when set and registered, else the default.
func (a *App) providerFor(spec AgentSpec) port.LLMProvider {
	if spec.Provider != "" {
		if p := a.providers[spec.Provider]; p != nil {
			return p
		}
	}
	return a.llm
}
