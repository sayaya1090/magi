package lua

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/port"
	"github.com/sayaya1090/magi/internal/prompt"
)

// ToolSink is the subset of a tool registry the host needs to add and remove
// plugin-contributed tools (satisfied by *builtin.Registry).
type ToolSink interface {
	Register(port.Tool)
	Unregister(name string)
}

// MCPManager is the interface for registering MCP servers from plugins.
type MCPManager interface {
	AddHTTP(ctx context.Context, name, url string, headers map[string]string) error
	AddHTTPDynamic(ctx context.Context, name, url string, headersFn func() map[string]string) error
	AddStdio(ctx context.Context, name, command string, args, env []string) error
	Remove(name string)
}

// RuntimeInfo provides runtime context information to plugins.
type RuntimeInfo struct {
	Model    string // Current LLM model name
	Platform string // OS platform (darwin, linux, windows)
	Workdir  string // Current working directory
	Username string // OS/login user name ("" when undeterminable)
	// CouncilEnabled says a consensus council judges completion declarations in this run. A
	// plugin that has a door of its own for "the turn is finished" (landing's `land`) should
	// then register a declaration gate instead — one door, not two (2026-09-05).
	CouncilEnabled bool
}

// ContextProviderRegistry allows plugins to register context providers.
type ContextProviderRegistry interface {
	RegisterContextProvider(port.ContextProvider)
}

// LLMHeaderRegistry allows plugins to inject custom headers into LLM backend
// requests (e.g. an in-house gateway's X-CLIENT-API-KEY or an SSO token). fn is
// re-evaluated on every request.
type LLMHeaderRegistry interface {
	AddLLMHeaders(fn func() map[string]string)
}

// EmbedEndpointRegistry is where an embedding endpoint chosen at run time is installed.
// *embed.Client is one.
type EmbedEndpointRegistry interface {
	SetEndpoint(url, model string)
}

// BaseURLRegistry lets a plugin redirect the LLM backend base URL at runtime
// (magi.set_base_url) — e.g. to point the agent at a loopback server the plugin runs
// in-process via magi.serve. An empty string clears the override.
type BaseURLRegistry interface {
	// SetBaseURL installs the override and returns an ownership token to release it with.
	// A real token is never 0; 0 means nothing was redirected, which is what a registry that
	// forwards to a backend with no redirect in it answers. The token is the answer, not the call.
	SetBaseURL(url string) uint64
	// ClearBaseURL releases the override only if tok still identifies the current one, so
	// unloading/reloading one plugin can't wipe an override a newer Set installed since
	// (including a hot-reload's new instance re-installing the same URL).
	ClearBaseURL(tok uint64)
}

// ModelRegistry lets a plugin change model settings for the live session at
// runtime. It groups the model-configuration bridges that share the
// config:write:model grant and a single wiring adapter over the App:
//   - SetModel (magi.set_model): switch the session's active model — e.g. an SSO
//     plugin that picks the model only after it learns which backend the user is
//     entitled to. Mirrors the TUI /route editor.
//   - SetContextWindow (magi.set_context_window): override a model's context
//     window in tokens — e.g. a plugin that learns the real window from an
//     in-house model API the built-in backend prober can't reach. Mirrors the
//     /context command; tokens <= 0 means unlimited/unknown, and the override
//     blocks a later lazy probe from clobbering it. An empty modelID targets the
//     session's current model.
//
// Implementations apply the change to the live session. An empty model id is
// rejected by the set_model bridge (but allowed by set_context_window, where it
// means "the session model").
type ModelRegistry interface {
	SetModel(modelID string) error
	SetContextWindow(modelID string, tokens int) error
}

// UserLabelRegistry lets a plugin set the display name shown for the user in the
// transcript (magi.set_user_label) — e.g. an SSO plugin that learns the
// authenticated username. The implementation applies it to the live session and
// broadcasts the change; an empty label is ignored (the UI keeps "you").
type UserLabelRegistry interface {
	SetUserLabel(label string)
}

