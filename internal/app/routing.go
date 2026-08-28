package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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

// Backend is where this companion's LLM requests go now — the reader half of UseBackend.
//
// Empty when the provider cannot say, which a caller reads as "nothing has redirected this". The
// console needs it to show WHICH of the backends it lists is the one in use: without it the
// provider select could name every backend that is serving and none of them as current, so it
// opened blank on every companion.
func (a *App) Backend() string {
	reader, ok := a.llm.(interface{ BaseURL() string })
	if !ok {
		return ""
	}
	return reader.BaseURL()
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
	// There was a per-spawn tool allowlist override here, read from a session field that
	// SpawnRequest.Tools was supposed to fill. There is no SpawnRequest type any more, nothing
	// ever wrote the field, and the branch could not fire — so a worker's tools are its agent's,
	// narrowed by toolSpecs' role and environment gates, which is what actually happened all
	// along.
	//
	// One session role does carry an allowlist, and it is written at the moment the session is
	// created: a conversation opened for a piece of handed-over work that was asked as a QUESTION.
	// Its tools are the four that only look, which is what lets it run beside another turn — see
	// WritingRun. The narrowing is enforced twice over, at advertising and at execution, by the
	// same gate a spawned child's allowlist goes through.
	if s.Agent == LookingAgent {
		return AgentSpec{Name: LookingAgent, System: a.cfg.System, Tools: ReadOnlyToolNames()}
	}
	return AgentSpec{Name: orDefault(s.Agent, "default"), System: a.cfg.System}
}

// LookingAgent is the role of a session that may only read.
//
// A role rather than a flag, because a role is already carried on the session, is already written
// into the log at creation, and is already what decides an agent's tools. A second field saying
// the same thing is a second thing to keep true.
const LookingAgent = "looking"

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
	// Recorded, not just announced.
	//
	// This was bus-only, which reaches every observer inside THIS process and nobody outside it.
	// The console is outside it: it reads the log, so the only model it could ever see was the one
	// on session.created — and its model menu snapped back to that after every successful change,
	// because the value it repaints from had no way to move. A separate process asking "what is it
	// on now" is the ordinary case here, not an edge.
	//
	// It belongs in the log on its own merits, which is the same argument labels.changed carries:
	// which model produced a turn is a fact about work that happened, and nothing can derive it
	// afterwards from a transcript that never wrote it down. appendFact publishes on the bus too,
	// so the observers that already worked keep working.
	// A store-less App still announces. An App is built without one for the doubles in tests and
	// for the read-only paths that never write a log, and SetModel was safe in those before this —
	// a routing change is not a reason for the process to die.
	d, _ := json.Marshal(event.ModelChangedData{Model: modelID})
	if a.store == nil {
		a.publishTransient(sid, event.TypeModelChanged, event.Actor{Kind: event.ActorSystem, ID: "route"}, d)
		return
	}
	// Best-effort on purpose, like the PersistModel above it: the switch has ALREADY taken effect
	// in memory and the turn after this one will use the new model. A store that could not take the
	// record is worth a lost line in the log, not an undone change the caller was told about.
	_ = a.appendFact(context.Background(), sid, event.TypeModelChanged,
		event.Actor{Kind: event.ActorSystem, ID: "route"}, d)
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
	// No session yet: an SSO-style plugin that logs in during its STARTUP handler
	// calls this before CreateSession has run — latch the label and CreateSession
	// applies it to every new session, instead of silently writing it under the
	// empty session id (the "username missing on the first turn" bug).
	if sid == "" {
		a.pendingUserLabel = label
		a.mu.Unlock()
		return
	}
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
	lister, ok := a.llm.(port.ModelLister)
	if !ok {
		// "I cannot say", not "there is nothing". They were one answer here — (nil, nil) — and a
		// caller cannot act differently on facts it was handed identically: an empty menu meant
		// both "this backend lists no models" and "nobody asked it", and the second is a bug the
		// first hides. The sentinel separates them; callers that only want a list still get one.
		return nil, port.ErrCapabilityAbsent
	}
	return lister.ListModels(ctx)
}