// Host loads, reloads, and unloads Lua plugins, registering their tools into a
// shared ToolSink so changes take effect in the running agent (hot reload).
type Host struct {
	sink               ToolSink
	mcpMgr             MCPManager
	contextReg         ContextProviderRegistry
	llmReg             LLMHeaderRegistry
	baseReg            BaseURLRegistry
	embedReg           EmbedEndpointRegistry
	modelReg           ModelRegistry
	userReg            UserLabelRegistry
	runtime            RuntimeInfo
	pluginConfigs      map[string]map[string]any    // [plugins.<name>] sections from config.toml
	configPath         string                       // path to the user's config.toml (magi.get/set_config_key)
	dataDir            string                       // base dir for per-plugin persistent stores
	prompter           prompt.Prompter              // interactive prompts (magi.ask); nil = unavailable
	analyzer           Analyzer                     // sidecar LLM analysis (magi.analyze); nil = unavailable
	experience         port.ExperienceStore         // D13 team store (magi.propose_experience); nil = unavailable
	notify             func(sessionID, text string) // session note sink (magi.notify); nil = unavailable
	finish             func(sessionID string)       // turn ender (magi.finish); nil = unavailable
	slotChangeNotifier func(target, plugin string)  // optional: slot observer sink (magi.notify_slot_change)
	logf               func(string)
	onAnalyze          func(AnalyzeUsage)

	mu        sync.Mutex
	plugins   map[string]*plugin
	uiEffects []string // UI effects queued by bridges (e.g. "clear_transcript"), drained by the interactive UI

	// Observation events (user_message / turn_finished) are fired through a
	// bounded queue drained by ONE worker goroutine: the app-side caller
	// (FireEventWith) never blocks on a plugin handler — a handler is free to
	// run a slow magi.analyze sidecar without holding up the turn — and per-
	// plugin handler order is preserved. Overflow drops the event (observation
	// is best-effort, never back-pressure on the conversation).
	evOnce  sync.Once
	evQueue chan firedEvent
	evBusy  sync.WaitGroup // outstanding queued/running observation events (DrainEvents)

	// Idle detection. A plugin that registers an "idle" handler gets one call
	// per quiet period: no user prompt and no turn in flight for idleAfter.
	// Activity is read off the observation events this host already carries
	// (user_message opens a turn, turn_finished closes it), so the app needs no
	// new plumbing. Background upkeep (engram's reflection/dream) runs then
	// instead of between a person's prompts.
	idleAfter    time.Duration
	idleTick     time.Duration
	idleOnce     sync.Once
	idleMu       sync.Mutex
	lastActivity time.Time
	inflight     map[string]time.Time // session → when its open turn started
	idleFired    bool                 // already fired for the current quiet period
}

// defaultIdleAfter is how long without user input before the "idle" event fires.
const defaultIdleAfter = 30 * time.Minute

// inflightStale bounds how long an open turn can hold off idle. A turn whose
// turn_finished was never observed (process crash, dropped event) must not
// block background upkeep forever.
const inflightStale = 6 * time.Hour

type firedEvent struct {
	name    string
	payload map[string]string
}

// Analyzer runs a one-shot, tool-free LLM analysis on behalf of a plugin
// (magi.analyze) — a "sidecar" call for observation-style plugins (lesson
// extraction, summarizers) that must never mutate anything. model "" = the
// session's current model. Implementations bound the call with their own
// timeout.
type Analyzer interface {
	Analyze(ctx context.Context, system, text, model string) (string, error)
}

// MeasuredAnalyzer optionally exposes provider usage for a tool-free request.
type MeasuredAnalyzer interface {
	AnalyzeMeasured(context.Context, string, string, string) (string, port.RunMetrics, error)
}

// AnalyzeUsage describes one finished magi.analyze call. Label is the plugin's own name for the
// call ("reflection", "dream"), empty when it gave none. Metrics.TokensKnown is false when the
// analyzer or the backend reported no usage, which is not the same as zero tokens.
type AnalyzeUsage struct {
	Plugin  string
	Label   string
	Metrics port.RunMetrics
	Err     error
}

// HostConfig configures the plugin host.
type HostConfig struct {
	OnAnalyze  func(AnalyzeUsage) // optional: told about every magi.analyze call
	ToolSink   ToolSink
	MCPMgr     MCPManager              // optional: enables magi.register_mcp()
	ContextReg ContextProviderRegistry // optional: enables magi.register_context_provider()
	LLMReg     LLMHeaderRegistry       // optional: enables magi.set_llm_headers()
	BaseReg    BaseURLRegistry         // optional: enables magi.set_base_url()
	// EmbedReg, when set, enables magi.set_embed_endpoint: a plugin that starts a local embedding
	// server hands its address to the client recall already uses.
	EmbedReg   EmbedEndpointRegistry
	ModelReg   ModelRegistry                // optional: enables magi.set_model()
	UserReg    UserLabelRegistry            // optional: enables magi.set_user_label()
	Analyzer   Analyzer                     // optional: enables magi.analyze (sidecar LLM analysis)
	Experience port.ExperienceStore         // optional: enables magi.propose_experience (D13 team store)
	Notify     func(sessionID, text string) // optional: enables magi.notify (session transcript note)
	// Finish, when set, enables magi.finish: a tool that ends the turn it was called in. The
	// landing plugin's `land` is the case — without it the turn ended only when the model's next
	// step carried no call, and a model that kept re-calling land never produced one.
	Finish             func(sessionID string)
	SlotChangeNotifier func(target, plugin string) // optional: slot change observer
	PluginConfigs      map[string]map[string]any   // optional: [plugins.<name>] settings, read via magi.store_get
	ConfigPath         string                      // optional: path to config.toml (enables magi.get/set_config_key)
	DataDir            string                      // base dir for per-plugin persistent config stores (store_set)
	Prompter           prompt.Prompter             // optional: enables magi.ask interactive prompts
	Runtime            RuntimeInfo                 // runtime context for plugins
	Logf               func(string)                // optional: log output
	// IdleAfter is the quiet period before the "idle" event fires (default 30m);
	// IdleTick is how often it is checked (default 1m). Tests shorten both.
	IdleAfter time.Duration
	IdleTick  time.Duration
}

// NewHostWithConfig returns a plugin host with full configuration.
func NewHostWithConfig(cfg HostConfig) *Host {
	if cfg.Logf == nil {
		cfg.Logf = func(string) {}
	}
	if cfg.IdleAfter <= 0 {
		cfg.IdleAfter = defaultIdleAfter
	}
	if cfg.IdleTick <= 0 {
		cfg.IdleTick = time.Minute
	}
	return &Host{
		idleAfter:          cfg.IdleAfter,
		idleTick:           cfg.IdleTick,
		lastActivity:       time.Now(),
		inflight:           map[string]time.Time{},
		sink:               cfg.ToolSink,
		mcpMgr:             cfg.MCPMgr,
		contextReg:         cfg.ContextReg,
		llmReg:             cfg.LLMReg,
		baseReg:            cfg.BaseReg,
		embedReg:           cfg.EmbedReg,
		modelReg:           cfg.ModelReg,
		userReg:            cfg.UserReg,
		runtime:            cfg.Runtime,
		pluginConfigs:      cfg.PluginConfigs,
		configPath:         cfg.ConfigPath,
		dataDir:            cfg.DataDir,
		prompter:           cfg.Prompter,
		analyzer:           cfg.Analyzer,
		experience:         cfg.Experience,
		notify:             cfg.Notify,
		finish:             cfg.Finish,
		slotChangeNotifier: cfg.SlotChangeNotifier,
		logf:               cfg.Logf,
		onAnalyze:          cfg.OnAnalyze,
		plugins:            map[string]*plugin{},
	}
}