// UseBackend points this companion's default backend at base, for the rest of the process's life
// or until something else points it elsewhere.
//
// It is the same override a plugin installs with magi.set_base_url — one registry, so a console
// switch and a plugin's claim cannot disagree about where requests go — and it is deliberately NOT
// persisted: a backend plugin re-establishes its own base on the next start, so writing this to
// config would leave a stale address fighting the plugin that owns it. The person switching in the
// console is switching THIS run.
//
// Refused rather than silently ignored when the provider cannot be redirected (a double in a test,
// a backend built without the capability): a control that reports success and changes nothing is
// the defect this tree keeps finding.
//
// That refusal was itself unreachable for as long as it has existed. The provider here is always
// WRAPPED — the hang guard, and the usage meter over it — and a wrapper implements SetBaseURL
// whether or not the backend under it does, forwarding to nothing and answering 0. So the type
// assertion succeeded on every real build, and the one line that reports "this backend cannot be
// redirected" was reachable only through a bare double nothing in this tree constructs. The token
// is what answers the question (see port.BaseRedirector): 0 is nothing redirected.
func (a *App) UseBackend(sid session.SessionID, base string) error {
	base = strings.TrimSpace(base)
	if base == "" {
		return fmt.Errorf("no backend named")
	}
	// The setter alone, not port.BaseRedirector: pointing a backend somewhere else needs this one
	// method, and demanding the reader and the release with it would refuse a provider that can do
	// exactly what is being asked.
	setter, ok := a.llm.(interface{ SetBaseURL(string) uint64 })
	if !ok {
		return fmt.Errorf("this backend cannot be redirected")
	}
	if setter.SetBaseURL(base) == 0 {
		return fmt.Errorf("this backend cannot be redirected")
	}
	a.adoptServedModel(sid)
	return nil
}

// adoptServedModel moves this session onto a model the NEW backend actually serves.
//
// Redirecting the base URL used to be the whole of a backend switch, and the model name was left
// exactly as it was — so a companion could sit on one backend while holding
// a model name only the OTHER backend has ever heard of.
// /fleet reported that pairing truthfully and the console drew it; the next turn would have been
// refused by the backend. The console can blank its own display and ask, but a display is not the
// state, and the state was wrong until somebody noticed.
//
// The replacement is the backend's FIRST advertised model, not a guess at what somebody wanted.
// There is no rule for which of a stranger's catalog they meant — the console asks them, and puts
// the caret in the field to say so — but a companion has to be runnable while they decide, and the
// only answer available here that is not an invention is the backend's own first offering.
//
// Silent about every failure, and deliberately so: a catalog this backend will not list (an older
// daemon, a gateway behind auth, ErrCapabilityAbsent) is not evidence that the current model is
// wrong, and refusing the switch over it would trade a working control for a guess. The model is
// left alone and the switch stands.
func (a *App) adoptServedModel(sid session.SessionID) {
	lister, ok := a.llm.(port.ModelLister)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return
	}
	a.mu.Lock()
	now := ""
	if s, ok := a.metaLocked(sid); ok {
		now = s.Model.Model
	}
	a.mu.Unlock()
	for _, m := range models {
		if m == now {
			return // this backend serves what the companion is already on
		}
	}
	a.SetModel(sid, models[0])
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
	// Guarded by a.mu: SetProfile writes a.providers at runtime (a /profile edit from the TUI), and
	// providerFor is read from every model-request goroutine — an unlocked read here is a concurrent
	// map read+write (a data race, and a potential panic). Callers do the actual StreamChat OUTSIDE
	// this call, never holding a.mu, so locking here can't deadlock.
	a.mu.Lock()
	defer a.mu.Unlock()
	// Metered HERE, not at construction: a.llm and a.providers stay the raw values so the optional
	// capabilities callers type-assert for (ListModels, and the doubles tests reach through) are not
	// hidden behind a wrapper that does not implement them. Every model request goes through this
	// accessor — the agent's stream and every side call alike — so one wrap here counts them all.
	if spec.Provider != "" {
		if p := a.providers[spec.Provider]; p != nil {
			return a.MeterProvider(p)
		}
	}
	return a.MeterProvider(a.llm)
}