// snapshot returns the loaded plugins in NAME ORDER. h.plugins is a map, and Go randomises map
// iteration on purpose, so anything built by ranging it — the slash-command list, the tool list,
// the doctor report — came out in a different order on every draw. Sorting here fixes all of them
// at once and makes "which plugin wins a duplicate name" answerable instead of a coin flip.
// Callers that go on to touch p.mu MUST use this and release h.mu first (see the note below).
func (h *Host) snapshot() []*plugin {
	out := make([]*plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// HasEventHandlers reports whether any loaded plugin registered a handler for
// the event — lets the app skip building observation payloads nobody consumes.
//
// The plugin list is snapshotted and h.mu RELEASED before touching any p.mu:
// bridge paths run Lua under p.mu and then take h.mu (runtimeModel/uiEffects),
// so holding both here would invert the lock order into an ABBA deadlock.
func (h *Host) HasEventHandlers(event string) bool {
	h.mu.Lock()
	plugins := h.snapshot()
	h.mu.Unlock()
	for _, p := range plugins {
		if p.hasHook(event) {
			return true
		}
	}
	return false
}

// Has reports whether a plugin with the given name is loaded — lets the caller
// skip an embedded fallback when a user-installed plugin of the same name won.
func (h *Host) Has(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.plugins[name]
	return ok
}

// FireEventWith enqueues a payload-carrying observation event (user_message /
// turn_finished) for every loaded plugin. Non-blocking: the caller returns
// immediately and one background worker runs the handlers in order; when the
// queue is full the event is dropped (observation is best-effort).
func (h *Host) FireEventWith(event string, payload map[string]string) {
	h.noteActivity(event, payload)
	h.evOnce.Do(func() {
		h.evQueue = make(chan firedEvent, 128)
		go func() {
			for ev := range h.evQueue {
				h.mu.Lock()
				plugins := make([]*plugin, 0, len(h.plugins))
				for _, p := range h.plugins {
					plugins = append(plugins, p)
				}
				h.mu.Unlock()
				for _, p := range plugins {
					p.fireWith(ev.name, ev.payload)
				}
				h.evBusy.Done()
			}
		}()
	})
	h.evBusy.Add(1)
	select {
	case h.evQueue <- firedEvent{name: event, payload: payload}:
	default:
		h.evBusy.Done()
		h.logf("plugin events: queue full — dropped " + event)
	}
}

// noteActivity records user activity from the observation stream: a prompt opens
// a turn for its session, a finished turn closes it. Either one ends the current
// quiet period, so the next "idle" fires only after a fresh idleAfter of silence.
func (h *Host) noteActivity(event string, payload map[string]string) {
	if event != "user_message" && event != "turn_finished" {
		return
	}
	sid := payload["session"]
	h.idleMu.Lock()
	defer h.idleMu.Unlock()
	now := time.Now()
	h.lastActivity = now
	h.idleFired = false
	if h.inflight == nil {
		h.inflight = map[string]time.Time{}
	}
	if event == "user_message" {
		h.inflight[sid] = now
	} else {
		delete(h.inflight, sid)
	}
}

// idleDue reports whether the host has been quiet long enough to fire "idle",
// and marks the quiet period as fired so it is delivered once.
func (h *Host) idleDue(now time.Time) (time.Duration, bool) {
	h.idleMu.Lock()
	defer h.idleMu.Unlock()
	quiet := now.Sub(h.lastActivity)
	if h.idleFired || quiet < h.idleAfter {
		return 0, false
	}
	for sid, started := range h.inflight {
		if now.Sub(started) < inflightStale {
			return 0, false // a turn is still running; a long agent run is not idle
		}
		delete(h.inflight, sid)
	}
	h.idleFired = true
	return quiet, true
}

// ensureIdleWatch starts the idle watcher the first time a plugin asks for the
// "idle" event. Hosts nobody registers idle on never spawn the goroutine.
func (h *Host) ensureIdleWatch() {
	h.idleOnce.Do(func() {
		go func() {
			t := time.NewTicker(h.idleTick)
			defer t.Stop()
			for now := range t.C {
				if quiet, ok := h.idleDue(now); ok {
					h.FireEventWith("idle", map[string]string{"idle_seconds": strconv.Itoa(int(quiet.Seconds()))})
				}
			}
		}()
	})
}

// DrainEvents waits (up to timeout) for queued observation events to finish.
// A headless one-shot run exits right after its turn — without a drain the
// process dies before an observer's sidecar analysis lands, so nothing is ever
// captured outside the TUI. Call before shutdown.
func (h *Host) DrainEvents(timeout time.Duration) {
	done := make(chan struct{})
	go func() { h.evBusy.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		h.logf("plugin events: drain timed out — an observation may be lost")
	}
}

// FireEvent runs every loaded plugin's handlers for a lifecycle event,
// synchronously (handlers may block — e.g. a startup auth flow). Call "startup"
// after loading plugins and before the first turn; "shutdown" on exit.
func (h *Host) FireEvent(event string) {
	h.mu.Lock()
	plugins := h.snapshot()
	h.mu.Unlock()
	for _, p := range plugins {
		p.fire(event)
	}
}

// Load loads the plugin in dir and registers its capabilities.
func (h *Host) Load(ctx context.Context, dir string) (port.PluginInfo, error) {
	p, err := loadPlugin(dir, h.logf, h)
	if err != nil {
		return port.PluginInfo{}, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.plugins[p.name]; ok {
		h.unregister(old)
		old.close()
	}
	h.plugins[p.name] = p
	if h.sink != nil {
		for _, t := range p.tools {
			h.sink.Register(t)
		}
	}
	return port.PluginInfo{Name: p.name, Version: p.manifest.Version, Capabilities: p.manifest.Capabilities}, nil
}

// Unload removes a plugin and its capabilities.
func (h *Host) Unload(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}
	h.unregister(p)
	p.close()
	delete(h.plugins, name)
	return nil
}

// Reload re-reads a plugin from disk (hot reload). Session state is unaffected
// because it lives in the core, not the plugin.
func (h *Host) Reload(name string) error {
	h.mu.Lock()
	dir := ""
	if p, ok := h.plugins[name]; ok {
		dir = p.dir
	}
	h.mu.Unlock()
	if dir == "" {
		return fmt.Errorf("plugin %q not loaded", name)
	}
	_, err := h.Load(context.Background(), dir)
	return err
}

// Capabilities returns the aggregate capabilities across loaded plugins.
func (h *Host) Capabilities() port.CapabilitySet {
	h.mu.Lock()
	defer h.mu.Unlock()
	var cs port.CapabilitySet
	for _, p := range h.snapshot() {
		for _, t := range p.tools {
			cs.Tools = append(cs.Tools, t)
		}
		for _, c := range p.commands {
			cs.Commands = append(cs.Commands, c)
		}
	}
	return cs
}

// PluginCommands returns every slash command contributed by loaded plugins, so
// the TUI can surface them in /help and the palette.
func (h *Host) PluginCommands() []port.PluginCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	var cmds []port.PluginCommand
	for _, p := range h.snapshot() {
		for _, c := range p.commands {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// DoctorProbes returns every environment check contributed by loaded plugins, so
// `magi -doctor` can fold them into its report (mirrors PluginCommands).
func (h *Host) DoctorProbes() []port.DoctorProbe {
	h.mu.Lock()
	defer h.mu.Unlock()
	var probes []port.DoctorProbe
	for _, p := range h.snapshot() {
		for _, pr := range p.probes {
			probes = append(probes, pr)
		}
	}
	return probes
}

// QuerySlots collects and queries all slots registered for the given target across loaded plugins.
func (h *Host) QuerySlots(ctx context.Context, target string) ([]port.PluginSlot, error) {
	h.mu.Lock()
	var slots []*luaSlot
	for _, p := range h.snapshot() {
		for _, s := range p.slots {
			if target == "" || s.target == target {
				slots = append(slots, s)
			}
		}
	}
	h.mu.Unlock()

	var result []port.PluginSlot
	for _, s := range slots {
		slot, err := s.Query(ctx)
		if err != nil {
			if h.logf != nil {
				h.logf(fmt.Sprintf("QuerySlots error for %s/%s: %v", s.plugin.name, s.id, err))
			}
			continue
		}
		result = append(result, slot)
	}
	return result, nil
}

// TriggerSlotAction executes an action on the slot a caller named. The plugin is required: an action
// with no plugin used to run on whichever plugin loaded first. The slot may be left empty only when
// the plugin has one slot that can act, because picking among several would run the wrong one.
func (h *Host) TriggerSlotAction(ctx context.Context, plugin, slot, action string) (string, error) {
	if plugin == "" {
		return "", fmt.Errorf("slot action %q names no plugin", action)
	}
	h.mu.Lock()
	var candidates []*luaSlot
	for _, p := range h.snapshot() {
		if p.name != plugin {
			continue
		}
		for _, s := range p.slots {
			if s.executeFn != nil && (slot == "" || s.id == slot) {
				candidates = append(candidates, s)
			}
		}
	}
	h.mu.Unlock()

	switch len(candidates) {
	case 0:
		if slot != "" {
			return "", fmt.Errorf("plugin %q has no slot %q that can act", plugin, slot)
		}
		return "", fmt.Errorf("no slot found for plugin %q", plugin)
	case 1:
		return candidates[0].Execute(ctx, action)
	}
	ids := make([]string, len(candidates))
	for i, s := range candidates {
		ids[i] = s.id
	}
	return "", fmt.Errorf("plugin %q has %d slots (%s); name one", plugin, len(ids), strings.Join(ids, ", "))
}

// DispatchCommand runs the plugin command whose name matches (leading slash
// tolerated), returning (true, err) if one handled it and (false, nil) if no
// plugin owns the name so the caller can report "unknown command".
func (h *Host) DispatchCommand(name string, args []string) (bool, error) {
	name = strings.TrimPrefix(name, "/")
	h.mu.Lock()
	var cmd *luaCommand
	for _, p := range h.snapshot() {
		for _, c := range p.commands {
			if c.name == name {
				cmd = c
				break
			}
		}
		if cmd != nil {
			break // first owner in name order wins; the inner break alone let a later plugin take it
		}
	}
	h.mu.Unlock() // release before Execute: it locks the plugin, not the host
	if cmd == nil {
		return false, nil
	}
	return true, cmd.Execute(args)
}

// setRuntimeModel updates the model reported by magi.model() so a set_model /
// reload_config bridge keeps the read side in sync with the running session.
func (h *Host) setRuntimeModel(model string) {
	h.mu.Lock()
	h.runtime.Model = model
	h.mu.Unlock()
}

// runtimeModel returns the model reported to plugins (guarded, since set_model /
// reload_config may update it from a command handler).
func (h *Host) runtimeModel() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runtime.Model
}

// pushUIEffect queues a UI effect requested by a plugin bridge (e.g. clearing the
// transcript back to the splash). Runs during a command's Lua handler, when the
// caller has released h.mu; the interactive UI drains these via TakeUIEffects.
func (h *Host) pushUIEffect(kind string) {
	h.mu.Lock()
	h.uiEffects = append(h.uiEffects, kind)
	h.mu.Unlock()
}

// TakeUIEffects returns and clears the queued UI effects. The TUI calls this right
// after DispatchCommand so a plugin command (e.g. /logout) can return to the splash.
func (h *Host) TakeUIEffects() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.uiEffects
	h.uiEffects = nil
	return e
}

// unregister removes a plugin's tools from the sink (caller holds h.mu).
func (h *Host) unregister(p *plugin) {
	if h.sink == nil {
		return
	}
	for _, t := range p.tools {
		h.sink.Unregister(t.name)
	}
}

// RunDeclarationGates asks every plugin gate whether a completion declaration may go to
// the council. Each gate runs under its plugin's lock with env set (so magi.turn_steps
// answers) and returns nil to pass or a string to refuse; the refusals come back in
// plugin order. No gates → nil. A gate that errors is logged and treated as passing:
// a broken gate must not lock a turn shut.
func (h *Host) RunDeclarationGates(ctx context.Context, env port.ToolEnv) []string {
	var out []string
	for _, p := range h.snapshot() {
		p.mu.Lock()
		if len(p.gates) == 0 {
			p.mu.Unlock()
			continue
		}
		prevEnv, prevCtx := p.env, p.callCtx
		p.env, p.callCtx = env, ctx
		for _, fn := range p.gates {
			L := p.L
			L.Push(fn)
			if err := L.PCall(0, 1, nil); err != nil {
				h.logf(fmt.Sprintf("[%s] declaration gate: %v", p.name, err))
				continue
			}
			if s, ok := L.Get(-1).(lua.LString); ok && strings.TrimSpace(string(s)) != "" {
				out = append(out, string(s))
			}
			L.Pop(1)
		}
		p.env, p.callCtx = prevEnv, prevCtx
		p.mu.Unlock()
	}
	return out
}
